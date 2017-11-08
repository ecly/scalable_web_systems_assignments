package app

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"

	"cloud.google.com/go/bigquery"
	"github.com/gorilla/mux"
	"golang.org/x/net/context"
	"google.golang.org/api/iterator"
	"google.golang.org/appengine"
	"google.golang.org/appengine/log"
	"google.golang.org/appengine/urlfetch"
	"googlemaps.github.io/maps"
)

var storageAPIURL = "https://www.googleapis.com/storage/v1/b/gcp-public-data-sentinel-2/o?prefix="
var googleGeoAPIURL = "https://maps.googleapis.com/maps/api/geocode/json?sensor=false&address="
var apiKey = "AIzaSyBfbOhnMrQFj0BUHWA4EABJMW8qIts49WU"


type queryResult struct {
	GranuleID string
	BaseURL   string
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
		result.BaseURL[32:], result.GranuleID)
}


func getUrlsBetweenCoords(ctx context.Context, lat1 float64, lng1 float64, lat2 float64, lng2 float64) []string {
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
            WHERE north_lat <= %f AND south_lat >= %f
            AND west_lon >= %f AND east_lon <= %f
			`, "`", "`", "%", lat1, lat2, lng1, lng2))

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

func getUrlsFromMgrs(ctx context.Context, mgrs string) []string {
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
		var value queryResult
		err := it.Next(&value)
		if err == iterator.Done || err != nil {
			break
		}
		urls = append(urls, formatURL(value))
	}
	return urls
}


func getImageUrlsInDirectory(ctx context.Context, directory string, ch chan []string) {
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

func getImageUrls(ctx context.Context, directoryUrls []string) []string {
	urls := make([]string, 0, 0)
	c := make(chan []string)
	for _, directory := range directoryUrls {
		go getImageUrlsInDirectory(ctx, directory, c)
	}
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
	address := r.FormValue("address")
	if address == "" {
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

func init() {
	r := mux.NewRouter()
	r.HandleFunc("/images", imageHandler)
	http.Handle("/", r)
}
