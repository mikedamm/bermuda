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
   `ok {sha}`, then purges the app's pull zone so no page built by the old
   version outlives it (the www pull zone keeps its own copies until they
   expire: at most 5 minutes plus its stale window)

Pull requests run `test` only.

| GitHub setting | kind | value |
|---|---|---|
| `BUNNYNET_API_KEY` | secret | bunny.net account API key |
| `BUNNY_APP_ID` | variable | Magic Containers app id (deploy is skipped while unset) |
| `BUNNY_HOST` | variable | the app's CDN hostname, for the post-deploy check |
| `BUNNY_PULLZONE_ID` | variable | the app's pull zone (7022417), purged after each deploy |

The GHCR package is private. Bunny pulls it through the registry entry
`airlock-ghcr` (id 8623), which holds GitHub credentials for `mikedamm`.

## The app

`deploy/provision.sh` creates the app, or refreshes its environment if it
exists. Everything except the image tag is defined there: env vars, probes on
`/healthz`, a 1 GB volume at `/data` for the discovery index, the CDN endpoint
on port 8080, and 1 instance. One instance keeps one index, so sitemaps stay
consistent; the CDN serves stale pages through restarts.

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

- **Basemap:** `https://i.wvcdn.com/v4.pmtiles` (Protomaps v4, on the `adsb`
  pull zone, id 5676314). Browsers read it cross-origin by range requests, so
  that zone's CORS extension list must include `pmtiles` (added 2026-11-16).
  Remove it and every map goes blank while the pages keep working. Override
  the archive with `MAP_TILES`.
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

Responses carry `Cache-Control: public, max-age=60…300,
stale-while-revalidate=…, stale-if-error=86400`; search answers are
`no-store`; static assets are versioned by path (`/tracker/static/<hash>/…`)
and immutable. For the www pull zone, match what `provision.sh` sets on the
app's own zone:

| setting | value | why |
|---|---|---|
| Cache expiration time | respect origin Cache-Control | pages set their own freshness (60 s live, 5 min archive) |
| Browser cache expiration | match server | |
| Ignore query strings | off | `/tracker/search?q=` must not share one cache entry |
| Smart cache | off | it skips HTML, which is most of what this site serves |
| Stale cache: while updating, while origin offline | on | instant pages during refresh, last good page during an API outage |

## Operating it

- Logs: Magic Containers → app → Logs. One JSON line per request; `api failed`
  warnings mean the internal API is erroring.
- `GET /healthz` returns `ok {commit}`.
- The API sees at most 24 concurrent requests from this app. The crawler
  makes about one airport sweep every 1.2 s plus one route lookup every 2.5 s.
