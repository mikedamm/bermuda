package web

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
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
	aircraft := map[string]any{
		"key": "N527AS", "airframe_id": "af-1", "days": 30,
		"aircraft": map[string]any{"icao_type": "B738", "manufacturer": "Boeing", "model": "737-890", "name": "Boeing 737-890",
			"registration": "N527AS", "hex": "A6A300", "country": map[string]any{"iso": "US", "name": "United States"}, "type_description": "L2J"},
		"operator": op, "owner": map[string]any{"name": "ALASKA AIRLINES INC", "city": "SEATAC", "state": "WA", "country": "US"},
		"status":         map[string]any{"phase": "flying", "flight": live, "upcoming": []any{}},
		"stats":          map[string]any{"days": 30, "flights": 2, "hours": 5.5, "airports": []any{map[string]any{"code": "KSEA", "count": 2, "airport": sea}}},
		"recent_flights": []any{hist},
		"ladd":           map[string]any{"listed": false},
	}
	mux := http.NewServeMux()
	write := func(w http.ResponseWriter, v any) { json.NewEncoder(w).Encode(v) }
	mux.HandleFunc("GET /search/aircraft", func(w http.ResponseWriter, r *http.Request) {
		switch strings.ToUpper(r.URL.Query().Get("q")) {
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
	mux.HandleFunc("GET /aircraft/{key}/flights", func(w http.ResponseWriter, r *http.Request) {
		write(w, map[string]any{"key": r.PathValue("key"), "flights": []any{hist}})
	})
	mux.HandleFunc("GET /aircraft/{key}", func(w http.ResponseWriter, r *http.Request) {
		switch strings.ToUpper(r.PathValue("key")) {
		case "N527AS", "A6A300":
			write(w, aircraft)
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

func testServer(t *testing.T) http.Handler {
	up := fakeAPI(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s, err := New(api.New(up.URL, "test-token", 4, log), store.NewIndex(), Config{SiteURL: "https://www.wingview.com", AppURL: "https://app.wingview.com"}, log)
	if err != nil {
		t.Fatal(err)
	}
	return s.Handler()
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
			`"@type":"Flight"`, ">AS14</h1>", "Alaska Airlines", "In the air, landing in",
			`href="/aircraft/N527AS"`, `href="/airport/KSEA"`, "2h 30m", // history duration from off/on, not the "seen" span
		}},
		{"/flight/AS14", 301, "/flight/ASA14", nil},
		{"/flight/asa14", 301, "/flight/ASA14", nil},
		{"/flight/N527AS", 301, "/aircraft/N527AS", nil},
		{"/flight/ZZZ999", 404, "", []string{`<meta name="robots" content="noindex,follow">`}},
		{"/aircraft/N527AS", 200, "", []string{`<link rel="canonical" href="https://www.wingview.com/aircraft/N527AS">`, "Boeing 737-890", "Alaska Airlines Inc"}},
		{"/aircraft/a6a300", 301, "/aircraft/N527AS", nil},
		{"/aircraft/N0NE", 404, "", nil},
		{"/airport/SEA", 301, "/airport/KSEA", nil},
		{"/airport/KSEA", 200, "", []string{`"@type":"Airport"`, "Seattle–Tacoma International Airport", "Arriving at SEA", `href="/flight/ASA14"`}},
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
		if c.status == 200 && strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
			if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "max-age=") || !strings.Contains(cc, "stale-while-revalidate") {
				t.Errorf("%s: Cache-Control %q", c.path, cc)
			}
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

func TestRobotsOnContainerHost(t *testing.T) {
	rec := get(t, testServer(t), "/robots.txt")
	if !strings.Contains(rec.Body.String(), "Disallow: /\n") {
		t.Fatalf("container host robots.txt must disallow crawling, got %q", rec.Body.String())
	}
}

func TestStaticAssets(t *testing.T) {
	h := testServer(t)
	rec := get(t, h, "/tracker/static/site.css")
	if rec.Code != 200 || !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("site.css: %d %q", rec.Code, rec.Header().Get("Cache-Control"))
	}
	// fonts referenced from CSS carry the same hash as the preload link
	if !strings.Contains(rec.Body.String(), "fonts/inter-latin.woff2?v=") {
		t.Fatal("css font url is not hashed")
	}
}
