package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"

	"cloud.google.com/go/bigquery"
	"github.com/gorilla/mux"
	"github.com/im7mortal/UTM"
	"google.golang.org/api/iterator"
)

var projectID string
var e100kLetters []string = []string{"ABCDEFGH", "JKLMNPQR", "STUVWXYZ"}
var n100kLetters []string = []string{"ABCDEFGHJKLMNPQRSTUV", "FGHJKLMNPQRSTUVABCDE"}

//http://www.movable-type.co.uk/scripts/latlong-utm-mgrs.html
func toMgrs(coord UTM.Coordinate, band rune) (string, string) {
	// MGRS zone is same as UTM zone
	var zone = coord.ZoneNumber
	var col = int(math.Floor(coord.Easting / 100e3))
	var e100k = e100kLetters[(zone-1)%3][col-1 : col]
	var row = int(math.Floor(coord.Northing/100e3)) % 20
	var n100k = n100kLetters[(zone-1)%2][row : row+1]

	return e100k, n100k
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

var baseUrl = "https://console.cloud.google.com/storage/browser/"

type Result struct {
	Granule_id string
	Base_url   string
}

func getUrlsFromMgrs(mgrs string) {
	ctx := context.Background()

	// Creates a client.
	client, err := bigquery.NewClient(ctx, projectID)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	q := client.Query(fmt.Sprintf(`
			SELECT granule_id, base_url
			FROM %sbigquery-public-data.cloud_storage_geo_index.sentinel_2_index%s
			WHERE (mgrs_tile LIKE '%s%s')
		`, "`", "`", mgrs, "%"))
	fmt.Println(q.Q)

	it, err := q.Read(ctx)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
		// TODO: Handle error.
	}

	///urls := make([]string, 0, 0)
	for {
		var values Result
		err := it.Next(&values)
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
			// TODO: Handle error.
		}
		fmt.Println(values)
	}
}

func imageHandler(w http.ResponseWriter, r *http.Request) {
	lat, _ := strconv.ParseFloat(r.FormValue("lat"), 64)
	lng, _ := strconv.ParseFloat(r.FormValue("lng"), 64)
	utm := getUTM(lat, lng)
	band := getBand(lat)
	e100k, n100k := toMgrs(utm, band)

	fmt.Fprintf(w, "UTM: %d, Band: %c, e100k: %s, n100k: %s", utm.ZoneNumber, band, e100k, n100k)
}

func main() {
	projectID = "ecly-178408"
	//projectID = os.Getenv("")
	r := mux.NewRouter()
	r.HandleFunc("/images", imageHandler)
	http.Handle("/", r)
	getUrlsFromMgrs("05MKP")
}
