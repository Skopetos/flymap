// Command server runs the Air Traffic Tracker backend: it serves the static
// frontend and proxies/caches the adsb.lol and adsbdb.com APIs.
package main

import (
	"log"
	"net/http"
	"os"

	"flymap/internal/api"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	srv := api.NewServer()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/aircraft", srv.Aircraft)
	mux.HandleFunc("GET /api/route/{callsign}", srv.Route)
	mux.Handle("GET /", http.FileServer(http.Dir("web")))

	addr := ":" + port
	log.Printf("listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}
