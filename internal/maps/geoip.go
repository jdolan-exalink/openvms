package maps

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// Public, key-less IP geolocation. One best-effort call at startup: it only decides where
// the default map view lands, so any failure keeps the Latin America default and nothing
// ever blocks on it. Var (not const) so tests can point it at a stub.
var serverGeoIPURL = "https://freeipapi.com/api/json"

// OPENVMS_MAPS_CENTER="lat,lng" pins the default view for air-gapped deployments where
// the geolocation lookup cannot work.
const centerEnvVar = "OPENVMS_MAPS_CENTER"

// ParseCenter reads a "lat,lng" pair with optional whitespace.
func ParseCenter(raw string) (DefaultCenter, bool) {
	parts := strings.Split(raw, ",")
	if len(parts) != 2 {
		return DefaultCenter{}, false
	}
	lat, errLat := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	lng, errLng := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if errLat != nil || errLng != nil {
		return DefaultCenter{}, false
	}
	return DefaultCenter{Lat: lat, Lng: lng}, true
}

// DetectServerCenter returns where the default map view should land: an explicit
// OPENVMS_MAPS_CENTER override, else the server's public-IP position, else the Latin
// America default. Never fails, never blocks for long.
func DetectServerCenter(ctx context.Context, log *slog.Logger) DefaultCenter {
	if raw := os.Getenv(centerEnvVar); raw != "" {
		if center, ok := ParseCenter(raw); ok {
			log.Info("maps: using configured default center", "lat", center.Lat, "lng", center.Lng)
			return center
		}
		log.Warn("maps: ignoring malformed OPENVMS_MAPS_CENTER", "value", raw)
	}

	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	center, err := detectCenter(ctx, http.DefaultClient, serverGeoIPURL)
	if err != nil {
		log.Info("maps: server geolocation unavailable, keeping the Latin America default", "error", err)
		return DefaultConfig().DefaultCenter
	}
	log.Info("maps: centering the default view on the server location", "lat", center.Lat, "lng", center.Lng)
	return center
}

// detectCenter asks a public geolocation service where this server's public IP is.
// freeipapi answers {"latitude":-31.42,"longitude":-64.18,...}; some services report the
// coordinates as strings, so both shapes are accepted.
func detectCenter(ctx context.Context, client *http.Client, url string) (DefaultCenter, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return DefaultCenter{}, err
	}
	res, err := client.Do(req)
	if err != nil {
		return DefaultCenter{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return DefaultCenter{}, fmt.Errorf("geolocation service answered %d", res.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	if err != nil {
		return DefaultCenter{}, err
	}

	var floats struct {
		Latitude  *float64 `json:"latitude"`
		Longitude *float64 `json:"longitude"`
	}
	if err := json.Unmarshal(body, &floats); err == nil && floats.Latitude != nil && floats.Longitude != nil {
		return DefaultCenter{Lat: *floats.Latitude, Lng: *floats.Longitude}, nil
	}

	var strs struct {
		Latitude  string `json:"latitude"`
		Longitude string `json:"longitude"`
	}
	if err := json.Unmarshal(body, &strs); err == nil {
		lat, latErr := strconv.ParseFloat(strings.TrimSpace(strs.Latitude), 64)
		lng, lngErr := strconv.ParseFloat(strings.TrimSpace(strs.Longitude), 64)
		if latErr == nil && lngErr == nil {
			return DefaultCenter{Lat: lat, Lng: lng}, nil
		}
	}
	return DefaultCenter{}, fmt.Errorf("geolocation payload without usable coordinates")
}
