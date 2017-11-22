package main

import (
    "github.com/golang/geo/s2"
    "fmt"
    "log"
    "strconv"
    "strings"
    "bufio"
    "os"
)

// ParsePolyFile ... 
func ParsePolyFile(filename string) [][]s2.Point{
    polygons := make([][]s2.Point, 0)
    if file, err := os.Open(filename); err == nil {
        defer file.Close()

        scanner := bufio.NewScanner(file)

        polygon := make([]s2.Point, 0)
        for scanner.Scan() {
            words := strings.Fields(scanner.Text())
            if len(words) == 1 {
                if len(polygon) != 0 {
                    polygons = append(polygons, polygon)
                    polygon = make([]s2.Point, 0)
                }
            } else { 
                x, _ := strconv.ParseFloat(words[0], 64)
                y, _ := strconv.ParseFloat(words[1], 64)
                p := s2.PointFromLatLng(s2.LatLngFromDegrees(x, y))
                polygon = append(polygon, p)
            }
        }

        if scanErr := scanner.Err(); err != nil {
            log.Fatal(scanErr)
        }
    } else {
        log.Fatal(err)
    }
    return polygons
}


// CellsFromPolygons ...
func CellsFromPolygons(polygons [][]s2.Point) []s2.Cell {
    cells := make([]s2.Cell, 0)
    for _, points := range polygons {
        l1 := s2.LoopFromPoints(points) 
        loops := []*s2.Loop{l1} 
        poly := s2.PolygonFromLoops(loops) 
        rc := &s2.RegionCoverer{MaxLevel: 30, MaxCells: 100}
        cover := rc.Covering(poly)
        for _, cid := range cover {
            cells = append(cells, s2.CellFromCellID(cid))
            cell := s2.CellFromCellID(cid)
            fmt.Printf("lob:%v, hib:%v\n", cell.RectBound().Lo(), cell.RectBound().Hi())
        }
    }
    return cells
}

func main() {
    polygons := ParsePolyFile("denmark.poly")
    _ = CellsFromPolygons(polygons)
    //fmt.Printf("%v", region)
}
