// Command site serves the WingView public flight pages.
//
// Configuration is by environment (see README):
//
//	WV_API_BASE   internal API base URL, ending in /internal/v1   (required)
//	WV_API_TOKEN  internal API token                               (required)
//	SITE_URL      canonical origin, e.g. https://www.wingview.com  (required)
//	APP_URL       the WingView app                                 (default https://app.wingview.com)
//	PORT          listen port                                      (default 8080)
//	DATA_DIR      where the discovery index is snapshotted         (optional; a persistent volume)
//	CRAWL         "0" disables the background crawler              (default on)
//	LOG_LEVEL     debug, info, warn                                (default info)
//	BUNNY_DATABASE_URL, BUNNY_DATABASE_AUTH_TOKEN
//	              shared cache for API answers (set by Bunny on the app; optional)
//	MAP_TILES     PMTiles basemap for the interactive maps           (default https://i.wvcdn.com/20261115.pmtiles; "off" draws SVG maps)
//	TERRAIN       elevation tiles shaded under the maps, a {z}/{x}/{y} template of Terrarium tiles
//	              (default https://tiles.mapterhorn.com/{z}/{x}/{y}.webp; "off" draws flat maps)
//	LOGOS         operator logos, {LOGOS}/{ICAO}/icon.png            (default https://i.wvcdn.com/logos; "off" shows none)
package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
	_ "time/tzdata" // airport local times in a scratch image

	"github.com/mikedamm/bermuda/internal/api"
	"github.com/mikedamm/bermuda/internal/dbcache"
	"github.com/mikedamm/bermuda/internal/store"
	"github.com/mikedamm/bermuda/internal/web"
)

// version is the commit the binary was built from (set by the Containerfile).
var version = "dev"

func main() {
	level := slog.LevelInfo
	_ = level.UnmarshalText([]byte(env("LOG_LEVEL", "info")))
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

// openCache connects the Bunny Database tier, creates its table, and prunes
// expired answers every ten minutes for as long as ctx lives.
func openCache(ctx context.Context, url, token string, log *slog.Logger) (*dbcache.DB, error) {
	db, err := dbcache.Open(url, token)
	if err != nil {
		return nil, err
	}
	ictx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := db.Init(ictx); err != nil {
		return nil, err
	}
	go func() {
		t := time.NewTicker(10 * time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				pctx, cancel := context.WithTimeout(ctx, 30*time.Second)
				n, err := db.Prune(pctx, 5000)
				cancel()
				if err != nil {
					log.Warn("cache prune failed", "err", err)
				} else if n > 0 {
					log.Info("cache pruned", "rows", n)
				}
			}
		}
	}()
	return db, nil
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func run(log *slog.Logger) error {
	base, token, site := os.Getenv("WV_API_BASE"), os.Getenv("WV_API_TOKEN"), os.Getenv("SITE_URL")
	if base == "" || token == "" || site == "" {
		return errors.New("WV_API_BASE, WV_API_TOKEN and SITE_URL are required")
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	client := api.New(base, token, 24, log)
	if dbURL, dbToken := os.Getenv("BUNNY_DATABASE_URL"), os.Getenv("BUNNY_DATABASE_AUTH_TOKEN"); dbURL != "" && dbToken != "" {
		if db, err := openCache(ctx, dbURL, dbToken, log); err != nil {
			log.Warn("shared API cache unavailable; using memory only", "err", err)
		} else {
			client.UseStore(db)
			log.Info("shared API cache on")
		}
	}
	idx := store.NewIndex()
	dataDir := os.Getenv("DATA_DIR")
	if dataDir != "" {
		if err := os.MkdirAll(dataDir, 0o755); err != nil {
			log.Warn("data dir unusable; index will not persist", "dir", dataDir, "err", err)
			dataDir = ""
		} else if err := idx.Load(dataDir); err != nil {
			log.Warn("index snapshot not loaded", "err", err)
		}
	}
	if env("CRAWL", "1") != "0" {
		gap, _ := strconv.Atoi(env("CRAWL_GAP_MS", "1200"))
		resolveGap, _ := strconv.Atoi(env("RESOLVE_GAP_MS", "2500"))
		cr := &store.Crawler{
			API: client, Index: idx, Log: log, DataDir: dataDir,
			SweepGap: time.Duration(gap) * time.Millisecond, SweepEvery: 15 * time.Minute,
			ResolveGap: time.Duration(resolveGap) * time.Millisecond,
			RadiusMi:   40, MaxBackoff: 6 * time.Hour, SaveEvery: 5 * time.Minute,
		}
		cr.Run(ctx)
	}

	// a dated Protomaps build, never a moving alias: the CDN caches range
	// slices, and an alias that moves under the cache mixes two builds
	tiles := env("MAP_TILES", "https://i.wvcdn.com/20261115.pmtiles")
	if tiles == "off" {
		tiles = ""
	}
	terrain := env("TERRAIN", "https://tiles.mapterhorn.com/{z}/{x}/{y}.webp")
	if terrain == "off" {
		terrain = ""
	}
	logos := env("LOGOS", "https://i.wvcdn.com/logos")
	if logos == "off" {
		logos = ""
	}
	srv, err := web.New(client, idx, web.Config{SiteURL: site, AppURL: env("APP_URL", "https://app.wingview.com"), Version: version, MapTiles: tiles, MapTerrain: terrain, LiveKey: liveKey(token), LogoBase: logos}, log)
	if err != nil {
		return err
	}
	hs := &http.Server{
		Addr:              ":" + env("PORT", "8080"),
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      40 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	errc := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", hs.Addr, "site", site, "version", version)
		errc <- hs.ListenAndServe()
	}()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down")
	shut, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	err = hs.Shutdown(shut)
	if dataDir != "" {
		if serr := idx.Save(dataDir); serr != nil {
			log.Warn("final index save failed", "err", serr)
		}
	}
	return err
}

// liveKey signs the flight pages' live-refresh URLs. It is derived from the
// API token, which every instance already shares, so a URL one instance
// signs verifies on any other; the token cannot be recovered from it.
func liveKey(apiToken string) []byte {
	k := sha256.Sum256([]byte("wingview live-refresh v1\x00" + apiToken))
	return k[:]
}
