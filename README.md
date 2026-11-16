# WingView flight pages

Server-rendered, crawlable pages for every flight, aircraft, airport and
airline WingView tracks, built from the internal API (`/internal/v1`). They
live under the main site:

| path | page |
|---|---|
| `/flight/{callsign}` | a flight number: live status, times, route map, 30 days of history (`/flight/UAL1275`; `UA1275` redirects) |
| `/aircraft/{registration}` | an airframe: position, route network, specs, register entry, recent flights (`/aircraft/N527AS`; the Mode S hex redirects) |
| `/airport/{icao}` | live arrivals, departures and ground traffic within 50 mi, reference data, nearby airports (`/airport/KSEA`; `SEA` redirects) |
| `/airline/{icao}` | flight numbers and aircraft seen this week (`/airline/ASA`) |
| `/tracker/` | landing page, search (`/tracker/search?q=`), sitemaps (`/tracker/sitemap.xml`), static assets |

## How it works

One Go binary, standard library only.

- **Pages render on request and cache.** Every page is complete HTML (no
  client-side rendering), sent with `Cache-Control: max-age` plus
  `stale-while-revalidate`, so the CDN serves it instantly and refreshes it in
  the background. A small in-process cache collapses concurrent misses into
  one render. Live pages keep 60 s, archives 5 min. Pre-generating files at
  build time does not fit: positions change every minute, and the API has no
  "list everything" endpoint to generate from.
- **Discovery.** A background crawler sweeps the ~1,200 large airports
  (`/airports/{code}/aircraft`), backing off from airports that come back
  empty, and resolves routes for flights it saw with one end known. What it
  learns, plus everything pages render, goes into an index that feeds the
  sitemaps, airline pages and internal links. The index is snapshotted to
  `DATA_DIR` (a persistent volume) every 5 minutes and on shutdown.
- **Maps** are interactive Mapbox GL JS maps (light basemap tinted to the
  brand). The server computes everything they draw (great-circle routes split
  at the live position, airport pins, range rings, the aircraft and its
  heading) and embeds it as JSON; `static/site.js` only renders it, loading
  Mapbox after the page content and never for crawlers. Without
  `MAPBOX_TOKEN` the server draws the same maps as SVG from embedded Natural
  Earth data (local development, tests).
- **Upstream protection.** At most 24 concurrent API calls, a 10 s timeout,
  one retry on 5xx, and short TTL caches per endpoint. Not-found answers are
  cached too, because bots probe a lot of nonsense.

```
cmd/site            main: env config, crawler, HTTP server
internal/api        typed internal-API client + caching
internal/cache      bounded TTL cache with request coalescing
internal/store      discovery index, crawler, snapshots
internal/geo        embedded airports/basemap, projection, SVG maps
internal/web        handlers, view models, templates, CSS, map script, sitemaps
tools/gendata       rebuilds internal/geo/data from public sources
tools/ogcard        renders the default social share card
```

## Run locally

```sh
export WV_API_BASE=https://…/internal/v1 WV_API_TOKEN=… SITE_URL=https://www.wingview.com
go run ./cmd/site            # http://localhost:8080/flight/UAL1275
go test ./...
```

| variable | |
|---|---|
| `WV_API_BASE`, `WV_API_TOKEN` | internal API (required) |
| `SITE_URL` | canonical origin used in canonical tags, JSON-LD and sitemaps (required) |
| `APP_URL` | the app, for "open live" deep links (default `https://app.wingview.com`) |
| `MAPBOX_TOKEN` | Mapbox public (`pk.`) token for interactive maps; optional, SVG maps without it |
| `DATA_DIR` | snapshot directory for the discovery index (optional) |
| `CRAWL=0` | disable the crawler; `CRAWL_GAP_MS` (1200) and `RESOLVE_GAP_MS` (2500) set its pace |
| `PORT`, `LOG_LEVEL` | 8080, info |

## SEO notes

- One canonical URL per entity. IATA forms, lowercase, hex codes and
  FlightAware-shaped paths (`/live/flight/…`) 301 to it. Unknown entities are
  real 404s with `noindex`, never soft 404s.
- JSON-LD: `Flight`, `Airport`, `Airline`, `BreadcrumbList`, plus `WebSite` and
  `Organization` on the landing page.
- Aircraft in the FAA LADD program render identity and owner only, and are
  `noindex` and left out of sitemaps.
- The container's own hostname serves a `Disallow: /` robots.txt; canonical
  tags always point at `SITE_URL`.

## Design

The pages follow the flight-tracker convention FlightAware and Flightradar24
set (owner's direction), in WingView's navy, sky blue and Inter. Product
context is in `PRODUCT.md`; the visual system is in `DESIGN.md`.

## Reference data

Embedded under `internal/geo/data`, rebuilt by `go run ./tools/gendata -raw DIR`:
airports from [OurAirports](https://ourairports.com/data/) (public domain)
with time zones from [mwgg/Airports](https://github.com/mwgg/Airports) (MIT),
and the basemap from [Natural Earth](https://www.naturalearthdata.com/) 1:50m
(public domain). Inter is under the SIL Open Font License.

## Share image

`internal/web/static/og-default.jpg` comes from `go run ./tools/ogcard > og.html`,
screenshotted at 1200×630 next to copies of `wingview-header.svg` and `fonts/`.
