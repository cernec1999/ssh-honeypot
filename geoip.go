package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
)

const apiKey string = "366ec2003acfda8339c16cd079ad601e"
const url string = "http://api.ipstack.com/"

// GeoData for the json
type GeoData struct {
	ContinentCode string `json:"continent_code"`
	CountryCode   string `json:"country_code"`
	City          string `json:"city"`
}

// GetGeoData returns geographical data for an IP address
func GetGeoData(ip string) GeoData {
	// Create default GeoData
	geoData := GeoData{
		ContinentCode: "unk",
		CountryCode:   "unk",
		City:          "unk",
	}

	// Create HTTP request with the IP address
	req, err := http.NewRequest("GET", url+ip, nil)
	if err != nil {
		debugPrint(fmt.Sprintf("Error creating API Request: %v", err))
		return geoData
	}

	// Add the API token
	q := req.URL.Query()
	q.Add("access_key", apiKey)
	req.URL.RawQuery = q.Encode()

	// Accept json as response
	req.Header.Add("Accept", "application/json")

	// Create HTTP client and submit request
	client := &http.Client{}
	resp, err := client.Do(req)

	if err != nil {
		debugPrint(fmt.Sprintf("Error submitting API Request: %v", err))
		return geoData
	}

	respBody, err := ioutil.ReadAll(resp.Body)

	if err != nil {
		debugPrint(fmt.Sprintf("Error reading response data: %v", err))
		return geoData
	}

	err = json.Unmarshal(respBody, &geoData)
	if err != nil {
		debugPrint(fmt.Sprintf("Unable to get IP data for %s: %v", ip, err))
	}
	return geoData
}
