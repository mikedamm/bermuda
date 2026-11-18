// Package web renders the public pages: flights, aircraft, airports,
// airlines, the home page, sitemaps and robots.txt. Handlers fetch from the
// API, build view models with pure functions and render html/template.
package web

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mikedamm/bermuda/internal/api"
	"github.com/mikedamm/bermuda/internal/store"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

// Prefix holds everything that is not an entity page: the tracker landing
// page, search, sitemaps and static assets.
const Prefix = "/tracker"

type Config struct {
	SiteURL string // canonical origin, no trailing slash
	AppURL  string // the WingView app, for deep links
	Version string // build commit, reported by /healthz
	// MapTiles is the PMTiles archive the maps draw (a Protomaps v4
	// basemap); the browser reads it with HTTP range requests, so its host
	// must send CORS headers. Empty means maps are drawn server-side as SVG.
	MapTiles string
	// MapTilesNow, when set, is the archive the maps draw at this moment,
	// which may move while the server runs (cmd/site follows Protomaps'
	// newest final build); it is on MapTiles' host, and MapTiles is the
	// archive it started with.
	MapTilesNow func() string
	// MapTerrain is a {z}/{x}/{y} template for Terrarium-encoded elevation
	// tiles (Mapterhorn), shaded under the maps; empty means flat maps.
	MapTerrain string
	// LiveKey signs the flight pages' live-refresh URLs. Every instance
	// needs the same key; empty means a random one (a single instance).
	LiveKey []byte
	// LogoBase is where operator logos live ({base}/{ICAO}/icon.png);
	// empty means no logos.
	LogoBase string
}

// Vendored map libraries, served from versioned folders under static/vendor.
const (
	mapLibreJS  = "vendor/maplibre-gl-6.16.0/maplibre-gl.mjs"
	mapLibreCSS = "vendor/maplibre-gl-6.16.0/maplibre-gl.css"
	pmtilesJS   = "vendor/pmtiles-4.5.0/pmtiles.js"
	glyphsPath  = "vendor/glyphs/{fontstack}/{range}.pbf"
)

type Server struct {
	api           *api.Client
	index         *store.Index
	cfg           Config
	log           *slog.Logger
	tpl           map[string]*template.Template
	assets        map[string]asset // path under /static/ → content
	host          string           // canonical host
	tilesOrigin   string           // scheme://host of MapTiles, for the CSP
	terrainOrigin string           // and of MapTerrain
	appOrigin     string           // and of AppURL, whose 3D model pages are framed
	logoBase      string           // operator logos, no trailing slash; empty means none
	imgSrc        string           // the CSP's img-src origins
}

type asset struct {
	body  []byte
	gz    []byte
	ctype string
	hash  string
}

func New(a *api.Client, idx *store.Index, cfg Config, log *slog.Logger) (*Server, error) {
	cfg.SiteURL = strings.TrimRight(cfg.SiteURL, "/")
	u, err := url.Parse(cfg.SiteURL)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("bad SITE_URL %q", cfg.SiteURL)
	}
	if len(cfg.LiveKey) == 0 {
		cfg.LiveKey = make([]byte, 32)
		rand.Read(cfg.LiveKey)
	}
	s := &Server{api: a, index: idx, cfg: cfg, log: log, host: u.Host,
		logoBase: strings.TrimRight(cfg.LogoBase, "/"), imgSrc: "'self' https://i.wvcdn.com"}
	if cfg.LogoBase != "" {
		l, err := url.Parse(cfg.LogoBase)
		if err != nil || l.Host == "" {
			return nil, fmt.Errorf("bad LOGOS %q", cfg.LogoBase)
		}
		if o := l.Scheme + "://" + l.Host; o != "https://i.wvcdn.com" {
			s.imgSrc += " " + o
		}
	}
	if a, err := url.Parse(cfg.AppURL); err == nil && a.Host != "" {
		s.appOrigin = a.Scheme + "://" + a.Host
	}
	if cfg.MapTiles != "" {
		t, err := url.Parse(cfg.MapTiles)
		if err != nil || t.Host == "" {
			return nil, fmt.Errorf("bad MAP_TILES %q", cfg.MapTiles)
		}
		s.tilesOrigin = t.Scheme + "://" + t.Host
		if cfg.MapTerrain != "" {
			e, err := url.Parse(cfg.MapTerrain)
			if err != nil || e.Host == "" || !strings.Contains(cfg.MapTerrain, "{z}") {
				return nil, fmt.Errorf("bad TERRAIN %q", cfg.MapTerrain)
			}
			if o := e.Scheme + "://" + e.Host; o != s.tilesOrigin {
				s.terrainOrigin = o
			}
		}
	}
	if err := s.loadAssets(); err != nil {
		return nil, err
	}
	if err := s.loadTemplates(); err != nil {
		return nil, err
	}
	return s, nil
}

