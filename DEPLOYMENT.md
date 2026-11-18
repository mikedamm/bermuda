# Deployment

The site runs as one container on **Bunny Magic Containers**, app
`wingview-seo`, region `SIL` (Silicon Valley, next to the internal API in
Fremont). The Magic Containers CDN endpoint sits in front of it.

## Builds: CI only

Images are built **only by GitHub Actions** (`.github/workflows/deploy.yml`),
never locally (podman/docker are disabled on the dev box). On every push to
`main`:

1. `test`: gofmt, `go vet`, `go test ./...`
2. `image`: buildah builds the `Containerfile` for linux/amd64 and pushes
   `ghcr.io/mikedamm/bermuda:{sha,latest}`
3. `deploy`: `BunnyWay/actions/container-update-image` rolls container `web`
   onto `:{sha}`, waits until `https://$BUNNY_HOST/healthz` answers
   `ok {sha}`

Pull requests run `test` only.

| GitHub setting | kind | value |
|---|---|---|
| `BUNNYNET_API_KEY` | secret | bunny.net account API key |
| `BUNNY_APP_ID` | variable | Magic Containers app id (deploy is skipped while unset) |
| `BUNNY_HOST` | variable | the app's CDN hostname, for the post-deploy check |

The GHCR package is private. Bunny pulls it through the registry entry
`airlock-ghcr` (id 8623), which holds GitHub credentials for `mikedamm`.

## The app

`deploy/provision.sh` creates the app, or refreshes its environment if it
exists. Everything except the image tag is defined there: env vars, probes on
`/healthz`, a 1 GB volume at `/data` for the discovery index, the CDN endpoint
on port 8080, and 1 instance. One instance keeps one index, so sitemaps stay
consistent. The CDN endpoint is for development; pages pass through it
uncached (they are `no-store`).

```sh
BUNNYNET_API_KEY=… WV_API_TOKEN=… WV_API_BASE=https://…/internal/v1 \
IMAGE_TAG=<sha CI pushed> ./deploy/provision.sh
# → BUNNY_APP_ID=…  BUNNY_HOST=…   (set both as repository variables)
```

Rotating the internal API token: update it in the gateway (INTERNAL-API.md),
then rerun `provision.sh` with the new `WV_API_TOKEN`. It replaces the
container's environment and the app restarts. The script merges: variables
it does not own, such as the `BUNNY_DATABASE_*` ones Bunny adds for the
attached database, are kept.

## Maps and the shared cache

- **Basemap:** Protomaps' newest *final* v4 build, through the `adsb` pull
  zone (id 5676314) at `https://i.wvcdn.com/{date}.pmtiles`, whose origin is
  `build.protomaps.com`. Browsers read it cross-origin by range requests, so
  that zone's CORS extension list must include `pmtiles` (added 2026-11-16).
  Remove it and every map goes blank while the pages keep working.
  - Protomaps keeps the past week's daily builds and, for good, the last
    build of each patch version. The server (`internal/basemap`) reads
    https://build-metadata.protomaps.dev/builds.json at start and every six
    hours and draws the last build of the newest version a later one has
    replaced: 4.15.1's `20260811.pmtiles` while 4.15.2 is current. It moves
    when Protomaps finishes a version, every few weeks to few months, so the
    zone's cache of one archive stays good that long. A daily build is never
    drawn: it would be deleted from origin while its name is still in use.
  - Before moving, the server reads the new archive's first range through
    the zone and moves only on a 206 of the listed size that starts
    `PMTiles`; otherwise it keeps the archive it has and logs
    `basemap: keeping …`. The zone caches a 404 too (and slices of a file
    that changed), and a bad first range blanks every map until that file is
    purged. Until the list answers it draws `basemap.Fallback`.
  - `/statusz` names the archive drawn (`map_tiles`); the log says
    `basemap build=… version=…` when it moves.
  - `MAP_TILES` set to a URL pins one archive. Never pin a moving alias like
    `v4.pmtiles`: when an alias moves under the zone's cache, the browser
    reads one build's directory and another's tiles, and every map loses its
    basemap (2026-11-18). Never pin a daily build either.
- **Shared API cache:** the Bunny Database `wv-seo-cache` is attached to the
  app; Bunny injects `BUNNY_DATABASE_URL` and `BUNNY_DATABASE_AUTH_TOKEN`.
  Table `api_cache` holds gzip-compressed API answers until they expire;
  the app creates it on start and prunes expired rows every 10 minutes.
  Losing the database costs only speed: reads fall back to the API.
  `/statusz` shows `api_cache` hits, misses and errors.

## Serving from www.wingview.com

The pages are built for `https://www.wingview.com` (canonical tags, JSON-LD and
sitemaps all use `SITE_URL`). Route these path prefixes on the www pull zone to
the container's CDN hostname, leaving everything else on the main site:

```
/flight/*    /aircraft/*    /airport/*    /airline/*    /tracker/*
```

`/tracker/*` carries the CSS, fonts, search and sitemaps, so pages break
without it. Then:

- add `Sitemap: https://www.wingview.com/tracker/sitemap.xml` to the main
  site's robots.txt, and submit it in Search Console
- leave `/robots.txt` on the main site; the container's own robots.txt
  disallows everything, so its hostname never gets indexed

Every response is `Cache-Control: no-store` (pages, redirects, sitemaps,
live-refresh answers): one request is one fresh render from the container,
built from cached API answers. The only exception is static assets at their
exact content hash (`/tracker/static/<hash>/…`), which are immutable. For the
www pull zone:

| setting | value | why |
|---|---|---|
| Cache expiration time | respect origin Cache-Control | keeps pages out of the cache; static assets still cache |
| Browser cache expiration | match server | |
| Ignore query strings | off | `/tracker/search?q=` must reach the container with its query |

## Operating it

- Logs: Magic Containers → app → Logs. One JSON line per request; `api failed`
  warnings mean the internal API is erroring.
- `GET /healthz` returns `ok {commit}`.
- The API sees at most 24 concurrent requests from this app. The crawler
  makes about one airport sweep every 1.2 s plus one route lookup every 2.5 s.
