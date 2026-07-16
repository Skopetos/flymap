# Documentation

## Architecture

```
Browser (vanilla JS + Leaflet)
        │  /api/aircraft?lat=..&lon=..&radius=..
        │  /api/route/{callsign}
        ▼
Go backend (net/http, stdlib only)
        │  proxies + caches
        ▼
adsb.lol (positions)   adsbdb.com (routes/airline)
```

The Go backend exists for three reasons: avoid CORS issues in the browser, cache responses so the upstream APIs aren't hammered by every client poll, and keep the frontend dead simple (no API keys, no upstream-specific parsing on the client).

## Data sources

Both are free and require no API key.

### Positions — adsb.lol

`GET https://api.adsb.lol/v2/lat/{lat}/lon/{lon}/dist/{radius_nm}`

Returns an `ac` array; the backend keeps only: `hex` (ICAO 24-bit id, the stable key), `flight` (callsign, whitespace-trimmed), `lat`, `lon`, `alt_baro` (feet, or the string `"ground"`), `gs` (knots), `track` (degrees), `t` (type, e.g. `A320`), `r` (registration). Aircraft with no position (`lat`/`lon` missing) are dropped. Polled by the backend at most once per 5s per (lat, lon, radius).

### Routes/airline — adsbdb.com

`GET https://api.adsbdb.com/v0/callsign/{callsign}`

Returns airline name and origin/destination airport (IATA/ICAO + name). Many callsigns — military, private, charter — have no known route; the backend treats a 404 (or a response with an empty `flightroute.callsign`) as unknown rather than an error.

## Backend API

### `GET /api/aircraft?lat=&lon=&radius=`

- `lat`, `lon`: decimal degrees. `radius`: nautical miles, integer > 0.
- All three are required; a missing/invalid value returns `400`.
- Cache key is `lat,lon` rounded to 2 decimals plus `radius`; TTL 5s.
- Response:

```json
{
  "aircraft": [
    {
      "hex": "3964e8",
      "flight": "TVF61MQ",
      "lat": 37.433075,
      "lon": 22.124829,
      "alt_baro": 38000,
      "gs": 427.3,
      "track": 294.77,
      "t": "B738",
      "r": "F-GZHI"
    }
  ]
}
```

`alt_baro` is the string `"ground"` instead of a number when the aircraft is on the ground.

### `GET /api/route/{callsign}`

- Cache TTL 24h, keyed by the uppercased callsign — routes don't change mid-day, and unknown results are cached too so a repeated lookup for a routeless callsign doesn't keep hitting adsbdb.
- Response when known:

```json
{
  "known": true,
  "callsign": "AAL100",
  "airline": "American Airlines",
  "origin": { "iata": "JFK", "icao": "KJFK", "name": "John F Kennedy International Airport" },
  "destination": { "iata": "LHR", "icao": "EGLL", "name": "London Heathrow Airport" }
}
```

- Response when unknown: `{"known": false}` — always `200`, never a bare 404 passed through to the client.

### `GET /`

Serves `/web` as static files.

### Errors

Upstream failures (timeout, non-200, malformed JSON) return `502` with `{"error": "..."}`. Upstream calls time out at 5s. One log line is written per upstream fetch (not per client request), e.g.:

```
2026/07/16 20:11:50 aircraft fetch lat=37.98 lon=23.72 radius=100: 31 aircraft
2026/07/16 20:11:51 route fetch callsign=AAL100: known
```

## Caching

`internal/cache` is a generic, mutex-guarded map (`Cache[T]`) with TTL eviction on read — no sweeper goroutine, no external deps. Both the aircraft cache and the route cache are separate instances of it, storing pre-encoded JSON bytes so a cache hit is just a byte-slice write to the response.

## Frontend behavior

`web/app.js` is a single ES module, no bundler.

- **Map**: Leaflet, CARTO `dark_all` tiles, centered on Athens (37.98, 23.72) by default.
- **Polling**: `/api/aircraft` every 5s for the current center/radius.
- **Markers**: keyed by `hex`, one `L.divIcon` each (inline SVG dart/plane shape rotated to `track`).
  - **Gliding**: on each poll, existing markers get `transition: transform 4.5s linear` set inline right before `marker.setLatLng(...)` — since Leaflet positions markers via a CSS `transform`, this turns the position update into a CSS-animated glide for free. The inline transition is reset to `none` ~100ms after it finishes, so map pan/zoom (which also repositions markers) never triggers a spurious glide.
  - **Staleness**: every marker gets a `missed` counter incremented at the start of each poll and reset to 0 when its aircraft reappears in the response; markers with `missed >= 3` are removed.
- **Selection**: clicking a marker adds a `.selected` class (accent color) and opens the side panel with callsign/type/registration/altitude/speed/heading from the last poll, then fetches `/api/route/{callsign}` to fill in airline/origin/destination (or "Route unknown"). While the panel is open, subsequent polls patch just the stat values in place rather than re-rendering the whole panel or re-fetching the route.
- **Radius selector / locate-me**: both clear all markers and immediately re-poll against the new center/radius. Locate-me falls back to Athens on denial, error, or when `navigator.geolocation` is unavailable.
- **Stale indicator**: a poll that throws (network error, non-OK status) shows the "stale data" badge and leaves existing markers untouched; a subsequent successful poll clears it.

## UI direction

Dark, quiet, instrument-like — glass cockpit, not consumer app. One accent color (`--accent`, teal) marks the selected aircraft. System sans-serif for labels, monospace for data readouts (callsign, altitude, speed, heading, airport codes). No shadows/gradients beyond a subtle backdrop blur on panels; the map stays uncluttered.

## Non-goals

No accounts, no database, no Docker, no React/build step, no flight history/trails, no third-party analytics or trackers.

## Testing

There's no test suite (the app is thin proxying + DOM glue over two well-defined upstream APIs). What's actually been exercised:

- `go build ./...` / `go vet ./...`
- `curl` against `/api/aircraft` and `/api/route/{callsign}` for cache hits, known/unknown routes, and the `400` bad-request path
- A headless-browser pass (Playwright) driving the real page against live traffic: marker rendering, click → detail panel → route resolution, radius-selector refetch, and the geolocation-denied fallback — all with zero console errors

If you want a repeatable smoke test checked into the repo (e.g. a `Makefile` target or a small Playwright script under `/web` tooling), that's not currently there — ask if you'd like it added.
