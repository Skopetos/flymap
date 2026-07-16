// Package api implements the HTTP handlers that proxy and cache the
// upstream adsb.lol (positions) and adsbdb.com (routes) APIs.
package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"flymap/internal/cache"
)

const (
	aircraftTTL  = 5 * time.Second
	routeTTL     = 24 * time.Hour
	upstreamTO   = 5 * time.Second
	adsbBaseURL  = "https://api.adsb.lol/v2/lat"
	adsbdbURL    = "https://api.adsbdb.com/v0/callsign/"
)

// Aircraft is the trimmed shape returned to the frontend.
type Aircraft struct {
	Hex     string `json:"hex"`
	Flight  string `json:"flight,omitempty"`
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
	AltBaro any     `json:"alt_baro,omitempty"`
	Gs      float64 `json:"gs,omitempty"`
	Track   float64 `json:"track,omitempty"`
	Type    string  `json:"t,omitempty"`
	Reg     string  `json:"r,omitempty"`
}

// AircraftResponse is what /api/aircraft returns.
type AircraftResponse struct {
	Aircraft []Aircraft `json:"aircraft"`
}

type rawAdsbAircraft struct {
	Hex     string   `json:"hex"`
	Flight  string   `json:"flight"`
	Lat     *float64 `json:"lat"`
	Lon     *float64 `json:"lon"`
	AltBaro any      `json:"alt_baro"`
	Gs      float64  `json:"gs"`
	Track   float64  `json:"track"`
	Type    string   `json:"t"`
	Reg     string   `json:"r"`
}

type rawAdsbResponse struct {
	Ac []rawAdsbAircraft `json:"ac"`
}

// Airport is a trimmed origin/destination airport.
type Airport struct {
	IATA string `json:"iata"`
	ICAO string `json:"icao"`
	Name string `json:"name"`
}

// RouteResponse is what /api/route/{callsign} returns.
type RouteResponse struct {
	Known       bool     `json:"known"`
	Callsign    string   `json:"callsign,omitempty"`
	Airline     string   `json:"airline,omitempty"`
	Origin      *Airport `json:"origin,omitempty"`
	Destination *Airport `json:"destination,omitempty"`
}

type rawAdsbdbAirport struct {
	IATACode string `json:"iata_code"`
	ICAOCode string `json:"icao_code"`
	Name     string `json:"name"`
}

type rawAdsbdbResponse struct {
	Response struct {
		Flightroute struct {
			Callsign string `json:"callsign"`
			Airline  struct {
				Name string `json:"name"`
			} `json:"airline"`
			Origin      rawAdsbdbAirport `json:"origin"`
			Destination rawAdsbdbAirport `json:"destination"`
		} `json:"flightroute"`
	} `json:"response"`
}

// Server holds the shared dependencies for the API handlers.
type Server struct {
	client        *http.Client
	aircraftCache *cache.Cache[[]byte]
	routeCache    *cache.Cache[[]byte]
}

// NewServer builds a Server with fresh caches and an HTTP client bound by upstreamTO.
func NewServer() *Server {
	return &Server{
		client:        &http.Client{Timeout: upstreamTO},
		aircraftCache: cache.New[[]byte](),
		routeCache:    cache.New[[]byte](),
	}
}

func writeJSON(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, []byte(fmt.Sprintf(`{"error":%q}`, msg)))
}

// Aircraft handles GET /api/aircraft?lat=&lon=&radius=
func (s *Server) Aircraft(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	lat, errLat := strconv.ParseFloat(q.Get("lat"), 64)
	lon, errLon := strconv.ParseFloat(q.Get("lon"), 64)
	radius, errRadius := strconv.Atoi(q.Get("radius"))
	if errLat != nil || errLon != nil || errRadius != nil || radius <= 0 {
		writeError(w, http.StatusBadRequest, "lat, lon and radius (nm) are required")
		return
	}

	key := fmt.Sprintf("%.2f,%.2f,%d", lat, lon, radius)
	if cached, ok := s.aircraftCache.Get(key); ok {
		writeJSON(w, http.StatusOK, cached)
		return
	}

	upstreamURL := fmt.Sprintf("%s/%.2f/lon/%.2f/dist/%d", adsbBaseURL, lat, lon, radius)
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, upstreamURL, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build upstream request")
		return
	}

	resp, err := s.client.Do(req)
	if err != nil {
		log.Printf("aircraft fetch lat=%.2f lon=%.2f radius=%d: %v", lat, lon, radius, err)
		writeError(w, http.StatusBadGateway, "upstream unavailable")
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != http.StatusOK {
		log.Printf("aircraft fetch lat=%.2f lon=%.2f radius=%d: status=%d err=%v", lat, lon, radius, resp.StatusCode, err)
		writeError(w, http.StatusBadGateway, "upstream unavailable")
		return
	}

	var raw rawAdsbResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		log.Printf("aircraft fetch lat=%.2f lon=%.2f radius=%d: decode error: %v", lat, lon, radius, err)
		writeError(w, http.StatusBadGateway, "upstream returned malformed data")
		return
	}

	out := AircraftResponse{Aircraft: make([]Aircraft, 0, len(raw.Ac))}
	for _, a := range raw.Ac {
		if a.Lat == nil || a.Lon == nil {
			continue
		}
		out.Aircraft = append(out.Aircraft, Aircraft{
			Hex:     a.Hex,
			Flight:  strings.TrimSpace(a.Flight),
			Lat:     *a.Lat,
			Lon:     *a.Lon,
			AltBaro: a.AltBaro,
			Gs:      a.Gs,
			Track:   a.Track,
			Type:    a.Type,
			Reg:     a.Reg,
		})
	}

	encoded, err := json.Marshal(out)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode response")
		return
	}

	log.Printf("aircraft fetch lat=%.2f lon=%.2f radius=%d: %d aircraft", lat, lon, radius, len(out.Aircraft))
	s.aircraftCache.Set(key, encoded, aircraftTTL)
	writeJSON(w, http.StatusOK, encoded)
}

// Route handles GET /api/route/{callsign}
func (s *Server) Route(w http.ResponseWriter, r *http.Request) {
	callsign := strings.ToUpper(strings.TrimSpace(r.PathValue("callsign")))
	if callsign == "" {
		writeError(w, http.StatusBadRequest, "callsign is required")
		return
	}

	if cached, ok := s.routeCache.Get(callsign); ok {
		writeJSON(w, http.StatusOK, cached)
		return
	}

	upstreamURL := adsbdbURL + url.PathEscape(callsign)
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, upstreamURL, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build upstream request")
		return
	}

	resp, err := s.client.Do(req)
	if err != nil {
		log.Printf("route fetch callsign=%s: %v", callsign, err)
		writeError(w, http.StatusBadGateway, "upstream unavailable")
		return
	}
	defer resp.Body.Close()

	var encoded []byte
	if resp.StatusCode == http.StatusNotFound {
		encoded, _ = json.Marshal(RouteResponse{Known: false})
		log.Printf("route fetch callsign=%s: unknown", callsign)
	} else if resp.StatusCode == http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			log.Printf("route fetch callsign=%s: read error: %v", callsign, err)
			writeError(w, http.StatusBadGateway, "upstream unavailable")
			return
		}
		var raw rawAdsbdbResponse
		if err := json.Unmarshal(body, &raw); err != nil {
			log.Printf("route fetch callsign=%s: decode error: %v", callsign, err)
			writeError(w, http.StatusBadGateway, "upstream returned malformed data")
			return
		}
		fr := raw.Response.Flightroute
		if fr.Callsign == "" {
			encoded, _ = json.Marshal(RouteResponse{Known: false})
			log.Printf("route fetch callsign=%s: unknown", callsign)
		} else {
			encoded, _ = json.Marshal(RouteResponse{
				Known:       true,
				Callsign:    fr.Callsign,
				Airline:     fr.Airline.Name,
				Origin:      &Airport{IATA: fr.Origin.IATACode, ICAO: fr.Origin.ICAOCode, Name: fr.Origin.Name},
				Destination: &Airport{IATA: fr.Destination.IATACode, ICAO: fr.Destination.ICAOCode, Name: fr.Destination.Name},
			})
			log.Printf("route fetch callsign=%s: known", callsign)
		}
	} else {
		log.Printf("route fetch callsign=%s: status=%d", callsign, resp.StatusCode)
		writeError(w, http.StatusBadGateway, "upstream unavailable")
		return
	}

	s.routeCache.Set(callsign, encoded, routeTTL)
	writeJSON(w, http.StatusOK, encoded)
}
