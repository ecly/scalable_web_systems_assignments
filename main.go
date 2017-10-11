package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"net/http"
	"strconv"

	"cloud.google.com/go/bigquery"
	"github.com/gorilla/mux"
	"github.com/im7mortal/UTM"
	geo "github.com/martinlindhe/google-geolocate"
	"google.golang.org/api/iterator"
)

var projectID string
var e100kLetters []string = []string{"ABCDEFGH", "JKLMNPQR", "STUVWXYZ"}
var n100kLetters []string = []string{"ABCDEFGHJKLMNPQRSTUV", "FGHJKLMNPQRSTUVABCDE"}
var baseUrl = "https://www.googleapis.com/storage/v1/b/gcp-public-data-sentinel-2/o?prefix="
var googleApi = "https://maps.googleapis.com/maps/api/geocode/json?address="
var apiKey = "AIzaSyBfbOhnMrQFj0BUHWA4EABJMW8qIts49WU"

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

//https://gis.stackexchange.com/questions/15608/how-to-calculate-the-utm-latitude-band
var UTMzdlChars []rune = []rune("CDEFGHJKLMNPQRSTUVWXX")

func getBand(lat float64) rune {
	return UTMzdlChars[int(math.Floor((lat+80)/8))]
}

type QueryResult struct {
	Granule_id string
	Base_url   string
}

func formatUrl(result QueryResult) string {
	return fmt.Sprintf("%s%s/GRANULE/%s/IMG_DATA/", baseUrl,
		result.Base_url[32:], result.Granule_id)
}

func getUrlsFromMgrs(mgrs string) []string {
	ctx := context.Background()
	client, err := bigquery.NewClient(ctx, projectID)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	q := client.Query(fmt.Sprintf(`
			SELECT granule_id, base_url
			FROM %sbigquery-public-data.cloud_storage_geo_index.sentinel_2_index%s
			WHERE (mgrs_tile LIKE '%s%s')
			`, "`", "`", mgrs, "%"))
	//fmt.Println(q.Q)

	it, err := q.Read(ctx)
	if err != nil {
		log.Fatalf("Query failed to execute: %v", err)
	}

	urls := make([]string, 0, 0)
	for {
		var value QueryResult
		err := it.Next(&value)
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		urls = append(urls, formatUrl(value))
	}
	return urls
}

func getImageUrlsInDirectory(directory string, ch chan []string) {
	urls := make([]string, 0, 0)
	client := http.Client{}

	req, err := http.NewRequest(http.MethodGet, directory, nil)
	if err != nil {
		log.Fatal(err)
	}
	res, resErr := client.Do(req)
	if resErr != nil {
		log.Fatal(resErr)
	}
	body, readErr := ioutil.ReadAll(res.Body)
	if readErr != nil {
		log.Fatal(readErr)
	}
	c := make(map[string]interface{})
	json.Unmarshal(body, &c)
	items := c["items"].([]interface{})
	for _, item := range items {
		itemMap := item.(map[string]interface{})
		urls = append(urls, itemMap["mediaLink"].(string))
	}
	ch <- urls
}

func getImageUrls(directoryUrls []string) []string {
	urls := make([]string, 0, 0)
	c := make(chan []string)
	for _, directory := range directoryUrls {
		go getImageUrlsInDirectory(directory, c)
	}
	for range directoryUrls {
		urls = append(urls, <-c...)
	}

	return urls
}

func getLatLngFromAddress(address string) (float64, float64) {
	client := geo.NewGoogleGeo(apiKey)
	res, _ := client.Geocode(address)
	return res.Lat, res.Lng
}

func imageHandler(w http.ResponseWriter, r *http.Request) {
	var lat, lng float64
	address := r.FormValue("address")
	if address == "" {
		lat, _ = strconv.ParseFloat(r.FormValue("lat"), 64)
		lng, _ = strconv.ParseFloat(r.FormValue("lng"), 64)
	} else {
		lat, lng = getLatLngFromAddress(address)
	}
	utm := getUTM(lat, lng)
	band := getBand(lat)
	mgrs := toMgrs(utm, band)
	urls := getUrlsFromMgrs(mgrs)
	imageUrls := getImageUrls(urls)

	encoder := json.NewEncoder(w)
	//avoid escape &
	encoder.SetEscapeHTML(false)
	encoder.Encode(imageUrls)
}

func main() {
	projectID = "ecly-178408"
	//projectID = os.Getenv("")
	r := mux.NewRouter()
	r.HandleFunc("/images", imageHandler)
	r.HandleFunc("/images", imageHandler)
	http.Handle("/", r)
	//getUrlsFromMgrs("05MKP")
	if err := http.ListenAndServe("127.0.0.1:8080", nil); err != nil {
		log.Fatal(err)
	}
}
