package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
)

const apiKey string = "366ec2003acfda8339c16cd079ad601e"
const ipStackURL string = "http://api.ipstack.com/"
const torExitURL string = "https://check.torproject.org/torbulkexitlist"

var isExitNodeIP map[string]bool

// GeoData for the json
type GeoData struct {
	ContinentCode string `json:"continent_code"`
	CountryCode   string `json:"country_code"`
	City          string `json:"city"`
	TorExitNode   bool
}

// SetupExitNodeMap setups of the exit node map
func SetupExitNodeMap() error {
	// Make map
	isExitNodeIP = make(map[string]bool)

	// Create HTTP request with the IP address
	req, err := http.NewRequest("GET", torExitURL, nil)
	if err != nil {
		debugPrint(fmt.Sprintf("Error creating API Request: %v", err))
		return err
	}

	// Create HTTP client and submit request
	client := &http.Client{}
	resp, err := client.Do(req)

	if err != nil {
		debugPrint(fmt.Sprintf("Error sending TOR API Request: %v", err))
		return err
	}

	respBody, err := ioutil.ReadAll(resp.Body)

	if err != nil {
		debugPrint(fmt.Sprintf("Error reading TOR API Request Body: %v", err))
		return err
	}

	// Turn byte array into string
	respStr := string(respBody)

	// Turn into list
	temp := strings.Split(respStr, "\n")

	// Finally, turn into map
	for _, val := range temp {
		if val != "" {
			trimmedVal := strings.TrimSpace(val)
			isExitNodeIP[trimmedVal] = true
			// fmt.Println(trimmedVal)
		}
	}

	return nil
}

// IsTorExitNode returns true if it's an exit node
func isTorExitNode(ip string) bool {
	_, exists := isExitNodeIP[ip]
	if exists {
		return true
	}
	return false
}

// GetGeoData returns geographical data for an IP address
func GetGeoData(ip string) GeoData {
	// Create default GeoData
	geoData := GeoData{
		ContinentCode: "unk",
		CountryCode:   "unk",
		City:          "unk",
		TorExitNode:   isTorExitNode(ip),
	}

	// Create HTTP request with the IP address
	req, err := http.NewRequest("GET", ipStackURL+ip, nil)
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
