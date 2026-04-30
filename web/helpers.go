package web

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/oschwald/maxminddb-golang"
	log "github.com/sirupsen/logrus"
	"github.com/umahmood/haversine"

	"github.com/librespeed/speedtest-go/config"
	"github.com/librespeed/speedtest-go/results"
)

var (
	serverCoord haversine.Coord
)

func getRandomData(length int) []byte {
	data := make([]byte, length)
	if _, err := rand.Read(data); err != nil {
		log.Fatalf("Failed to generate random data: %s", err)
	}
	return data
}

func getIPInfoURL(address string) string {
	apiKey := config.LoadedConfig().IPInfoAPIKey

	ipInfoURL := `https://ipinfo.io/%s/json`
	if address != "" {
		ipInfoURL = fmt.Sprintf(ipInfoURL, address)
	} else {
		ipInfoURL = "https://ipinfo.io/json"
	}

	if apiKey != "" {
		ipInfoURL += "?token=" + apiKey
	}

	return ipInfoURL
}

func getIPInfo(addr string) results.IPInfoResponse {
	var ret results.IPInfoResponse
	resp, err := http.DefaultClient.Get(getIPInfoURL(addr))
	if err != nil {
		log.Errorf("Error getting response from ipinfo.io: %s", err)
		return ret
	}

	raw, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Errorf("Error reading response from ipinfo.io: %s", err)
		return ret
	}
	defer resp.Body.Close()

	if err := json.Unmarshal(raw, &ret); err != nil {
		log.Errorf("Error parsing response from ipinfo.io: %s", err)
	}

	return ret
}

func SetServerLocation(conf *config.Config) {
	if conf.ServerLat != 0 || conf.ServerLng != 0 {
		log.Infof("Configured server coordinates: %.6f, %.6f", conf.ServerLat, conf.ServerLng)
		serverCoord.Lat = conf.ServerLat
		serverCoord.Lon = conf.ServerLng
		return
	}

	var ret results.IPInfoResponse
	resp, err := http.DefaultClient.Get(getIPInfoURL(""))
	if err != nil {
		log.Errorf("Error getting repsonse from ipinfo.io: %s", err)
		return
	}
	raw, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Errorf("Error reading response from ipinfo.io: %s", err)
		return
	}
	defer resp.Body.Close()

	if err := json.Unmarshal(raw, &ret); err != nil {
		log.Errorf("Error parsing response from ipinfo.io: %s", err)
		return
	}

	if ret.Location != "" {
		serverCoord, err = parseLocationString(ret.Location)
		if err != nil {
			log.Errorf("Cannot get server coordinates: %s", err)
			return
		}
	}

	log.Infof("Fetched server coordinates: %.6f, %.6f", serverCoord.Lat, serverCoord.Lon)
}

func parseLocationString(location string) (haversine.Coord, error) {
	var coord haversine.Coord

	parts := strings.Split(location, ",")
	if len(parts) != 2 {
		err := fmt.Errorf("unknown location format: %s", location)
		log.Error(err)
		return coord, err
	}

	lat, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		log.Errorf("Error parsing latitude: %s", parts[0])
		return coord, err
	}

	lng, err := strconv.ParseFloat(parts[1], 64)
	if err != nil {
		log.Errorf("Error parsing longitude: %s", parts[0])
		return coord, err
	}

	coord.Lat = lat
	coord.Lon = lng

	return coord, nil
}

func calculateDistance(clientLocation string, unit string) string {
	clientCoord, err := parseLocationString(clientLocation)
	if err != nil {
		log.Errorf("Error parsing client coordinates: %s", err)
		return ""
	}

	dist, km := haversine.Distance(clientCoord, serverCoord)

	switch unit {
	case "km":
		dist = km
		rounded := roundToNearest10(dist)
		if dist < 20 {
			return "<20 km"
		}
		return fmt.Sprintf("%.0f km", rounded)
	case "NM":
		dist = km * 0.539957
		return fmt.Sprintf("%.2f NM", dist)
	default: // miles
		distMi := dist
		rounded := roundToNearest10(distMi)
		if distMi < 15 {
			return "<15 mi"
		}
		return fmt.Sprintf("%.0f mi", rounded)
	}
}

