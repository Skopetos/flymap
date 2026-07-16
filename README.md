# Air Traffic Tracker

A clean, fast, browser-based live air traffic map. No ads, no clutter, no login — just planes on a map, updating smoothly.

Shows live aircraft around a location (default: Athens, Greece) on a dark, instrument-like Leaflet map. Click a plane to see its airline, route, altitude, speed, and heading.

## Quickstart

```bash
go run ./cmd/server
```

Open `http://localhost:8080`. Planes should appear within ~5 seconds and glide between updates.

Optional: set `PORT` to run on something other than 8080.

```bash
PORT=3000 go run ./cmd/server
```

## Project layout

```
/cmd/server/main.go       entrypoint: wires routes, serves /web
/internal/api/handlers.go proxies + trims adsb.lol and adsbdb.com responses
/internal/cache/cache.go  in-memory TTL cache (no external deps)
/web/                     static frontend (vanilla JS + Leaflet, no build step)
```

No database, no Docker, no frontend framework or bundler — stdlib Go on the backend, ES modules on the frontend.

## Requirements

- Go 1.22+ (uses stdlib `net/http` method/path routing)
- A browser. No API keys needed — both upstream data sources are free.

See [docs.md](docs.md) for architecture, API reference, and design notes.
