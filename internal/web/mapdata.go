package web

import (
	"encoding/json"
	"html/template"
	"math"
	"sort"

	"github.com/mikedamm/bermuda/internal/geo"
)

// MapV is a map as a page embeds it: the data the browser's Mapbox map
// draws, and, when no Mapbox token is configured (local development, tests),
// a server-drawn SVG instead.
type MapV struct {
	JSON  template.JS
	Label string // accessible description of what the map shows
	SVG   *geo.Map
}

type mapData struct {
	Lines  []mapLine    `json:"lines,omitempty"`
	Pins   []mapPin     `json:"pins,omitempty"`
	Plane  *mapPlane    `json:"plane,omitempty"`
	Planes []mapPlane   `json:"planes,omitempty"` // other live aircraft, each linking to its page
	Rings  []mapRing    `json:"rings,omitempty"`
	Focus  *[2]float64  `json:"focus,omitempty"` // centre for single-place maps
	Fit    [][2]float64 `json:"fit,omitempty"`   // frame these points, not everything
	Zoom   float64      `json:"zoom,omitempty"`
}

type mapLine struct {
	C [][2]float64 `json:"c"`
	K string       `json:"k"` // flown, ahead, net
	W float64      `json:"w,omitempty"`
}

type mapPin struct {
	P [2]float64 `json:"p"`
	L string     `json:"l"`
	K string     `json:"k,omitempty"` // subject (the page's own airport), main, minor
	U string     `json:"u,omitempty"` // page link
	N int        `json:"n,omitempty"` // weight for sizing
}

type mapPlane struct {
	P [2]float64 `json:"p"`
	H float64    `json:"h"`
	L string     `json:"l,omitempty"`
	U string     `json:"u,omitempty"`
}

type mapRing struct {
	C [2]float64 `json:"c"`
	R float64    `json:"r"` // statute miles
}

func pt(l geo.LL) [2]float64 {
	return [2]float64{math.Round(l.Lon*1e4) / 1e4, math.Round(l.Lat*1e4) / 1e4}
}

func arc(a, b geo.LL) [][2]float64 {
	n := int(math.Max(8, math.Min(160, geo.CentralAngleDeg(a, b)*2)))
	var out [][2]float64
	for _, p := range geo.GreatCircle(a, b, n) {
		out = append(out, pt(p))
	}
	return out
}

func encodeMap(d mapData) template.JS {
	b, _ := json.Marshal(d) // escapes <, >, & for the <script> element
	return template.JS(b)
}

// legMapV draws one leg: the route split at the live position, both
// airports, and the aircraft.
func (s *Server) legMapV(l LegV, label string) *MapV {
	arr := l.Arr
	if l.DivertedTo != nil && l.DivertedTo.LL.Valid() {
		arr = *l.DivertedTo
	}
	if !l.Dep.LL.Valid() && !arr.LL.Valid() && !l.HasPos {
		return nil
	}
	var d mapData
	for _, a := range []AirportV{l.Dep, arr} {
		if a.LL.Valid() {
			d.Pins = append(d.Pins, mapPin{P: pt(a.LL), L: a.Code, K: "main", U: a.URL})
		}
	}
	flying := l.Airborne && l.HasPos && l.Phase != "arrived" && l.Phase != "landed"
	if l.Dep.LL.Valid() && arr.LL.Valid() {
		switch {
		case flying:
			d.Lines = append(d.Lines, mapLine{C: arc(l.Dep.LL, l.Pos), K: "flown"}, mapLine{C: arc(l.Pos, arr.LL), K: "ahead"})
		case l.Progress >= 100:
			d.Lines = append(d.Lines, mapLine{C: arc(l.Dep.LL, arr.LL), K: "flown"})
		default:
			d.Lines = append(d.Lines, mapLine{C: arc(l.Dep.LL, arr.LL), K: "ahead"})
		}
	}
	if l.HasPos && l.Phase != "arrived" && l.Phase != "landed" {
		d.Plane = &mapPlane{P: pt(l.Pos), H: math.Round(l.Hdg)}
	}
	m := &MapV{Label: label}
	if s.cfg.MapboxToken == "" {
		m.SVG = legMap(l, 960, 420)
		return m
	}
	m.JSON = encodeMap(d)
	return m
}