// roundToNearest10 rounds a float64 to the nearest 10, matching PHP round($d, -1)
func roundToNearest10(val float64) float64 {
	return float64(int64(val/10+0.5)) * 10
}

// GeoIP database holder (lazily opened on first use)
var (
	geoIPReader *maxminddb.Reader
	geoIPOpened bool
)

// getGeoIPData looks up the given IP in the configured GeoIP .mmdb database
// and returns ISP and country information if available.
// It returns nil if GeoIP is not configured or the lookup fails.
func getGeoIPData(ipStr string) *struct {
	ASName      string
	CountryName string
} {
	conf := config.LoadedConfig()
	if conf.GeoIPDatabaseFile == "" {
		return nil
	}

	if !geoIPOpened {
		geoIPOpened = true
		if _, err := os.Stat(conf.GeoIPDatabaseFile); os.IsNotExist(err) {
			log.Warnf("GeoIP database file not found: %s", conf.GeoIPDatabaseFile)
			return nil
		}
		reader, err := maxminddb.Open(conf.GeoIPDatabaseFile)
		if err != nil {
			log.Warnf("Failed to open GeoIP database: %s", err)
			return nil
		}
		geoIPReader = reader
	}

	if geoIPReader == nil {
		return nil
	}

	ip := net.ParseIP(ipStr)
	if ip == nil {
		return nil
	}

	// Try ipinfo.io offline database format first
	var ipinfoResult map[string]interface{}
	if err := geoIPReader.Lookup(ip, &ipinfoResult); err != nil {
		log.Warnf("GeoIP lookup failed: %s", err)
		return nil
	}

	if len(ipinfoResult) == 0 {
		return nil
	}

	result := &struct {
		ASName      string
		CountryName string
	}{}

	// ipinfo.io offline format uses "as_name" and "country_name"
	if v, ok := ipinfoResult["as_name"].(string); ok {
		result.ASName = v
	}
	if v, ok := ipinfoResult["country_name"].(string); ok {
		result.CountryName = v
	}

	// If ipinfo format fields are empty, try standard MaxMind GeoIP2 format
	if result.ASName == "" {
		// Try autonomous_system > organization
		if as, ok := ipinfoResult["autonomous_system"].(map[string]interface{}); ok {
			if v, ok := as["organization"].(string); ok {
				result.ASName = v
			}
		}
	}
	if result.CountryName == "" {
		if country, ok := ipinfoResult["country"].(map[string]interface{}); ok {
			if v, ok := country["names"].(map[string]interface{}); ok {
				if n, ok := v["en"].(string); ok {
					result.CountryName = n
				}
			}
		}
		// Fallback: direct "country" string field (as used by some GeoIP DBs)
		if result.CountryName == "" {
			if v, ok := ipinfoResult["country"].(string); ok {
				result.CountryName = v
			}
		}
	}

	if result.ASName == "" && result.CountryName == "" {
		return nil
	}

	return result
}

// getISPInfoByPriority tries to fetch ISP info using the ipinfo.io API first,
// then falls back to the configured offline GeoIP database, mirroring PHP behavior.
func getISPInfoByPriority(addr string) results.IPInfoResponse {
	// First try: ipinfo.io API
	info := getIPInfo(addr)
	if info.Organization != "" || info.Country != "" {
		return info
	}

	// Second try: offline GeoIP database
	geo := getGeoIPData(addr)
	if geo != nil {
		info.Organization = geo.ASName
		info.Country = geo.CountryName
		return info
	}

	// Fallback: empty result (will show IP only)
	return info
}
