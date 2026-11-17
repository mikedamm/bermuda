package web

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/mikedamm/bermuda/internal/api"
	"github.com/mikedamm/bermuda/internal/store"
)

// fakeAPI serves synthetic answers in the internal API's shapes.
func fakeAPI(t *testing.T) *httptest.Server {
	now := time.Now().UTC()
	ts := func(d time.Duration) string { return now.Add(d).Format(time.RFC3339Nano) }
	ref := func(icao, iata, name, city, tz string, lat, lon float64) map[string]any {
		return map[string]any{"icao": icao, "iata": iata, "name": name, "city": city, "tz": tz, "lat": lat, "lon": lon, "country": "US"}
	}
	sea := ref("KSEA", "SEA", "SEATTLE-TACOMA INTL", "SEATTLE", "America/Los_Angeles", 47.45, -122.31)
	san := ref("KSAN", "SAN", "SAN DIEGO INTL", "SAN DIEGO", "America/Los_Angeles", 32.73, -117.19)
	op := map[string]any{"icao": "ASA", "iata": "AS", "name": "Alaska Airlines"}
	live := map[string]any{
		"flight_id": "f-live", "airframe_id": "af-1", "callsign": "ASA14", "iata_flight": "AS14", "operator": op,
		"registration": "N527AS", "hex": "A6A300", "type": "B738", "model": "737-890", "dep": "KSAN", "arr": "KSEA",
		"status": "active", "phase": "en_route", "phase_word": "En Route", "lat": 40.1, "lon": -120.2, "alt": 35000, "gs": 450, "hdg": 330,
		"on_ground": false, "position_at": ts(-10 * time.Second), "last_seen": ts(-10 * time.Second),
		"sched_dep": ts(-2 * time.Hour), "out": ts(-110 * time.Minute), "off": ts(-95 * time.Minute), "eta": ts(40 * time.Minute), "sched_arr": ts(30 * time.Minute),
	}
	hist := map[string]any{
		"flight_id": "f-old", "airframe_id": "af-1", "callsign": "ASA14", "iata_flight": "AS14", "operator": op,
		"registration": "N527AS", "type": "B738", "dep": san, "arr": sea, "status": "completed", "outcome": "completed",
		"phase": "arrived", "phase_word": "Arrived", "first_seen": ts(-30 * time.Hour), "last_seen": ts(-25 * time.Hour),
		"off": ts(-29 * time.Hour), "on": ts(-26*time.Hour - 30*time.Minute), "duration_s": 99999, "duration_basis": "seen",
	}
	// flights still to come: tomorrow's ASA14 (and another number, which
	// the ASA14 page must leave out), and this airframe's next leg
	planned := func(id, callsign, iata, dep, arr string, in time.Duration) map[string]any {
		return map[string]any{"flight_id": id, "callsign": callsign, "iata_flight": iata, "operator": op,
			"dep": dep, "arr": arr, "status": "scheduled", "phase": "scheduled", "phase_word": "Scheduled",
			"sched_dep": ts(in), "sched_arr": ts(in + 150*time.Minute)}
	}
	nextASA14 := planned("f-next", "ASA14", "AS14", "KSAN", "KSEA", 20*time.Hour)
	otherASA := planned("f-140", "ASA140", "AS140", "KSEA", "KSAN", 3*time.Hour)
	nextLeg := planned("f-leg", "ASA15", "AS15", "KSEA", "KSAN", 2*time.Hour)
	// landed hours ago and still live in the gateway (not archived yet),
	// while the aircraft flies its next leg
	landed := map[string]any{"flight_id": "f-landed", "airframe_id": "af-1", "callsign": "ASA13", "iata_flight": "AS13", "operator": op,
		"registration": "N527AS", "type": "B738", "dep": "KSEA", "arr": "KSAN", "status": "landed", "phase": "arrived", "phase_word": "Arrived",
		"sched_dep": ts(-6 * time.Hour), "out": ts(-6 * time.Hour), "off": ts(-350 * time.Minute), "on": ts(-200 * time.Minute), "in": ts(-195 * time.Minute)}
	otherAirframe := map[string]any{"flight_id": "f-x", "airframe_id": "af-2", "callsign": "ASA99", "iata_flight": "AS99", "operator": op,
		"dep": "KSEA", "arr": "KPDX", "status": "landed", "phase": "arrived", "phase_word": "Arrived", "off": ts(-2 * time.Hour), "on": ts(-time.Hour)}
	aircraft := map[string]any{
		"key": "N527AS", "airframe_id": "af-1", "days": 30,
		"aircraft": map[string]any{"icao_type": "B738", "manufacturer": "Boeing", "model": "737-890", "name": "Boeing 737-890",
			"registration": "N527AS", "hex": "A6A300", "country": map[string]any{"iso": "US", "name": "United States"}, "type_description": "L2J"},
		"operator": op, "owner": map[string]any{"name": "ALASKA AIRLINES INC", "city": "SEATAC", "state": "WA", "country": "US"},
		"status":         map[string]any{"phase": "flying", "flight": live, "upcoming": []any{nextLeg}},
		"stats":          map[string]any{"days": 30, "flights": 2, "hours": 5.5, "airports": []any{map[string]any{"code": "KSEA", "count": 2, "airport": sea}}},
		"recent_flights": []any{hist},
		"ladd":           map[string]any{"listed": false},
	}
	mux := http.NewServeMux()
	write := func(w http.ResponseWriter, v any) { json.NewEncoder(w).Encode(v) }
	mux.HandleFunc("GET /search/aircraft", func(w http.ResponseWriter, r *http.Request) {
		switch strings.ToUpper(r.URL.Query().Get("q")) {
		case "SLOW1": // an upstream having a bad moment
			time.Sleep(5 * time.Second)
			write(w, map[string]any{"results": []any{}})
		case "ASA14", "AS14":
			write(w, map[string]any{"results": []any{map[string]any{"airframe_id": "af-1", "registration": "N527AS", "icao_type": "B738",
				"name": "Boeing 737-800", "matched": map[string]any{"kind": "callsign", "value": "ASA14"}, "live": live}}})
		case "N527AS":
			write(w, map[string]any{"results": []any{map[string]any{"airframe_id": "af-1", "registration": "N527AS",
				"matched": map[string]any{"kind": "registration", "value": "N527AS"}}}})
		default:
			write(w, map[string]any{"results": []any{}})
		}
	})
	mux.HandleFunc("GET /flights/{id}/plan", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("id") != "f-live" {
			w.WriteHeader(404)
			return
		}
		line := [][2]float64{{-117.19, 32.73}, {-118.0, 35.0}, {-119.5, 38.0}, {-120.6, 41.0}, {-121.5, 44.0}, {-122.31, 47.45}}
		write(w, map[string]any{"flight_id": "f-live", "live": true, "dep": "KSAN", "arr": "KSEA", "phase": "en_route",
			"plan": map[string]any{"route": "KSAN DCT TESTA DCT TESTB KSEA", "route_kind": "filed", "dep": "KSAN", "arr": "KSEA",
				"line": map[string]any{"type": "LineString", "coordinates": line}, "length_nm": 900}})
	})
	mux.HandleFunc("GET /flights/{id}/track", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("id") != "f-live" {
			w.WriteHeader(404)
			return
		}
		ms := now.UnixMilli()
		write(w, map[string]any{"flight_id": "f-live", "live": true, "resolution": "line",
			"trail": []any{
				[]any{ms - 5_400_000, 32.80, -117.25, 3000, "adsb", 180, 300, 0},
				[]any{ms - 3_600_000, 35.50, -118.40, 35000, "adsb", 450, 330, 0},
				[]any{ms - 2_000_000, 45.00, -125.00, 35000, "est", 450, 330, 0}, // dead-reckoned: not drawn
				[]any{ms - 1_200_000, 38.20, -119.70, 35000, "mlat", 450, 330, 1},
			},
			"tail_5m": []any{
				[]any{ms - 290_000, 39.80, -120.05, 35000, "adsb", 450, 330, 0},
				[]any{ms - 285_000, 39.81, -120.06, 35000, "adsb", nil, nil, nil},
				[]any{ms - 10_000, 40.10, -120.20, 35000, "adsb", 450, 330, 0},
			}})
	})
	mux.HandleFunc("GET /flights/{id}/live", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("id") != "f-live" {
			w.WriteHeader(404)
			write(w, map[string]any{"error": "no such live flight"})
			return
		}
		moved := map[string]any{}
		for k, v := range live {
			moved[k] = v
		}
		moved["lat"], moved["lon"], moved["alt"] = 41.5, -121.0, 37000
		write(w, map[string]any{"key": "f-live", "kind": "flight_id", "live": true, "flight": moved})
	})
	mux.HandleFunc("GET /operators/{code}/flights", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.PathValue("code") != "ASA":
			write(w, map[string]any{"flights": []any{}})
		case r.URL.Query().Get("status") == "scheduled":
			write(w, map[string]any{"code": "ASA", "count": 2, "total": 2, "flights": []any{otherASA, nextASA14}})
		case r.URL.Query().Get("status") == "arrived":
			write(w, map[string]any{"code": "ASA", "count": 2, "total": 2, "flights": []any{otherAirframe, landed}})
		default:
			write(w, map[string]any{"flights": []any{}})
		}
	})
	mux.HandleFunc("GET /aircraft/{key}/flights", func(w http.ResponseWriter, r *http.Request) {
		write(w, map[string]any{"key": r.PathValue("key"), "flights": []any{hist}})
	})
	mux.HandleFunc("GET /aircraft/{key}", func(w http.ResponseWriter, r *http.Request) {
		switch strings.ToUpper(r.PathValue("key")) {
		case "N527AS", "A6A300":
			write(w, aircraft)
		case "N2PARK":
			// parked at SEA after its flight from SAN arrived; the last report
			// sits on the field but its ground flag is off (MLAT, some
			// transponders)
			park := map[string]any{}
			for k, v := range aircraft {
				park[k] = v
			}
			park["key"], park["airframe_id"] = "N2PARK", "af-9"
			park["aircraft"] = map[string]any{"registration": "N2PARK", "name": "Boeing 737-890", "icao_type": "B738"}
			park["status"] = map[string]any{"phase": "unknown", "flight": nil, "upcoming": []any{},
				"last": map[string]any{"live": false, "lat": 47.455, "lon": -122.305, "hdg": 160, "at": ts(-50 * time.Minute), "on_ground": false}}
			write(w, park)
		case "N1LADD":
			// a listed aircraft whose answer still carries flights and a
			// position: the page must not show them regardless
			ladd := map[string]any{}
			for k, v := range aircraft {
				ladd[k] = v
			}
			ladd["key"] = "N1LADD"
			ladd["aircraft"] = map[string]any{"registration": "N1LADD", "name": "Cessna 560XL"}
			ladd["ladd"] = map[string]any{"listed": true, "redacted": true}
			ladd["status"] = map[string]any{"phase": "on_ground", "last": map[string]any{"lat": 47.5, "lon": -122.3, "at": ts(-time.Hour), "on_ground": true, "airport": sea}}
			write(w, ladd)
		default:
			w.WriteHeader(404)
			write(w, map[string]any{"error": "no such aircraft"})
		}
	})
	mux.HandleFunc("GET /airports/{code}/aircraft", func(w http.ResponseWriter, r *http.Request) {
		write(w, map[string]any{"airport": r.PathValue("code"), "radius_mi": 50, "aircraft": []any{
			map[string]any{"callsign": "ASA14", "direction": "arriving", "registration": "N527AS", "alt_ft": 9000, "status": "active", "distance_mi": 30},
			map[string]any{"callsign": "SLOW1", "direction": "arriving", "alt_ft": 8000, "status": "active", "distance_mi": 20},
		}})
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			w.WriteHeader(401)
			return
		}
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func testServer(t *testing.T) http.Handler { return newTestServer(t).Handler() }

func newTestServer(t *testing.T) *Server {
	up := fakeAPI(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s, err := New(api.New(up.URL, "test-token", 4, log), store.NewIndex(), Config{SiteURL: "https://www.wingview.com", AppURL: "https://app.wingview.com"}, log)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	req.Host = "mc-example.bunny.run"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestPages(t *testing.T) {
	h := testServer(t)
	cases := []struct {
		path     string
		status   int
		location string
		contains []string
	}{
		{"/flight/ASA14", 200, "", []string{
			`<link rel="canonical" href="https://www.wingview.com/flight/ASA14">`,
			`"@type":"Flight"`, ">Alaska Airlines Flight 14</h1>", `<p class="status s-`, "<strong>En route</strong>", "Gate departure",
			// the masthead's store badges, and the band's count-up time
			`<a href="https://www.wingview.com/ios"><img src="/tracker/static/`, `/badge-app-store.svg" width="135" height="40" alt="Download on the App Store">`,
			`<a href="https://www.wingview.com/android"><img src="/tracker/static/`, `/badge-google-play.svg" width="135" height="40" alt="Get it on Google Play">`,
			// the band: the flight as people say it, its tail number and airframe opposite
			`<p class="band-tail"><a class="band-reg" href="/aircraft/N527AS">N527AS</a>`,
			`href="/aircraft/N527AS"`, `href="/airport/KSEA"`, "2h 30m", // history duration from off/on, not the "seen" span
		}},
		{"/flight/AS14", 301, "/flight/ASA14", nil},
		{"/flight/asa14", 301, "/flight/ASA14", nil},
		{"/flight/N527AS", 301, "/aircraft/N527AS", nil},
		{"/flight/ZZZ999", 404, "", []string{`<meta name="robots" content="noindex,follow">`}},
		{"/aircraft/N527AS", 200, "", []string{`<link rel="canonical" href="https://www.wingview.com/aircraft/N527AS">`, "Boeing 737-890", "Alaska Airlines Inc",
			// the band: the flight it is flying, named as its page names it, opposite its tail number
			`<a class="band-id band-hero" href="/flight/ASA14">`, `<span class="band-name">Alaska Airlines Flight 14</span>`, "<h1>N527AS</h1>", "<dt>Mode S</dt>"}},
		{"/aircraft/a6a300", 301, "/aircraft/N527AS", nil},
		{"/aircraft/N0NE", 404, "", nil},
		{"/airport/SEA", 301, "/airport/KSEA", nil},
		{"/airport/KSEA", 200, "", []string{`"@type":"Airport"`, "Seattle–Tacoma International Airport", ">Arrivals <", `href="/flight/ASA14"`}},
		{"/airport/XXXX", 404, "", nil},
		{"/tracker/", 200, "", []string{`action="/tracker/search"`}},
		{"/", 301, "/tracker/", nil},
		{"/tracker/search?q=AS14", 302, "/flight/ASA14", nil},
		{"/tracker/sitemap.xml", 200, "", []string{"https://www.wingview.com/tracker/sitemaps/airports.xml"}},
		{"/tracker/sitemaps/airports.xml", 200, "", []string{"https://www.wingview.com/airport/KSEA"}},
	}
	for _, c := range cases {
		rec := get(t, h, c.path)
		if rec.Code != c.status {
			t.Errorf("%s: status %d, want %d", c.path, rec.Code, c.status)
			continue
		}
		if c.location != "" && rec.Header().Get("Location") != c.location {
			t.Errorf("%s: Location %q, want %q", c.path, rec.Header().Get("Location"), c.location)
		}
		body := rec.Body.String()
		for _, want := range c.contains {
			if !strings.Contains(body, want) {
				t.Errorf("%s: body lacks %q", c.path, want)
			}
		}
		// nothing rendered is kept by a browser or a CDN: one request, one render
		if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
			t.Errorf("%s: Cache-Control %q, want no-store", c.path, cc)
		}
		if c.status == 200 && strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
			if xr := rec.Header().Get("X-Robots-Tag"); xr != "" {
				t.Errorf("%s: unexpected X-Robots-Tag %q (would noindex the proxied site)", c.path, xr)
			}
		}
	}
}

func TestLADDAircraftShowsNoFlights(t *testing.T) {
	rec := get(t, testServer(t), "/aircraft/N1LADD")
	body := rec.Body.String()
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	for _, banned := range []string{"map-svg", "Recent flights", "mk-plane", "Last seen at", "/flight/ASA14"} {
		if strings.Contains(body, banned) {
			t.Errorf("LADD page contains %q", banned)
		}
	}
	for _, want := range []string{`<meta name="robots" content="noindex,follow">`, "LADD", "N1LADD"} {
		if !strings.Contains(body, want) {
			t.Errorf("LADD page lacks %q", want)
		}
	}
}

// With a tiles archive configured the page embeds map data and the
// self-hosted MapLibre assets for site.js; without one it draws the SVG map
// and ships no script.
func TestMapLibreMaps(t *testing.T) {
	up := fakeAPI(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s, err := New(api.New(up.URL, "test-token", 4, log), store.NewIndex(),
		Config{SiteURL: "https://www.wingview.com", AppURL: "https://app.wingview.com", MapTiles: "https://i.wvcdn.com/v4.pmtiles",
			MapTerrain: "https://tiles.mapterhorn.com/{z}/{x}/{y}.webp"}, log)
	if err != nil {
		t.Fatal(err)
	}
	h := s.Handler()
	rec := get(t, h, "/flight/ASA14")
	body := rec.Body.String()
	for _, want := range []string{`data-map-tiles="https://i.wvcdn.com/v4.pmtiles"`, `data-map-lib="/tracker/static/vendor/maplibre-gl-6.16.0/maplibre-gl.mjs`,
		`data-map role="region"`, `"k":"flown"`, `"k":"ahead"`, `"plane":`, "/site.js", `<link rel="preconnect" href="https://i.wvcdn.com"`,
		`data-map-terrain="https://tiles.mapterhorn.com/{z}/{x}/{y}.webp"`, `<link rel="preconnect" href="https://tiles.mapterhorn.com"`,
		// the model sits under the status band, between the parts live refresh rewrites (or it would reload every 30s)
		"</p></div>\n\t\t\t<iframe class=\"model\" src=\"https://app.wingview.com/aircraft.html?embed=1&amp;registration=N527AS&amp;transparent=1\"",
		"</iframe>\n\t\t\t<div data-part=\"dock\">"} {
		if !strings.Contains(body, want) {
			t.Errorf("map page lacks %q", want)
		}
	}
	// the aircraft page draws its current flight as the flight page does and
	// the two before it (the landed one the archive lacks, then the archived
	// one; no tracks on record: great circles) in gray under it
	ac := get(t, h, "/aircraft/N527AS").Body.String()
	for _, want := range []string{`"k":"flown"`, `"k":"past","w":2}`, `"k":"past","w":1}`, `aria-label="Map of N527AS&#39;s current flight and the two before it"`} {
		if !strings.Contains(ac, want) {
			t.Errorf("aircraft map lacks %s", want)
		}
	}
	if strings.Index(ac, `"k":"past"`) < strings.Index(ac, `"k":"flown"`) {
		t.Error("an earlier flight is drawn before the current one")
	}
	if strings.Contains(ac, `"k":"past"}`) {
		t.Error("an earlier flight has no age: the map would draw it darkest")
	}

	csp := rec.Header().Get("Content-Security-Policy")
	for _, want := range []string{"connect-src 'self' https://i.wvcdn.com https://tiles.mapterhorn.com;", "worker-src 'self' blob:", "script-src 'self';",
		"frame-src https://app.wingview.com blob:;"} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP lacks %q: %q", want, csp)
		}
	}
	if strings.Contains(csp, "mapbox") {
		t.Errorf("CSP still allows Mapbox: %q", csp)
	}
	// vendored libraries are served as JavaScript modules and cached for good
	lib := get(t, h, "/tracker/static/vendor/maplibre-gl-6.16.0/maplibre-gl-worker.mjs")
	if lib.Code != 200 || !strings.Contains(lib.Header().Get("Content-Type"), "javascript") || !strings.Contains(lib.Header().Get("Cache-Control"), "immutable") {
		t.Errorf("worker module: %d %q %q", lib.Code, lib.Header().Get("Content-Type"), lib.Header().Get("Cache-Control"))
	}
	style := get(t, h, "/tracker/static/map-style.json")
	if style.Code != 200 || !strings.Contains(style.Body.String(), `"pmtiles://TILES"`) || !strings.Contains(style.Body.String(), `"raster-dem"`) {
		t.Errorf("style: %d", style.Code)
	}
	// every font the style and site.js name is served as glyphs
	var st struct {
		Layers []struct {
			Layout map[string]any `json:"layout"`
		} `json:"layers"`
	}
	if err := json.Unmarshal(style.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	fonts := map[string]bool{"Inter SemiBold": true} // site.js draws pins and aircraft labels in it
	for _, l := range st.Layers {
		if fs, ok := l.Layout["text-font"].([]any); ok {
			for _, f := range fs {
				fonts[f.(string)] = true
			}
		}
	}
	for f := range fonts {
		glyph := get(t, h, "/tracker/static/vendor/glyphs/"+url.PathEscape(f)+"/0-255.pbf")
		if glyph.Code != 200 || glyph.Body.Len() < 1000 {
			t.Errorf("glyphs for %q: %d, %d bytes", f, glyph.Code, glyph.Body.Len())
		}
	}
	if _, err := New(api.New(up.URL, "test-token", 4, log), store.NewIndex(),
		Config{SiteURL: "https://www.wingview.com", MapTiles: "https://i.wvcdn.com/v4.pmtiles", MapTerrain: "https://tiles.example.com/terrain.webp"}, log); err == nil {
		t.Error("a terrain URL without {z}/{x}/{y} was accepted")
	}
	plain := get(t, testServer(t), "/flight/ASA14").Body.String()
	if strings.Contains(plain, "site.js") || !strings.Contains(plain, "map-svg") {
		t.Error("without tiles the page must draw the SVG map and load no script")
	}
}

// A slow upstream lookup must not hold the airport page past the budget.
func TestAirportLiveLookupBudget(t *testing.T) {
	h := testServer(t)
	start := time.Now()
	rec := get(t, h, "/airport/KJFK")
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	if took := time.Since(start); took > liveLookupBudget+1500*time.Millisecond {
		t.Fatalf("airport page took %v with a slow lookup; budget is %v", took, liveLookupBudget)
	}
}

func TestRobotsOnContainerHost(t *testing.T) {
	rec := get(t, testServer(t), "/robots.txt")
	if !strings.Contains(rec.Body.String(), "Disallow: /\n") {
		t.Fatalf("container host robots.txt must disallow crawling, got %q", rec.Body.String())
	}
}

func TestStaticAssets(t *testing.T) {
	h := testServer(t)
	rec := get(t, h, "/tracker/static/site.css")
	if rec.Code != 200 || strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("unversioned site.css: %d %q (only hashed URLs may be immutable)", rec.Code, rec.Header().Get("Cache-Control"))
	}
	// versioned URLs carry the hash in the path, and CSS points at the same
	// font URL the preload link uses
	page := get(t, h, "/tracker/").Body.String()
	pre := regexp.MustCompile(`href="(/tracker/static/[0-9a-f]{10}/fonts/inter-latin\.woff2)"`).FindStringSubmatch(page)
	if pre == nil {
		t.Fatal("no hashed font preload on the page")
	}
	if !strings.Contains(rec.Body.String(), "url("+pre[1]+")") {
		t.Fatalf("css does not reference %s", pre[1])
	}
	css := regexp.MustCompile(`href="(/tracker/static/[0-9a-f]{10}/site\.css)"`).FindStringSubmatch(page)
	if css == nil {
		t.Fatal("no hashed stylesheet link")
	}
	if rec := get(t, h, css[1]); rec.Code != 200 || !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("hashed css: %d %q", rec.Code, rec.Header().Get("Cache-Control"))
	}
	if rec := get(t, h, "/tracker/static/0000000000/site.css"); rec.Code != 200 || strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("stale hash must not be cached forever: %d %q", rec.Code, rec.Header().Get("Cache-Control"))
	}
	if rec := get(t, h, "/tracker/search?q=AS14"); rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("search redirect is cacheable: %q", rec.Header().Get("Cache-Control"))
	}
}