var cssURL = regexp.MustCompile(`url\(([a-zA-Z0-9_./-]+)\)`)

// loadAssets embeds static files with content hashes. CSS url()s are
// rewritten to their hashed form so they match the hashed preload links and
// can be cached forever.
func (s *Server) loadAssets() error {
	s.assets = map[string]asset{}
	if err := s.walkAssets(); err != nil {
		return err
	}
	for name, a := range s.assets {
		if a.ctype != "text/css; charset=utf-8" && !strings.HasSuffix(name, ".css") {
			continue
		}
		dir := path.Dir(name)
		css := cssURL.ReplaceAllStringFunc(string(a.body), func(m string) string {
			ref := path.Join(dir, cssURL.FindStringSubmatch(m)[1])
			if _, ok := s.assets[ref]; ok {
				return "url(" + s.staticURL(ref) + ")"
			}
			return m
		})
		s.assets[name] = newAsset([]byte(css), a.ctype)
	}
	return nil
}

func newAsset(b []byte, ctype string) asset {
	sum := sha256.Sum256(b)
	a := asset{body: b, ctype: ctype, hash: hex.EncodeToString(sum[:])[:10]}
	if strings.HasPrefix(ctype, "text/") || strings.Contains(ctype, "javascript") || strings.Contains(ctype, "svg") {
		var buf bytes.Buffer
		level := gzip.BestCompression
		if len(b) > 256<<10 { // the vendored map library: default level, much faster at start
			level = gzip.DefaultCompression
		}
		zw, _ := gzip.NewWriterLevel(&buf, level)
		zw.Write(b)
		zw.Close()
		a.gz = buf.Bytes()
	}
	return a
}

func (s *Server) walkAssets() error {
	return fs.WalkDir(staticFS, "static", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := staticFS.ReadFile(p)
		if err != nil {
			return err
		}
		ctype := mime.TypeByExtension(path.Ext(p))
		if ctype == "" {
			ctype = "application/octet-stream"
		}
		s.assets[strings.TrimPrefix(p, "static/")] = newAsset(b, ctype)
		return nil
	})
}

// staticURL returns the versioned URL of an embedded asset. The content hash
// is a path segment, not a query string: CDNs that drop query strings from
// the cache key (a common default) would otherwise pin old CSS for a year.
func (s *Server) staticURL(name string) string {
	if strings.HasPrefix(name, "vendor/") {
		// versioned by folder name; modules there import their siblings by
		// relative path, so they must not sit under a per-file hash
		return Prefix + "/static/" + name
	}
	if a, ok := s.assets[name]; ok {
		return Prefix + "/static/" + a.hash + "/" + name
	}
	return Prefix + "/static/" + name
}

func (s *Server) loadTemplates() error {
	s.tpl = map[string]*template.Template{}
	pages := []string{"home", "flight", "aircraft", "airport", "airline", "search", "notfound", "error"}
	for _, p := range pages {
		t, err := template.New(p).Funcs(s.funcs()).ParseFS(templateFS, "templates/layout.html", "templates/parts.html", "templates/"+p+".html")
		if err != nil {
			return fmt.Errorf("template %s: %w", p, err)
		}
		s.tpl[p] = t
	}
	return nil
}

