package main

import (
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/im7mortal/UTM"
)

func getUTM(lat float64, lng float64) int {
	latLon := UTM.LatLon{
		Latitude:  lat,
		Longitude: lng,
	}

	result, err := latLon.FromLatLon()
	if err != nil {
		panic(err.Error())
	}

	return result.ZoneNumber
}

//https://gis.stackexchange.com/questions/15608/how-to-calculate-the-utm-latitude-band
var UTMzdlChars []rune = []rune("CDEFGHJKLMNPQRSTUVWXX")

func getBand(lat float64) rune {
	return UTMzdlChars[int(math.Floor((lat+80)/8))]
}

func imageHandler(w http.ResponseWriter, r *http.Request) {
	lat, _ := strconv.ParseFloat(r.FormValue("lat"), 64)
	lng, _ := strconv.ParseFloat(r.FormValue("lng"), 64)
	utm := getUTM(lat, lng)
	band := getBand(lat)

	fmt.Fprintf(w, "UTM: %d, Band: %c", utm, band)
}

func main() {
	r := mux.NewRouter()

	r.HandleFunc("/images", imageHandler)
	http.Handle("/", r)
	if err := http.ListenAndServe("127.0.0.1:8080", nil); err != nil {
		log.Fatal(err)
	}
}
