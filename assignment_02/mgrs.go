package app

import (
	"math"
	"strconv"
	"github.com/im7mortal/UTM"
)

//https://gis.stackexchange.com/questions/15608/how-to-calculate-the-utm-latitude-band
var utmZdlChars = []rune("CDEFGHJKLMNPQRSTUVWXX")
var e100kLetters = []string{"ABCDEFGH", "JKLMNPQR", "STUVWXYZ"}
var n100kLetters = []string{"ABCDEFGHJKLMNPQRSTUV", "FGHJKLMNPQRSTUVABCDE"}

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
	return utmZdlChars[int(math.Floor((lat+80)/8))]
}

// GetMgrsFromCoords generates MGRS coords with max granularity based on Lat/Lng coords
func GetMgrsFromCoords(lat float64, lng float64) string {
	utm := getUTM(lat, lng)
	band := getBand(lat)
	mgrs := toMgrs(utm, band)
    return mgrs
}

func GetAllTilesBetweenCoords(lat1 float64, lng1 float64, lat2 float64, lng2 float64) []string{
    return nil
}