// networkMapV draws a set of routes (an aircraft's month, an airline's week)
// with the busiest drawn strongest, plus an optional last position.
func (s *Server) networkMapV(routes []routeCount, plane *mapPlane, label string, svg func() *geo.Map) *MapV {
	if len(routes) == 0 && plane == nil {
		return nil
	}
	m := &MapV{Label: label}
	if s.cfg.MapboxToken == "" {
		m.SVG = svg()
		return m
	}
	var d mapData
	maxN, uses := 1, map[string]int{}
	airports := map[string]AirportV{}
	for _, r := range routes {
		maxN = max(maxN, r.n)
	}
	for _, r := range routes {
		d.Lines = append(d.Lines, mapLine{C: arc(r.a.LL, r.b.LL), K: "net", W: math.Round(float64(r.n)/float64(maxN)*100) / 100})
		for _, ap := range []AirportV{r.a, r.b} {
			uses[ap.ICAO] += r.n
			airports[ap.ICAO] = ap
		}
	}
	codes := make([]string, 0, len(airports))
	for c := range airports {
		codes = append(codes, c)
	}
	sort.Slice(codes, func(i, j int) bool {
		if uses[codes[i]] != uses[codes[j]] {
			return uses[codes[i]] > uses[codes[j]]
		}
		return codes[i] < codes[j]
	})
	for i, c := range codes {
		k := "minor"
		if i < 6 {
			k = "main"
		}
		ap := airports[c]
		d.Pins = append(d.Pins, mapPin{P: pt(ap.LL), L: ap.Code, K: k, U: ap.URL, N: uses[c]})
	}
	d.Plane = plane
	if len(routes) == 0 && plane != nil {
		d.Focus, d.Zoom = &plane.P, 7
	}
	m.JSON = encodeMap(d)
	return m
}

type routeCount struct {
	a, b AirportV
	n    int
}

// placeMapV centres on one place with range rings and nearby airports.
func (s *Server) placeMapV(center geo.LL, code, url string, ringsMi []float64, nearby []AirportV, planes []mapPlane, label string, svg func() *geo.Map) *MapV {
	m := &MapV{Label: label}
	if s.cfg.MapboxToken == "" {
		m.SVG = svg()
		return m
	}
	d := mapData{Focus: &[2]float64{math.Round(center.Lon*1e4) / 1e4, math.Round(center.Lat*1e4) / 1e4}, Zoom: 7.4}
	d.Pins = append(d.Pins, mapPin{P: pt(center), L: code, K: "subject", U: url}) // drawn above live traffic
	for _, n := range nearby {
		d.Pins = append(d.Pins, mapPin{P: pt(n.LL), L: n.Code, K: "minor", U: n.URL})
	}
	for _, r := range ringsMi {
		d.Rings = append(d.Rings, mapRing{C: pt(center), R: r})
	}
	d.Planes = planes
	m.JSON = encodeMap(d)
	return m
}

// pointsMapV marks a set of places (the busiest airports now), sized by n.
func (s *Server) pointsMapV(pins []mapPin, planes []mapPlane, label string, svg func() *geo.Map) *MapV {
	if len(pins) == 0 {
		return nil
	}
	m := &MapV{Label: label}
	if s.cfg.MapboxToken == "" {
		m.SVG = svg()
		return m
	}
	// A world of pins puts some region under the docked panel whatever the
	// centre. Frame the busiest airport's region instead: every pin within
	// 2,500 miles of it, and the live flights there.
	top := pins[0]
	for _, p := range pins {
		if p.N > top.N {
			top = p
		}
	}
	near := func(p [2]float64) bool {
		return geo.DistanceMi(top.P[1], top.P[0], p[1], p[0]) <= 2500
	}
	var fit [][2]float64
	for _, p := range pins {
		if near(p.P) {
			fit = append(fit, p.P)
		}
	}
	for _, p := range planes {
		if near(p.P) {
			fit = append(fit, p.P)
		}
	}
	m.JSON = encodeMap(mapData{Pins: pins, Planes: planes, Fit: fit})
	return m
}
