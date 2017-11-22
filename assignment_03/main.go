package app

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"

    "github.com/golang/geo/s2"
    "github.com/abiosoft/semaphore"
	"cloud.google.com/go/bigquery"
	"github.com/gorilla/mux"
	"golang.org/x/net/context"
	"google.golang.org/api/iterator"
	"google.golang.org/appengine"
	"google.golang.org/appengine/log"
	"google.golang.org/appengine/urlfetch"
	"googlemaps.github.io/maps"
)

const storageAPIURL = "https://www.googleapis.com/storage/v1/b/gcp-public-data-sentinel-2/o?prefix="
const googleGeoAPIURL = "https://maps.googleapis.com/maps/api/geocode/json?sensor=false&address="
const apiKey = "AIzaSyBfbOhnMrQFj0BUHWA4EABJMW8qIts49WU"
const maxConcurrentRequests = 100

//semaphore limit total number of concurrent goroutines
var sem = semaphore.New(maxConcurrentRequests)

type queryResult struct {
	Granule_id string
	Base_url   string
}

type googleGeocodeResponse struct {
	Results []struct {
		Geometry struct {
			Location struct {
				Lat float64
				Lng float64
			}
		}
	}
}

// From a query result, formulate an url to the google storage api
// for the folder of the queryResult
func formatURL(result queryResult) string {
	return fmt.Sprintf("%s%s/GRANULE/%s/IMG_DATA/", storageAPIURL,
		result.Base_url[32:], result.Granule_id)
}

func getUrlsBetweenCoords(ctx context.Context, northLat float64, southLat float64, 
                          eastLng float64, westLng float64) []string {
	projectID := appengine.AppID(ctx)
	client, err := bigquery.NewClient(ctx, projectID)
	if err != nil {
		log.Errorf(ctx, "Failed to create client: %v", err)
	}

	// using a dirty hack to insert backticks into the string
	q := client.Query(fmt.Sprintf(`
            SELECT granule_id, base_url 
			FROM %sbigquery-public-data.cloud_storage_geo_index.sentinel_2_index%s
            WHERE north_lat <= %f AND south_lat >= %f
            AND east_lon <= %f AND west_lon >= %f 
			`, "`", "`", northLat, southLat, eastLng, westLng))

	it, queryErr := q.Read(ctx)
	if queryErr != nil {
		log.Criticalf(ctx, "Query failed to execute: %v", queryErr)
        return make([]string, 0)
	}

	urls := make([]string, 0, 0)
	for {
		var value queryResult
		err := it.Next(&value)
		if err == iterator.Done || err != nil {
			break
		}
		urls = append(urls, formatURL(value))
	}
	return urls
}

func getUrlsFromMgrs(ctx context.Context, mgrs string) []string {
	projectID := appengine.AppID(ctx)
	client, err := bigquery.NewClient(ctx, projectID)
	if err != nil {
		log.Errorf(ctx, "Failed to create client: %v", err)
	}

	// using a dirty hack to insert backticks into the string
	q := client.Query(fmt.Sprintf(`
			SELECT granule_id, base_url
			FROM %sbigquery-public-data.cloud_storage_geo_index.sentinel_2_index%s
			WHERE (mgrs_tile LIKE '%s%s')
			`, "`", "`", mgrs, "%"))

	it, queryErr := q.Read(ctx)
	if queryErr != nil {
		log.Errorf(ctx, "Query failed to execute: %v", queryErr)
	}

	urls := make([]string, 0, 0)
	for {
		var value queryResult
		err := it.Next(&value)
		if err == iterator.Done || err != nil {
			break
		}
		urls = append(urls, formatURL(value))
	}
	return urls
}

// Download a file using urlfetch with the given context at the given URL
func downloadFile(ctx context.Context, url string) []byte {
	client := urlfetch.Client(ctx)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		log.Errorf(ctx, err.Error())
	}
	res, resErr := client.Do(req)
	if resErr != nil {
		log.Errorf(ctx, resErr.Error())
	}
	file, readErr := ioutil.ReadAll(res.Body)
	if readErr != nil {
		log.Errorf(ctx, readErr.Error())
	}
    return file
}

func getImageUrlsInDirectory(ctx context.Context, directory string, ch chan []string) {
	urls := make([]string, 0, 0)
    body := downloadFile(ctx, directory)
	c := make(map[string]interface{})
	json.Unmarshal(body, &c)

	// get items as an array of maps, in which
	// mediaLink coorresponds to the url to the download link
	items := c["items"].([]interface{})
	for _, item := range items {
		itemMap := item.(map[string]interface{})
		urls = append(urls, itemMap["mediaLink"].(string))
	}
	ch <- urls
    sem.Release()
}

func initiateRequests(ctx context.Context, directoryUrls []string, c chan []string) {
	for _, directory := range directoryUrls {
        sem.Acquire()
        //log.Infof(ctx, "Starting request for: %s\n", directory)
		go getImageUrlsInDirectory(ctx, directory, c)
	}
}

func getImageUrls(ctx context.Context, directoryUrls []string) []string {
	urls := make([]string, 0, 0)
	c := make(chan []string)

    go initiateRequests(ctx, directoryUrls, c)
	for range directoryUrls {
		urls = append(urls, <-c...)
	}

	return urls
}

func getLatLngFromAddress(ctx context.Context, address string) (float64, float64) {
	client := urlfetch.Client(ctx)
	mapsClient, err := maps.NewClient(maps.WithAPIKey(apiKey), maps.WithHTTPClient(client))
	if err != nil {
		log.Errorf(ctx, "Failed to create client: %v", err)
	}

	request := &maps.GeocodingRequest{
		Address: address,
	}
	res, _ := mapsClient.Geocode(ctx, request)

	lat := res[0].Geometry.Location.Lat
	lng := res[0].Geometry.Location.Lng
	return lat, lng
}

// Count the amount of sentinel images available for the given cells
func getImageCountFromCells(ctx context.Context, cells []s2.Cell) int {
    c := make(chan int)
    count := 0
    
    // concurrently retrieve image count for each cell
    for _, cell := range cells {
        go func(cell s2.Cell) {
            bounds := cell.RectBound()
            lo := bounds.Lo()
            hi := bounds.Hi()
            urls := getUrlsBetweenCoords(ctx, hi.Lat.Degrees(), lo.Lat.Degrees(), 
                                          hi.Lng.Degrees(), lo.Lng.Degrees())
            c<-len(urls)
        } (cell)
    }

    for range cells {
        count += <-c
    }

    return count
}

// Since google appengine inexplicably will not compile when using
// json.SetEscapeHTML(true), we've had to made our own version, where
// we temporarily encode the json to a buffer, and replace escaped characters
// with their unescaped counterpart
func safeMarshalJSON(imageUrls []string) string {
	var b bytes.Buffer
	writer := bufio.NewWriter(&b)
	encoder := json.NewEncoder(writer)
	encoder.Encode(imageUrls)

	arr := b.Bytes()

	arr = bytes.Replace(arr, []byte("\\u003c"), []byte("<"), -1)
	arr = bytes.Replace(arr, []byte("\n"), []byte("<"), -1)
	arr = bytes.Replace(arr, []byte("\\u003e"), []byte(">"), -1)
	arr = bytes.Replace(arr, []byte("\\u0026"), []byte("&"), -1)

	return string(arr)
}

func imageHandler(w http.ResponseWriter, r *http.Request) {
	ctx := appengine.NewContext(r)
	var lat, lng float64
	// if param is an address, get latlng from from google geocode api
    if address := r.FormValue("address"); address == "" {
        lat, _ = strconv.ParseFloat(r.FormValue("lat"), 64)
        lng, _ = strconv.ParseFloat(r.FormValue("lng"), 64)
	} else {
		lat, lng = getLatLngFromAddress(ctx, address)
	}

    mgrs := GetMgrsFromCoords(lat, lng)
	urls := getUrlsFromMgrs(ctx, mgrs)
	imageUrls := getImageUrls(ctx, urls)

	data := safeMarshalJSON(imageUrls)
	fmt.Fprint(w, data)
}

func areaHandler(w http.ResponseWriter, r *http.Request) {
	ctx := appengine.NewContext(r)

    northLat, _ := strconv.ParseFloat(r.FormValue("north_lat"), 64)
    southLat, _ := strconv.ParseFloat(r.FormValue("south_lat"), 64)
    eastLng, _ := strconv.ParseFloat(r.FormValue("east_lng"), 64)
    westLng, _ := strconv.ParseFloat(r.FormValue("west_lng"), 64)

	urls := getUrlsBetweenCoords(ctx, northLat, southLat, eastLng, westLng)
	imageUrls := getImageUrls(ctx, urls)
	data := safeMarshalJSON(imageUrls)
	fmt.Fprint(w, data)
}

func testHandler(w http.ResponseWriter, r *http.Request) {
	ctx := appengine.NewContext(r)
    vars := mux.Vars(r)

    var urls []string
    if vars["case"] == "address" {
        address := "Rued Langgaards Vej,7,2300,København S"
        lat, lng := getLatLngFromAddress(ctx, address)
        mgrs := GetMgrsFromCoords(lat,lng)
        urls = getUrlsFromMgrs(ctx, mgrs)
    } else if vars["case"] == "coords" {
        mgrs := GetMgrsFromCoords(37.4224764, -122.0842499)
        urls = getUrlsFromMgrs(ctx, mgrs)
    } else if vars["case"] == "area" {
        urls = getUrlsBetweenCoords(ctx, -2.89, -6.55, 29.63, 25.93)
    } else {
        log.Criticalf(ctx, "Bad testcase: %s\n", vars["case"])
    }

	imageUrls := getImageUrls(ctx, urls)
	data := safeMarshalJSON(imageUrls)
	fmt.Fprint(w, data)
}


func polyHandler(w http.ResponseWriter, r *http.Request) {
	ctx := appengine.NewContext(r)
    vars := mux.Vars(r)

    region := vars["region"]
    country := vars["country"]
    if region == "" || country == "" {
        log.Criticalf(ctx, "Bad or missing region/country")
    } 

    url := fmt.Sprintf("http://download.geofabrik.de/%s/%s.poly", region, country)
    file := downloadFile(ctx, url)
    polygons := ParsePolyFile(bytes.NewReader(file))
    cells := CellsFromPolygons(polygons)

    count := getImageCountFromCells(ctx, cells)

    fmt.Fprint(w, "Amount of images in region: ", count)
}

func init() {
    //projectID = "ecly-178408"
	r := mux.NewRouter()
	r.HandleFunc("/images", imageHandler)
	r.HandleFunc("/images/area", areaHandler)
	r.HandleFunc("/test/{case}", testHandler)
	r.HandleFunc("/poly/{region}/{country}", polyHandler)
	http.Handle("/", r)
}
