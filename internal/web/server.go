// Package web renders the public pages: flights, aircraft, airports,
// airlines, the home page, sitemaps and robots.txt. Handlers fetch from the
// API, build view models with pure functions and render html/template.
package web

import (
	"bytes"
	"compress/gzip"
	"context"
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
	"github.com/mikedamm/bermuda/internal/cache"
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
}

// Vendored map libraries, served from versioned folders under static/vendor.
const (
	mapLibreJS  = "vendor/maplibre-gl-6.16.0/maplibre-gl.mjs"
	mapLibreCSS = "vendor/maplibre-gl-6.16.0/maplibre-gl.css"
	pmtilesJS   = "vendor/pmtiles-4.5.0/pmtiles.js"
	glyphsPath  = "vendor/glyphs/{fontstack}/{range}.pbf"
)

type Server struct {
	api         *api.Client
	index       *store.Index
	cfg         Config
	log         *slog.Logger
	tpl         map[string]*template.Template
	pages       *cache.Cache
	assets      map[string]asset // path under /static/ → content
	host        string           // canonical host
	tilesOrigin string           // scheme://host of MapTiles, for the CSP
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
	s := &Server{api: a, index: idx, cfg: cfg, log: log, pages: cache.New(5000), host: u.Host}
	if cfg.MapTiles != "" {
		t, err := url.Parse(cfg.MapTiles)
		if err != nil || t.Host == "" {
			return nil, fmt.Errorf("bad MAP_TILES %q", cfg.MapTiles)
		}
		s.tilesOrigin = t.Scheme + "://" + t.Host
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
		zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
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
		w.Header().Set("Cache-Control", "public, max-age=86400")
		http.Redirect(w, r, to(r), http.StatusMovedPermanently)
	}
}

// resp is what a page handler produces; the pipeline turns it into HTTP.
type resp struct {
	status   int
	body     []byte
	ctype    string
	maxAge   time.Duration // CDN and browser freshness; 0 means no-store
	redirect string
}

func redirect(to string) resp {
	return resp{status: http.StatusMovedPermanently, redirect: to, maxAge: time.Hour}
}

type handlerFunc func(ctx context.Context, r *http.Request) (resp, error)

// page caches rendered responses in-process for their max-age, so the CDN's
// many edge locations missing at once cost one render, not one each.
func (s *Server) page(h handlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Path + "?" + r.URL.RawQuery // bodies never depend on Host; headers do, and are set per request
		out, err := cache.Do(s.pages, key, func() (resp, time.Duration, error) {
			ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 25*time.Second)
			defer cancel()
			o, err := h(ctx, r)
			if err != nil {
				return o, 0, err
			}
			ttl := o.maxAge
			if o.status >= 500 {
				ttl = 0
			}
			return o, ttl, nil
		})
		if err != nil {
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
	if o.maxAge > 0 {
		sec := int(o.maxAge.Seconds())
		// stale-while-revalidate keeps the CDN serving instantly while it refreshes;
		// stale-if-error rides out an API outage on the last good page.
		h.Set("Cache-Control", fmt.Sprintf("public, max-age=%d, stale-while-revalidate=%d, stale-if-error=86400", sec, sec*5))
	} else {
		h.Set("Cache-Control", "no-store")
	}
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
	name, cacheFor := r.PathValue("path"), "public, max-age=300"
	if strings.HasPrefix(name, "vendor/") { // versioned by folder name
		cacheFor = "public, max-age=31536000, immutable"
	}
	if hash, rest, ok := strings.Cut(name, "/"); ok && len(hash) == 10 {
		if a, ok := s.assets[rest]; ok {
			name = rest
			if a.hash == hash { // exact version: cache forever
				cacheFor = "public, max-age=31536000, immutable"
			} // an old hash after a deploy gets today's file, briefly cached
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
		// MapLibre runs from our own origin (module scripts, a blob: module
		// worker) and reads tiles from the PMTiles host by range requests.
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' https://i.wvcdn.com data: blob:; "+
			"style-src 'self' 'unsafe-inline'; script-src 'self'; font-src 'self'; "+
			"connect-src 'self' "+s.tilesOrigin+"; worker-src 'self' blob:; child-src blob:; "+
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
