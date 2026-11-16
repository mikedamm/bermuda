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
   onto `:{sha}`, then waits until `https://$BUNNY_HOST/healthz` answers
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
consistent; the CDN serves stale pages through restarts.

```sh
BUNNYNET_API_KEY=… WV_API_TOKEN=… WV_API_BASE=https://…/internal/v1 \
IMAGE_TAG=<sha CI pushed> ./deploy/provision.sh
# → BUNNY_APP_ID=…  BUNNY_HOST=…   (set both as repository variables)
```

Rotating the internal API token: update it in the gateway (INTERNAL-API.md),
then rerun `provision.sh` with the new `WV_API_TOKEN`. It replaces the
container's environment and the app restarts.

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
stale-while-revalidate=…, stale-if-error=86400`. Let the www pull zone follow
origin cache headers, and keep query strings in the cache key (search and
asset versions use them).

## Operating it

- Logs: Magic Containers → app → Logs. One JSON line per request; `api failed`
  warnings mean the internal API is erroring.
- `GET /healthz` returns `ok {commit}`.
- The API sees at most 24 concurrent requests from this app. The crawler
  makes about one airport sweep every 1.2 s plus one route lookup every 2.5 s.