// The flight page hands out a signed refresh URL; only that URL answers,
// with the page's changing parts and the moved aircraft.
func TestLiveRefresh(t *testing.T) {
	s := newTestServer(t)
	h := s.Handler()
	page := get(t, h, "/flight/ASA14")
	m := regexp.MustCompile(`data-live="(/tracker/live/f-live/[^"]+)"`).FindStringSubmatch(page.Body.String())
	if m == nil {
		t.Fatal("live flight page carries no refresh URL")
	}
	if !strings.Contains(page.Body.String(), "/live.js") {
		t.Error("live flight page does not load live.js")
	}
	for _, part := range []string{`data-part="status"`, `data-part="dock"`, `data-part="times"`, `data-part="track"`} {
		if !strings.Contains(page.Body.String(), part) {
			t.Errorf("page lacks %s", part)
		}
	}

	rec := get(t, h, m[1])
	if rec.Code != 200 {
		t.Fatalf("refresh: status %d %s", rec.Code, rec.Body)
	}
	for k, want := range map[string]string{"Content-Type": "application/json", "Cache-Control": "no-store", "X-Robots-Tag": "noindex", "Cross-Origin-Resource-Policy": "same-origin"} {
		if got := rec.Header().Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Error("refresh must not be readable cross-origin")
	}
	var a struct {
		Parts map[string]string
		Map   struct{ Plane struct{ P [2]float64 } }
		Next  string
		Done  bool
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &a); err != nil {
		t.Fatal(err)
	}
	if a.Done || !strings.HasPrefix(a.Next, "/tracker/live/f-live/") {
		t.Errorf("done=%v next=%q", a.Done, a.Next)
	}
	// the dock keeps the essentials; the aircraft and the rest of the
	// position refresh in the side panel
	if !strings.Contains(a.Parts["dock"], "37,000 ft") || strings.Contains(a.Parts["dock"], "N527AS") || strings.Contains(a.Parts["dock"], "in 3D") {
		t.Errorf("dock part: want the new altitude, not the registration or a 3D button:\n%s", a.Parts["dock"])
	}
	if !strings.Contains(a.Parts["status"], "<strong>En route</strong>") || strings.Contains(a.Parts["dock"], `class="status`) {
		t.Errorf("status part: want the band, and only there:\n%s", a.Parts["status"])
	}
	if !strings.Contains(a.Parts["track"], "N527AS") || !strings.Contains(a.Parts["track"], "Squawk") {
		t.Errorf("track part lacks the registration or the squawk:\n%s", a.Parts["track"])
	}
	if !strings.Contains(a.Parts["times"], "Flight times") {
		t.Error("times part missing")
	}
	if a.Map.Plane.P != [2]float64{-121, 41.5} {
		t.Errorf("map plane at %v, want the moved position", a.Map.Plane.P)
	}

	tok := strings.TrimPrefix(m[1], "/tracker/live/f-live/")
	bad := map[string]string{
		"tampered":       m[1][:len(m[1])-1] + map[bool]string{true: "A", false: "B"}[!strings.HasSuffix(m[1], "A")],
		"another flight": "/tracker/live/f-other/" + tok,
		"expired":        s.liveURL("f-live", time.Now().Add(-7*time.Hour)),
		"no signature":   "/tracker/live/f-live/zzzz",
	}
	for name, u := range bad {
		if rec := get(t, h, u); rec.Code != 403 || rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s: status %d, cache %q", name, rec.Code, rec.Header().Get("Cache-Control"))
		}
	}

	// a flight that has closed and left the gateway: stop polling
	rec = get(t, h, s.liveURL("f-gone", time.Now()))
	if rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != `{"done":true}` {
		t.Errorf("closed flight: %d %s", rec.Code, rec.Body)
	}

	req := httptest.NewRequest("GET", "/robots.txt", nil)
	req.Host = "www.wingview.com"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if !strings.Contains(rr.Body.String(), "Disallow: /tracker/live/") {
		t.Errorf("robots.txt does not keep crawlers off the refresh:\n%s", rr.Body)
	}
}

