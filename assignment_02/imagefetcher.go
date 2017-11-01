package main

import (
    "net/http"
    "io/ioutil"
    "encoding/xml"
    "fmt"
)

type ImageInformation struct { 
    GranuleList []Granule `xml:"General_Info>Product_Info>Product_Organisation>Granule_List>Granule"`
    SpectralInfo []SpectralInformation `xml:"General_Info>Product_Image_Characteristics>Spectral_Information_List>Spectral_Information"`

}
type Granule struct {
    StripID     string `xml:"datastripIdentifier,attr"`
    GranuleID   string `xml:"granuleIdentifier,attr"`
    Images      []string `xml:"IMAGE_ID"`
    ImageFiles  []string `xml:"IMAGE_FILES"`
} 


type SpectralInformation struct{
    PhysicalBand    string `xml:"physicalBand,attr"`
    BandID          string `xml:"bandId,attr"`
    Wavelengths     []struct {
        Min     float64 `xml:"MIN"`
        Max     float64 `xml:"MAX"`
        Central float64 `xml:"CENTRAL"`
    } `xml:"Wavelength"`
}

//https://stackoverflow.com/questions/42717716/reading-xml-from-http-get-in-golang
func getContent(url string) ([]byte, error) {
    resp, err := http.Get(url)
    if err != nil {
        return nil, fmt.Errorf("GET error: %v", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        return nil, fmt.Errorf("Status error: %v", resp.StatusCode)
    }

    data, err := ioutil.ReadAll(resp.Body)
    if err != nil {
        return nil, fmt.Errorf("Read body: %v", err)
    }

    return data, nil
}


func GetImageInformationFromURL(url string) (ImageInformation, error) {
    data, urlErr := getContent(url)
    //afmt.Println(string(data))
    if urlErr != nil {
        fmt.Println(urlErr)
        return ImageInformation{}, urlErr
    } else {
        var info ImageInformation
        xmlErr := xml.Unmarshal(data, &info)
        return info, xmlErr
    }

func main(){
    info, _ := GetImageInformationFromURL("https://www.googleapis.com/download/storage/v1/b/gcp-public-data-sentinel-2/o/tiles%2F13%2FX%2FDC%2FS2A_MSIL1C_20160331T194310_N0201_R042_T13XDC_20160401T080133.SAFE%2FS2A_OPER_MTD_SAFL1C_PDMC_20160401T080133_R042_V20160331T194310_20160331T194310.xml?alt=media")
    fmt.Println(info)
}
