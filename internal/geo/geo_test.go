package geo

import (
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestLookupAirport(t *testing.T) {
	for _, c := range []struct{ code, icao string }{{"KSEA", "KSEA"}, {"sea", "KSEA"}, {"LHR", "EGLL"}, {"CUN", "MMUN"}} {
		a, ok := LookupAirport(c.code)
		if !ok || a.ICAO != c.icao {
			t.Errorf("LookupAirport(%q) = %q, %v; want %q", c.code, a.ICAO, ok, c.icao)
		}
	}
	if a, _ := LookupAirport("KSEA"); a.TZ != "America/Los_Angeles" || a.Place() != "Seattle, WA" {
		t.Errorf("KSEA tz/place = %q / %q", a.TZ, a.Place())
	}
	if a, _ := LookupAirport("EGLL"); a.Place() != "London, United Kingdom" {
		t.Errorf("EGLL place = %q", a.Place())
	}
}

func TestDistance(t *testing.T) {
	sea, _ := LookupAirport("KSEA")
	lax, _ := LookupAirport("KLAX")
	if d := DistanceMi(sea.Lat, sea.Lon, lax.Lat, lax.Lon); math.Abs(d-954) > 10 {
		t.Errorf("SEA-LAX = %.0f mi, want ~954", d)
	}
}

var pathRe = regexp.MustCompile(` d="([^"]*)"`)

// Every path must be valid enough that a browser parses it to the end: no
// bare commands, no NaN.
func checkPaths(t *testing.T, svg string) {
	t.Helper()
	if strings.Contains(svg, "NaN") || strings.Contains(svg, "Inf") {
		t.Fatal("svg contains NaN/Inf")
	}
	for _, m := range pathRe.FindAllStringSubmatch(svg, -1) {
		if strings.Contains(m[1], "lM") || strings.HasSuffix(m[1], "l") || strings.Contains(m[1], "lz") {
			t.Fatalf("bare l command in path: …%s…", m[1][:min(80, len(m[1]))])
		}
	}
}

func TestRenderMaps(t *testing.T) {
	ap := func(c string) LL { a, _ := LookupAirport(c); return LL{a.Lat, a.Lon} }
	pos := LL{33.87, -114.0}
	cases := map[string]MapSpec{
		"cun-lax": {W: 960, H: 440, Routes: []MapRoute{{From: ap("MMUN"), To: ap("KLAX"), Pos: &pos}},
			Pins: []MapPin{{At: ap("MMUN"), Label: "CUN", Primary: true}, {At: ap("KLAX"), Label: "LAX", Primary: true}}, Plane: &pos, PlaneHdg: 292},
		"san-dca": {W: 960, H: 440, Routes: []MapRoute{{From: ap("KSAN"), To: ap("KDCA"), Flown: true}},
			Pins: []MapPin{{At: ap("KSAN"), Label: "SAN"}, {At: ap("KDCA"), Label: "DCA"}}},
		"sea-lhr": {W: 960, H: 440, Routes: []MapRoute{{From: ap("KSEA"), To: ap("EGLL")}},
			Pins: []MapPin{{At: ap("KSEA"), Label: "SEA"}, {At: ap("EGLL"), Label: "LHR"}}},
		"sea-airport": {W: 960, H: 360, Center: ptr(ap("KSEA")), RadiusMi: 50, Rings: []float64{25, 50},
			Pins: []MapPin{{At: ap("KSEA"), Label: "SEA", Primary: true}, {At: ap("KPAE"), Label: "PAE"}}},
		"sea-pdx": {W: 960, H: 440, Routes: []MapRoute{{From: ap("KSEA"), To: ap("KPDX")}},
			Pins: []MapPin{{At: ap("KSEA"), Label: "SEA"}, {At: ap("KPDX"), Label: "PDX"}}},
	}
	dir := os.Getenv("MAP_OUT")
	for name, spec := range cases {
		m := RenderMap(spec)
		checkPaths(t, m.SVG)
		if len(m.SVG) > 120_000 {
			t.Errorf("%s: svg is %d bytes", name, len(m.SVG))
		}
		if !strings.Contains(m.SVG, "m-land") {
			t.Errorf("%s: no land drawn", name)
		}
		t.Logf("%s: %d bytes, %d markers", name, len(m.SVG), len(m.Markers))
		if dir != "" {
			css := `<style>body{margin:0;background:#fff}.wrap{position:relative;width:960px}.map-svg{display:block;width:100%;height:auto}
.m-space{fill:#f1f4f8}.m-ocean{fill:#dcebf8}.m-land{fill:#fbfcfe;stroke:#b9cbe0;stroke-width:.6}.m-lake{fill:#dcebf8;stroke:#b9cbe0;stroke-width:.5}
.m-grat{fill:none;stroke:#c9def3;stroke-width:.5}.m-border{fill:none;stroke:#9fb3cc;stroke-width:.7}.m-state{fill:none;stroke:#d3dde9;stroke-width:.6}
.m-route{fill:none;stroke-width:2.5;stroke-linecap:round}.m-route-flown{stroke:#0065ad}.m-route-ahead{stroke:#229cf8;stroke-dasharray:2 6}
.m-ring{fill:none;stroke:#229cf8;stroke-dasharray:3 4}.m-ring-label{font:11px sans-serif;fill:#53627a}
.mk{position:absolute;transform:translate(-50%,-50%);font:600 13px sans-serif;color:#002f64}</style>`
			var mk strings.Builder
			for _, k := range m.Markers {
				mk.WriteString(`<div class="mk" style="left:` + ftoa(k.X) + `%;top:` + ftoa(k.Y) + `%">● ` + k.Label + `</div>`)
			}
			html := `<!doctype html><html><head>` + css + `</head><body><div class="wrap">` + m.SVG + mk.String() + `</div></body></html>`
			os.WriteFile(filepath.Join(dir, name+".html"), []byte(html), 0o644)
		}
	}
}

func ptr[T any](v T) *T { return &v }

func ftoa(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }
