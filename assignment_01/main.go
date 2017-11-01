package app

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math"
	"net/http"
	"strconv"

	"cloud.google.com/go/bigquery"
	"github.com/gorilla/mux"
	"github.com/im7mortal/UTM"
	"golang.org/x/net/context"
	"google.golang.org/api/iterator"
	"google.golang.org/appengine"
	"google.golang.org/appengine/log"
	"google.golang.org/appengine/urlfetch"
	"googlemaps.github.io/maps"
)

var e100kLetters []string = []string{"ABCDEFGH", "JKLMNPQR", "STUVWXYZ"}
var n100kLetters []string = []string{"ABCDEFGHJKLMNPQRSTUV", "FGHJKLMNPQRSTUVABCDE"}
var storageApiUrl = "https://www.googleapis.com/storage/v1/b/gcp-public-data-sentinel-2/o?prefix="
var googleGeoApiUrl = "https://maps.googleapis.com/maps/api/geocode/json?sensor=false&address="
var apiKey = "AIzaSyBfbOhnMrQFj0BUHWA4EABJMW8qIts49WU"

//https://gis.stackexchange.com/questions/15608/how-to-calculate-the-utm-latitude-band
var UTMzdlChars []rune = []rune("CDEFGHJKLMNPQRSTUVWXX")

type QueryResult struct {
	Granule_id string
	Base_url   string
}

type GoogleGeocodeResponse struct {
	Results []struct {
		Geometry struct {
			Location struct {
				Lat float64
				Lng float64
			}
		}
	}
}

//http://www.movable-type.co.uk/scripts/latlong-utm-mgrs.html
func toMgrs(coord UTM.Coordinate, band rune) string {
	// MGRS zone is same as UTM zone
	var zone = coord.ZoneNumber
	var col = int(math.Floor(coord.Easting / 100e3))
	var e100k = e100kLetters[(zone-1)%3][col-1 : col]
	var row = int(math.Floor(coord.Northing/100e3)) % 20
	var n100k = n100kLetters[(zone-1)%2][row : row+1]

	var zoneString string
	if zone < 10 {
		zoneString = "0" + strconv.Itoa(zone)
	} else {
		zoneString = strconv.Itoa(zone)
	}
	return zoneString + string(band) + e100k + n100k
}

func getUTM(lat float64, lng float64) UTM.Coordinate {
	latLon := UTM.LatLon{
		Latitude:  lat,
		Longitude: lng,
	}
	result, err := latLon.FromLatLon()
	if err != nil {
		panic(err.Error())
	}
	return result
}

func getBand(lat float64) rune {
	return UTMzdlChars[int(math.Floor((lat+80)/8))]
}

// From a query result, formulate an url to the google storage api
// for the folder of the QueryResult
func formatUrl(result QueryResult) string {
	return fmt.Sprintf("%s%s/GRANULE/%s/IMG_DATA/", storageApiUrl,
		result.Base_url[32:], result.Granule_id)
}

func getUrlsFromMgrs(mgrs string, ctx context.Context) []string {
	// Create a BigQuery client for the given projectID
	// - the projectID needs to have permissions to use BigQuery
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
		var value QueryResult
		err := it.Next(&value)
		if err == iterator.Done || err != nil {
			break
		}
		urls = append(urls, formatUrl(value))
	}
	return urls
}

func getImageUrlsInDirectory(directory string, ch chan []string, ctx context.Context) {
	urls := make([]string, 0, 0)
	client := urlfetch.Client(ctx)

	req, err := http.NewRequest(http.MethodGet, directory, nil)
	if err != nil {
		log.Errorf(ctx, err.Error())
	}
	res, resErr := client.Do(req)
	if resErr != nil {
		log.Errorf(ctx, resErr.Error())
	}
	body, readErr := ioutil.ReadAll(res.Body)
	if readErr != nil {
		log.Errorf(ctx, readErr.Error())
	}
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
}

func getImageUrls(directoryUrls []string, ctx context.Context) []string {
	urls := make([]string, 0, 0)
	c := make(chan []string)
	for _, directory := range directoryUrls {
		go getImageUrlsInDirectory(directory, c, ctx)
	}
	for range directoryUrls {
		urls = append(urls, <-c...)
	}

	return urls
}

func getLatLngFromAddress(address string, ctx context.Context) (float64, float64) {
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

// Since google appengine inexplicably will not compile when using
// json.SetEscapeHTML(true), we've had to made our own version, where
// we temporarily encode the json to a buffer, and replace escaped characters
// with their unescaped counterpart
func safeMarshalJson(imageUrls []string) string {
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
	address := r.FormValue("address")
	if address == "" {
		lat, _ = strconv.ParseFloat(r.FormValue("lat"), 64)
		lng, _ = strconv.ParseFloat(r.FormValue("lng"), 64)
	} else {
		lat, lng = getLatLngFromAddress(address, ctx)
	}

	utm := getUTM(lat, lng)
	band := getBand(lat)
	mgrs := toMgrs(utm, band)
	urls := getUrlsFromMgrs(mgrs, ctx)
	imageUrls := getImageUrls(urls, ctx)

	data := safeMarshalJson(imageUrls)
	fmt.Fprint(w, data)
}

func init() {
	r := mux.NewRouter()
	r.HandleFunc("/images", imageHandler)
	http.Handle("/", r)
}