// With the trail and plan endpoints answering, the flight map draws the
// path flown and the rest of the filed route, not great circles.
func TestFlightMapRealPath(t *testing.T) {
	up := fakeAPI(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s, err := New(api.New(up.URL, "test-token", 4, log), store.NewIndex(),
		Config{SiteURL: "https://www.wingview.com", AppURL: "https://app.wingview.com", MapTiles: "https://i.wvcdn.com/v4.pmtiles"}, log)
	if err != nil {
		t.Fatal(err)
	}
	h := s.Handler()
	lines := func(raw string) map[string][][2]float64 {
		var d struct {
			Lines []struct {
				C [][2]float64
				K string
			}
		}
		if err := json.Unmarshal([]byte(raw), &d); err != nil {
			t.Fatalf("map data: %v", err)
		}
		out := map[string][][2]float64{}
		for _, l := range d.Lines {
			out[l.K] = l.C
		}
		return out
	}
	has := func(c [][2]float64, p [2]float64) bool {
		for _, q := range c {
			if q == p {
				return true
			}
		}
		return false
	}

	body := get(t, h, "/flight/ASA14").Body.String()
	m := regexp.MustCompile(`data-map role="region"[^>]*><div class="map-canvas"></div><script type="application/json">([^<]+)</script>`).FindStringSubmatch(body)
	if m == nil {
		t.Fatal("no map data on the flight page")
	}
	l := lines(m[1])
	flown, ahead := l["flown"], l["ahead"]
	if !has(flown, [2]float64{-118.4, 35.5}) || !has(flown, [2]float64{-119.7, 38.2}) {
		t.Errorf("flown line is not the trail: %v", flown)
	}
	if has(flown, [2]float64{-125, 45}) {
		t.Error("flown line includes a dead-reckoned point")
	}
	if last := flown[len(flown)-1]; last != [2]float64{-120.2, 40.1} {
		t.Errorf("flown line ends at %v, not the aircraft", last)
	}
	if len(ahead) == 0 || ahead[0] != [2]float64{-120.2, 40.1} || !has(ahead, [2]float64{-121.5, 44}) || has(ahead, [2]float64{-118, 35}) {
		t.Errorf("ahead line is not the rest of the plan from the aircraft: %v", ahead)
	}

	// the live refresh sends the same shape, split at the new position
	u := regexp.MustCompile(`data-live="([^"]+)"`).FindStringSubmatch(body)
	if u == nil {
		t.Fatal("no refresh URL")
	}
	var a struct{ Map json.RawMessage }
	if err := json.Unmarshal(get(t, h, u[1]).Body.Bytes(), &a); err != nil {
		t.Fatal(err)
	}
	l = lines(string(a.Map))
	if f := l["flown"]; len(f) == 0 || f[len(f)-1] != [2]float64{-121, 41.5} || !has(f, [2]float64{-118.4, 35.5}) {
		t.Errorf("refresh flown line: %v", f)
	}
	if a := l["ahead"]; len(a) == 0 || a[0] != [2]float64{-121, 41.5} || !has(a, [2]float64{-121.5, 44}) || has(a, [2]float64{-120.6, 41}) {
		t.Errorf("refresh ahead line: %v", a)
	}
}

// Logos appear once the CDN has been seen to have them: never a broken
// image for an operator without one.
func TestOperatorLogos(t *testing.T) {
	up := fakeAPI(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	base := "https://logos.example.com/logos"
	s, err := New(api.New(up.URL, "test-token", 4, log), store.NewIndex(),
		Config{SiteURL: "https://www.wingview.com", AppURL: "https://app.wingview.com", LogoBase: base + "/"}, log)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.logoURL("ASA"); got != base+"/ASA/icon.png" {
		t.Fatalf("ASA logo = %q", got)
	}
	for _, code := range []string{"../x", "asa", "", "AB", "ABCD", "A1B"} {
		if got := s.logoURL(code); got != "" {
			t.Errorf("logo for %q = %q, want none", code, got)
		}
	}

	h := s.Handler()
	body := get(t, h, "/flight/ASA14").Body.String()
	if !strings.Contains(body, `<img class="band-logo" src="`+base+`/ASA/icon.png"`) {
		t.Error("flight page band has no airline logo")
	}
	rec := get(t, h, "/airport/KSEA")
	if !strings.Contains(rec.Body.String(), `<img class="op" src="`+base+`/ASA/icon.png"`) {
		t.Error("airport board has no logo beside the Alaska flight")
	}
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "img-src 'self' https://i.wvcdn.com https://logos.example.com") {
		t.Errorf("CSP does not allow the logo host: %s", csp)
	}

	off, err := New(api.New(up.URL, "test-token", 4, log), store.NewIndex(),
		Config{SiteURL: "https://www.wingview.com", AppURL: "https://app.wingview.com"}, log)
	if err != nil {
		t.Fatal(err)
	}
	if got := off.logoURL("ASA"); got != "" {
		t.Errorf("a logo with LOGOS=off: %q", got)
	}
}

// Open pages learn the build that rendered them and can ask which one the
// server runs now, so they reload after a deploy.
func TestVersionReload(t *testing.T) {
	up := fakeAPI(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s, err := New(api.New(up.URL, "test-token", 4, log), store.NewIndex(), Config{SiteURL: "https://www.wingview.com", Version: "v-test"}, log)
	if err != nil {
		t.Fatal(err)
	}
	h := s.Handler()
	page := get(t, h, "/aircraft/N527AS").Body.String()
	for _, want := range []string{`data-version="v-test"`, `/version.js" defer></script>`} {
		if !strings.Contains(page, want) {
			t.Errorf("page lacks %s", want)
		}
	}
	rec := get(t, h, "/tracker/version")
	if rec.Code != 200 || rec.Body.String() != "v-test" || rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("version: %d %q %q", rec.Code, rec.Body, rec.Header().Get("Cache-Control"))
	}
	if strings.Contains(get(t, testServer(t), "/aircraft/N527AS").Body.String(), "version.js") {
		t.Error("a build without a version must not poll for one")
	}
}

// One Flights panel, latest first: what is still to come (grayed,
// Scheduled), the current flight highlighted, then the past. Flight times
// sit in the side rail.
func TestFlightsPanel(t *testing.T) {
	h := testServer(t)
	order := func(t *testing.T, body string, wants ...string) {
		t.Helper()
		at := 0
		for _, w := range wants {
			i := strings.Index(body[at:], w)
			if i < 0 {
				t.Fatalf("lacks %q after offset %d", w, at)
			}
			at += i + len(w)
		}
	}
	flight := get(t, h, "/flight/ASA14").Body.String()
	order(t, flight,
		`<h2 id="flights-h">Flights</h2>`,
		`<tr class="fl-future">`, ">Scheduled</span>",
		`<tr class="fl-current" aria-current="true">`, "En Route",
		`<tr class="fl-past">`, "Arrived",
		"3 flights, last 30 days",
		`<aside class="col-side">`, `data-part="times"`)
	if strings.Contains(flight, "AS140") {
		t.Error("another flight number's plan is listed on ASA14")
	}
	if strings.Contains(flight, `id="hist-h"`) || strings.Contains(flight, `class="pager"`) {
		t.Error("old history title or a pager on a one-page list")
	}

	aircraft := get(t, h, "/aircraft/N527AS").Body.String()
	order(t, aircraft,
		`<h2 id="flights-h">Flights</h2>`, "<th scope=\"col\">Flight</th>",
		`<tr class="fl-future">`, `href="/flight/ASA15"`,
		`<tr class="fl-current" aria-current="true">`, `href="/flight/ASA14"`, " <small class=\"est\">est.</small>",
		// landed, still live in the gateway, not archived: it must not go missing
		`<tr class="fl-past">`, `href="/flight/ASA13"`,
		`<tr class="fl-past">`, `href="/flight/ASA14"`)
	if strings.Contains(aircraft, "ASA99") {
		t.Error("another airframe's arrival is listed")
	}
	for _, gone := range []string{"Recent flights", "Planned flights"} {
		if strings.Contains(aircraft, gone) {
			t.Errorf("aircraft page still has %q", gone)
		}
	}
}

// Pages of 25: the bare URL is page 1, later pages link both ways, and a
// page that does not exist redirects to one that does.
func TestFlightsPaging(t *testing.T) {
	rows := make([]FlightRowV, 60)
	req := func(q string) *http.Request { return httptest.NewRequest("GET", "/aircraft/N1"+q, nil) }

	v, to := pageFlights(req(""), rows, true, 30)
	if to != "" || v.Page != 1 || v.Pages != 3 || len(v.Rows) != 25 || v.Newer != "" || v.Older != "/aircraft/N1?page=2#flights" {
		t.Errorf("page 1: %+v %q", v, to)
	}
	v, to = pageFlights(req("?page=2"), rows, true, 30)
	if to != "" || v.First != 26 || v.Last != 50 || v.Total != 60 || v.Newer != "/aircraft/N1#flights" || v.Older != "/aircraft/N1?page=3#flights" {
		t.Errorf("page 2: %+v %q", v, to)
	}
	if v, _ := pageFlights(req("?page=3"), rows, true, 30); len(v.Rows) != 10 || v.Older != "" {
		t.Errorf("last page: %d rows, older %q", len(v.Rows), v.Older)
	}
	for q, want := range map[string]string{"?page=1": "/aircraft/N1", "?page=x": "/aircraft/N1", "?page=0": "/aircraft/N1", "?page=9": "/aircraft/N1?page=3"} {
		if _, to := pageFlights(req(q), rows, true, 30); to != want {
			t.Errorf("%s: redirect %q, want %q", q, to, want)
		}
	}
	many := make([]FlightRowV, 25*12)
	v, _ = pageFlights(req("?page=6"), many, true, 30)
	var ns []int
	for _, l := range v.Links {
		ns = append(ns, l.N)
		if l.Gap != (l.N == 4 || l.N == 12) {
			t.Errorf("link %d gap=%v", l.N, l.Gap)
		}
	}
	if fmt.Sprint(ns) != "[1 4 5 6 7 8 12]" {
		t.Errorf("links %v", ns)
	}
	rec := get(t, testServer(t), "/aircraft/N527AS?page=4")
	if rec.Code != 302 || rec.Header().Get("Location") != "/aircraft/N527AS" {
		t.Errorf("past the end: %d %q", rec.Code, rec.Header().Get("Location"))
	}
}

// Three earlier flights: oldest first (so the latest draws on top), each
// with its age, 3 down to 1; none without one.
func TestPastLinesAges(t *testing.T) {
	leg := func(dep, arr string) LegV {
		return LegV{Dep: airportV(dep, nil), Arr: airportV(arr, nil)}
	}
	var d mapData
	d.addPast([]LegV{leg("KSEA", "KSAN"), leg("KPDX", "KSEA"), leg("KLAX", "KPDX")}, make([][][2]float64, 3))
	var ages []float64
	for _, l := range d.Lines {
		if l.K == "past" {
			ages = append(ages, l.W)
		}
	}
	if fmt.Sprint(ages) != "[3 2 1]" {
		t.Errorf("ages %v, want [3 2 1]", ages)
	}
	if b, _ := json.Marshal(d); strings.Contains(string(b), `"k":"past"}`) {
		t.Errorf("a line without its age: %s", b)
	}
	if len(d.Pins) != 4 {
		t.Errorf("%d minor pins, want SEA SAN PDX LAX", len(d.Pins))
	}
}

// Parked on the field its latest flight arrived at: on the ground there,
// even when the last report's ground flag is off, and the dock shows that
// flight as the map and the Flights panel do.
func TestParkedAircraft(t *testing.T) {
	body := get(t, testServer(t), "/aircraft/N2PARK").Body.String()
	for _, want := range []string{"<strong>On the ground</strong>", "At SEA, ", `Last flew <a href="/flight/ASA14">AS14</a> for Alaska Airlines`,
		`<tr class="fl-current" aria-current="true">`} {
		if !strings.Contains(body, want) {
			t.Errorf("parked page lacks %q", want)
		}
	}
	if strings.Contains(body, "Not tracked now") {
		t.Error("an aircraft parked at its arrival airport reads as not tracked")
	}
	far := parkedAtFar()
	if _, ok := parkedAt(&far.leg, &far.last); ok {
		t.Error("a last report 100 miles from the arrival field counts as parked there")
	}
}

func parkedAtFar() struct {
	leg  LegV
	last api.LastPosition
} {
	return struct {
		leg  LegV
		last api.LastPosition
	}{LegV{Arr: airportV("KSEA", nil), Progress: 100}, api.LastPosition{Lat: 46.0, Lon: -122.3}}
}