// Handler wires routes and middleware.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		io.WriteString(w, "ok "+s.cfg.Version+"\n")
	})
	// operational detail for the container hostname only (not routed from www)
	mux.HandleFunc("GET /statusz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		st := s.index.Status()
		st["version"] = s.cfg.Version
		if s.cfg.MapTiles != "" {
			st["map_tiles"] = s.tiles()
		}
		if c := s.api.Stats(); c != nil {
			st["api_cache"] = c
		}
		json.NewEncoder(w).Encode(st)
	})
	// Everything that is not an entity page lives under one prefix, so the
	// main site needs only a handful of routing rules (see DEPLOYMENT.md):
	// /flight/ /aircraft/ /airport/ /airline/ and /tracker/.
	mux.HandleFunc("GET "+Prefix+"/static/{path...}", s.serveStatic)
	mux.HandleFunc("GET "+Prefix+"/sitemap.xml", s.page(s.sitemapIndex))
	mux.HandleFunc("GET "+Prefix+"/sitemaps/{name}", s.page(s.sitemapFile))
	mux.HandleFunc("GET "+Prefix+"/search", s.page(s.searchPage))
	mux.HandleFunc("GET "+Prefix+"/live/{id}/{token}", s.liveFlight)
	mux.HandleFunc("GET "+Prefix+"/live/airport/{code}/{token}", s.liveAirport)
	// the build this server runs: open pages poll it and reload when it moves on
	mux.HandleFunc("GET "+Prefix+"/version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("X-Robots-Tag", "noindex")
		io.WriteString(w, s.cfg.Version)
	})
	mux.HandleFunc("GET "+Prefix+"/{$}", s.page(s.homePage))
	mux.HandleFunc("GET "+Prefix, redirectTo(func(r *http.Request) string { return Prefix + "/" }))

	mux.HandleFunc("GET /flight/{ident}", s.page(s.flightPage))
	mux.HandleFunc("GET /aircraft/{key}", s.page(s.aircraftPage))
	mux.HandleFunc("GET /airport/{code}", s.page(s.airportPage))
	mux.HandleFunc("GET /airline/{code}", s.page(s.airlinePage))

	// Only reached on the container's own hostname; the main site owns these.
	mux.HandleFunc("GET /robots.txt", s.robots)
	mux.HandleFunc("GET /{$}", redirectTo(func(r *http.Request) string { return Prefix + "/" }))
	mux.HandleFunc("GET /favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, s.staticURL("icon-192.png"), http.StatusMovedPermanently)
	})

	// FlightAware-shaped URLs people type from memory
	mux.HandleFunc("GET /live/flight/{ident}", redirectTo(func(r *http.Request) string { return "/flight/" + r.PathValue("ident") }))
	mux.HandleFunc("GET /live/airport/{code}", redirectTo(func(r *http.Request) string { return "/airport/" + r.PathValue("code") }))
	mux.HandleFunc("GET /resources/registration/{key}", redirectTo(func(r *http.Request) string { return "/aircraft/" + r.PathValue("key") }))

	mux.HandleFunc("/", s.page(func(ctx context.Context, r *http.Request) (resp, error) {
		return s.notFound("There is no page at this address.", ""), nil
	}))
	return s.middleware(mux)
}

func redirectTo(to func(*http.Request) string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, to(r), http.StatusMovedPermanently)
	}
}

// resp is what a page handler produces; the pipeline turns it into HTTP.
type resp struct {
	status   int
	body     []byte
	ctype    string
	redirect string
}

func redirect(to string) resp {
	return resp{status: http.StatusMovedPermanently, redirect: to}
}

type handlerFunc func(ctx context.Context, r *http.Request) (resp, error)

// page renders on every request; nothing rendered is kept. The API client
// caches the answers pages are built from (and runs one upstream call for
// any number of concurrent misses), so a render costs template time and a
// visitor never sees data older than its endpoint's TTL.
func (s *Server) page(h handlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
		defer cancel()
		out, err := h(ctx, r)
		if err != nil {
			if r.Context().Err() != nil {
				return // the visitor left; there is no one to answer
			}
			out = s.failure(err)
		}
		s.write(w, r, out)
	}
}

