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

- **Every request is a fresh render; only API answers are cached.** Every
  page is complete HTML (no client-side rendering), rendered on each request
  from the API client's cached answers and sent `Cache-Control: no-store`, as
  are redirects, sitemaps and live-refresh answers. Nothing rendered is kept
  in the process, a browser or a CDN; the only cacheable responses are static
  files at their exact content hash. Pre-generating files at build time does
  not fit: positions change every minute, and the API has no "list
  everything" endpoint to generate from.
- **Discovery.** A background crawler sweeps the ~1,200 large airports
  (`/airports/{code}/aircraft`), backing off from airports that come back
  empty, and resolves routes for flights it saw with one end known. What it
  learns, plus everything pages render, goes into an index that feeds the
  sitemaps, airline pages and internal links. The index is snapshotted to
  `DATA_DIR` (a persistent volume) every 5 minutes and on shutdown.
- **Maps** are fixed MapLibre GL JS maps (no pan, zoom or controls; pins
  and aircraft link to their pages), shaded with Mapterhorn elevation
  (`TERRAIN`), over WingView's own
  Protomaps basemap, a PMTiles archive the browser reads by HTTP range
  requests (`MAP_TILES`, default `https://i.wvcdn.com/20261115.pmtiles`, a dated build). MapLibre,
  the pmtiles protocol and the label glyphs are self-hosted under
  `static/vendor/`; the style (`static/map-style.json`) is generated from
  `@protomaps/basemaps` in the WingView palette. The server computes
  everything the maps draw (a flight's real path: the trail it flew from
  `/flights/{id}/track` and the rest of its filed route from
  `/flights/{id}/plan`, split at the live position, with great circles
  wherever either is missing; airport pins, range rings, live aircraft and
  their headings) and embeds it
  as JSON; `static/site.js` only renders it, after the page content and never
  for crawlers. With `MAP_TILES=off` the server draws SVG maps from embedded
  Natural Earth data instead (tests).
- **Shared API cache.** When the app has a Bunny Database attached
  (`BUNNY_DATABASE_URL`, `BUNNY_DATABASE_AUTH_TOKEN`, set by Bunny), API
  answers are kept there too, gzip-compressed, under the in-memory cache:
  shared by every instance and kept across restarts. Reads get 300 ms and
  fall back to the API; writes happen in the background; expired rows are
  pruned every 10 minutes. `/statusz` reports its hits and misses.
- **Reload on deploy.** Every page carries the build that rendered it
  (`data-version`); `static/version.js` asks `/tracker/version` every 30 s
  while the tab is visible, and once more when it comes back, and reloads
  the page when the build has changed. It never reloads over someone typing,
  and at most once a minute per build, so a rolling deploy cannot loop it.
- **Live refresh.** A flight page whose flight is under way polls
  `/tracker/live/{flight id}/{signature}` every 30 s (`static/live.js`) and
  swaps in its status band, dock and times, rendered by the page's own
  templates, plus the aircraft's new position on the map. The URL is
  HMAC-signed, bound to one flight, valid for 6 hours and renewed by each
  answer, so it only works from that flight's page: there is no key to
  enumerate. Every poll is one fresh render from one real-time lookup;
  neither is cached. No CORS, `Cross-Origin-Resource-Policy:
  same-origin`, `noindex`, and robots.txt disallows it. Polling pauses in
  hidden tabs, backs off on errors and stops when the flight arrives. The
  signing key is derived from `WV_API_TOKEN`, so every instance agrees.
  Airport pages poll `/tracker/live/airport/{ICAO}/{signature}` the same way
  for their arrivals and departures boards (signed under its own label, so a
  flight's token never passes for an airport's). Each poll is a fresh render;
  the boards behind it are an ordinary API answer, cached for 60 s like the
  rest, so a board is at most about a minute and a half behind.
- **Upstream protection.** At most 24 concurrent API calls, a 10 s timeout,
  one retry on 5xx, and every API answer cached for 60 s (in memory, then the
  shared store), except the real-time `/live` lookup, which is never cached.
  Not-found answers are cached too, because bots probe a lot of nonsense.

```
cmd/site            main: env config, crawler, HTTP server
internal/api        typed internal-API client + caching
internal/cache      bounded TTL cache with request coalescing
internal/dbcache    shared API cache in a Bunny Database (libSQL over HTTP)
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
| `MAP_TILES` | PMTiles basemap for the maps (default `https://i.wvcdn.com/20261115.pmtiles`, a dated build; `off` draws SVG maps) |
| `BUNNY_DATABASE_URL`, `BUNNY_DATABASE_AUTH_TOKEN` | shared API cache (Bunny Database, libSQL); optional |
| `TERRAIN` | elevation shaded under the maps, a `{z}/{x}/{y}` template of Terrarium tiles (default `https://tiles.mapterhorn.com/{z}/{x}/{y}.webp`; `off` draws flat maps) |
| `LOGOS` | operator logos as `{LOGOS}/{ICAO}/icon.png`, one per ICAO-assigned operator (default `https://i.wvcdn.com/logos`; `off` shows none) |
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
(public domain). Inter is under the SIL Open Font License. Map libraries,
glyphs and the generated style are listed with their licenses in
`internal/web/static/vendor/NOTICE.md`; map data is © OpenStreetMap
contributors via Protomaps, attributed on every map.

## Share image

`internal/web/static/og-default.jpg` comes from `go run ./tools/ogcard > og.html`,
screenshotted at 1200×630 next to copies of `wingview-header.svg` and `fonts/`.