func (s *Server) failure(err error) resp {
	s.log.Error("render failed", "err", err)
	status := http.StatusInternalServerError
	msg := "This page failed to load. Reload to try again."
	if errors.Is(err, api.ErrUpstream) {
		status = http.StatusServiceUnavailable
		msg = "Live flight data is not reachable at the moment. Reload in a minute to try again."
	}
	body, rerr := s.render("error", s.basePage(status, "Temporarily unavailable", "", ""), map[string]string{"Message": msg})
	if rerr != nil {
		body = []byte(msg)
	}
	return resp{status: status, body: body, ctype: "text/html; charset=utf-8"}
}

func (s *Server) write(w http.ResponseWriter, r *http.Request, o resp) {
	h := w.Header()
	if o.status == http.StatusServiceUnavailable {
		h.Set("Retry-After", "60")
	}
	// No host-based noindex here: behind the main site's routing the
	// container sees its own hostname, so Host cannot tell a preview hit from
	// a real one. Canonical tags point at SITE_URL and the container
	// hostname's robots.txt disallows crawling, which covers the preview.
	if o.redirect != "" {
		http.Redirect(w, r, o.redirect, o.status)
		return
	}
	if o.ctype == "" {
		o.ctype = "text/html; charset=utf-8"
	}
	h.Set("Content-Type", o.ctype)
	h.Set("Vary", "Accept-Encoding")
	body := o.body
	if len(body) > 1024 && strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		var buf bytes.Buffer
		zw, _ := gzip.NewWriterLevel(&buf, gzip.DefaultCompression)
		zw.Write(body)
		zw.Close()
		body = buf.Bytes()
		h.Set("Content-Encoding", "gzip")
	}
	h.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(o.status)
	if r.Method != http.MethodHead {
		w.Write(body)
	}
}

// canonicalHost is false on the container's own hostname and other aliases.
func (s *Server) canonicalHost(r *http.Request) bool {
	return strings.EqualFold(r.Host, s.host)
}

func (s *Server) serveStatic(w http.ResponseWriter, r *http.Request) {
	name, cacheFor := r.PathValue("path"), "no-store"
	if strings.HasPrefix(name, "vendor/") { // versioned by folder name
		cacheFor = "public, max-age=31536000, immutable"
	}
	if hash, rest, ok := strings.Cut(name, "/"); ok && len(hash) == 10 {
		if a, ok := s.assets[rest]; ok {
			name = rest
			if a.hash == hash { // exact version: cache forever
				cacheFor = "public, max-age=31536000, immutable"
			} // an old hash after a deploy gets today's file, not kept
		}
	}
	a, ok := s.assets[name]
	if !ok {
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	h.Set("Content-Type", a.ctype)
	h.Set("Cache-Control", cacheFor)
	h.Set("Vary", "Accept-Encoding")
	body := a.body
	if a.gz != nil && strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		body = a.gz
		h.Set("Content-Encoding", "gzip")
	}
	h.Set("Content-Length", strconv.Itoa(len(body)))
	w.Write(body)
}

func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		// Nothing this server sends may be kept by a browser or a CDN: every
		// request for a page is a fresh render. The one exception is a static
		// file at its exact content hash (serveStatic), which cannot go stale.
		h.Set("Cache-Control", "no-store")
		// MapLibre runs from our own origin (module scripts, a blob: module
		// worker), reads tiles from the PMTiles host by range requests and
		// fetches elevation tiles from the terrain host.
		h.Set("Content-Security-Policy", "default-src 'self'; img-src "+s.imgSrc+" data: blob:; "+
			"style-src 'self' 'unsafe-inline'; script-src 'self'; font-src 'self'; "+
			"connect-src 'self' "+strings.TrimSpace(s.tilesOrigin+" "+s.terrainOrigin)+"; worker-src 'self' blob:; child-src blob:; "+
			"frame-src "+strings.TrimSpace(s.appOrigin+" blob:")+"; "+
			"object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		rec := &statusRecorder{ResponseWriter: w, status: 200}
		defer func() {
			if p := recover(); p != nil {
				s.log.Error("panic", "path", r.URL.Path, "panic", p)
				http.Error(w, "internal error", http.StatusInternalServerError)
			}
			if r.URL.Path != "/healthz" {
				s.log.Info("req", "method", r.Method, "path", r.URL.Path, "status", rec.status,
					"ms", time.Since(start).Milliseconds(), "ua", r.UserAgent())
			}
		}()
		next.ServeHTTP(rec, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}
