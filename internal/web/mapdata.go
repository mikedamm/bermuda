package web

import (
	"context"
	"encoding/json"
	"html/template"
	"math"
	"sort"
	"sync"

	"github.com/mikedamm/bermuda/internal/geo"
)

// MapV is a map as a page embeds it: the data the browser's MapLibre map
// draws, and, when no tiles are configured (tests), a server-drawn SVG instead.
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
	K string       `json:"k"` // flown, ahead, net, past (an earlier flight; W is its age, 1 the latest)
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
	P [2]float64   `json:"p"`
	H float64      `json:"h"`
	L string       `json:"l,omitempty"`
	U string       `json:"u,omitempty"`
	K string       `json:"k,omitempty"` // airport maps: arr, dep or pass, which colours it
	T [][2]float64 `json:"t,omitempty"` // airport maps: its last few minutes, oldest first, ending where it is
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
func (s *Server) legMapV(l LegV, sh routeShape, label string) *MapV {
	d, ok := legMapData(l, sh)
	if !ok {
		return nil
	}
	m := &MapV{Label: label}
	if s.cfg.MapTiles == "" {
		m.SVG = legMap(l, 960, 420)
		return m
	}
	m.JSON = encodeMap(d)
	return m
}

// legMapData is what the interactive map draws for one leg: the trail it
// flew and the rest of its filed route when the API has them, great circles
// when not. The live refresh sends it again as the aircraft moves.
func legMapData(l LegV, sh routeShape) (mapData, bool) {
	arr := l.Arr
	if l.DivertedTo != nil && l.DivertedTo.LL.Valid() {
		arr = *l.DivertedTo
	}
	if !l.Dep.LL.Valid() && !arr.LL.Valid() && !l.HasPos {
		return mapData{}, false
	}
	var d mapData
	for _, a := range []AirportV{l.Dep, arr} {
		if a.LL.Valid() {
			d.Pins = append(d.Pins, mapPin{P: pt(a.LL), L: a.Code, K: "main", U: a.URL})
		}
	}
	flying := l.Airborne && l.HasPos && l.Phase != "arrived" && l.Phase != "landed"
	both := l.Dep.LL.Valid() && arr.LL.Valid()
	plan := sh.plan
	if l.DivertedTo != nil {
		plan = nil // it leads where the flight is no longer going
	}
	line := func(c [][2]float64, k string) {
		if len(c) < 2 {
			return
		}
		// each line starts in the copy of the world where the one before it
		// ends (the ahead line begins at the raw position; the flown line may
		// have run past ±180° to reach it)
		if n := len(d.Lines); n > 0 {
			prev := d.Lines[n-1].C
			if shift := 360 * math.Round((prev[len(prev)-1][0]-c[0][0])/360); shift != 0 {
				moved := make([][2]float64, len(c))
				for i, p := range c {
					moved[i] = [2]float64{p[0] + shift, p[1]}
				}
				c = moved
			}
		}
		d.Lines = append(d.Lines, mapLine{C: c, K: k})
	}
	switch {
	case flying:
		if sh.flown != nil {
			line(withEnd(sh.flown, l.Pos), "flown")
		} else if l.Dep.LL.Valid() {
			line(arc(l.Dep.LL, l.Pos), "flown")
		}
		if ahead := planAhead(plan, l.Pos); ahead != nil {
			line(ahead, "ahead")
		} else if arr.LL.Valid() {
			line(arc(l.Pos, arr.LL), "ahead")
		}
	case l.Progress >= 100:
		if sh.flown != nil {
			line(sh.flown, "flown")
		} else if both {
			line(arc(l.Dep.LL, arr.LL), "flown")
		}
	default:
		if plan != nil {
			line(plan, "ahead")
		} else if both {
			line(arc(l.Dep.LL, arr.LL), "ahead")
		}
	}
	if l.HasPos && l.Phase != "arrived" && l.Phase != "landed" {
		d.Plane = &mapPlane{P: pt(l.Pos), H: math.Round(l.Hdg)}
	}
	// a route across the antimeridian runs past ±180°: keep the airports and
	// the aircraft in the same copy of the world as the line beside them, or
	// the camera frames both copies and shows the whole planet
	for i := range d.Pins {
		d.Pins[i].P = alignLon(d.Lines, d.Pins[i].P)
	}
	if d.Plane != nil {
		d.Plane.P = alignLon(d.Lines, d.Plane.P)
	}
	return d, true
}

// aircraftMapV draws an aircraft's flight under way (else its latest one)
// as that flight's page does, the flights before it as gray lines fading
// with age, and, when it is not flying, where it was last seen. prev is
// latest first.
func (s *Server) aircraftMapV(ctx context.Context, cur LegV, prev []LegV, parked *mapPlane, label string, svg func() *geo.Map) *MapV {
	m := &MapV{Label: label}
	if s.cfg.MapTiles == "" {
		m.SVG = svg()
		return m
	}
	var sh routeShape
	tracks := make([][][2]float64, len(prev))
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		sh = s.legShape(ctx, cur.FlightID)
	}()
	for i, l := range prev {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tracks[i] = s.flownLine(ctx, l.FlightID)
		}()
	}
	wg.Wait()
	d, _ := legMapData(cur, sh)
	d.addPast(prev, tracks)
	if d.Plane == nil && parked != nil {
		p := *parked
		p.P = alignLon(d.Lines, p.P)
		d.Plane = &p
	}
	if len(d.Lines) == 0 && len(d.Pins) == 0 && d.Plane == nil {
		return nil
	}
	m.JSON = encodeMap(d)
	return m
}

// addPast draws earlier flights (latest first) under what the map shows:
// each its track, else a great circle, in a gray that lightens with age,
// the oldest first so the latest lies on top. Their airports join as minor
// pins.
func (d *mapData) addPast(legs []LegV, tracks [][][2]float64) {
	pinned := map[string]bool{}
	for _, p := range d.Pins {
		pinned[p.L] = true
	}
	for i := len(legs) - 1; i >= 0; i-- {
		l := legs[i]
		arr := l.Arr
		if l.DivertedTo != nil {
			arr = *l.DivertedTo
		}
		c := tracks[i]
		if len(c) < 2 && l.Dep.LL.Valid() && arr.LL.Valid() && l.Dep.ICAO != arr.ICAO {
			c = arc(l.Dep.LL, arr.LL)
		}
		if len(c) < 2 {
			continue
		}
		c = alignLine(d.Lines, c)
		// W is the age, 1 the latest: never 0, which the JSON leaves out
		d.Lines = append(d.Lines, mapLine{C: c, K: "past", W: float64(i + 1)})
		for _, a := range []AirportV{l.Dep, arr} {
			if a.LL.Valid() && !pinned[a.Code] {
				pinned[a.Code] = true
				d.Pins = append(d.Pins, mapPin{P: alignLon(d.Lines, pt(a.LL)), L: a.Code, K: "minor", U: a.URL})
			}
		}
	}
}

// alignLine moves a whole line by turns of longitude into the copy of the
// world nearest the lines already drawn.
func alignLine(lines []mapLine, c [][2]float64) [][2]float64 {
	if len(lines) == 0 {
		return c
	}
	shift := alignLon(lines, c[0])[0] - c[0][0]
	if shift == 0 {
		return c
	}
	out := make([][2]float64, len(c))
	for i, p := range c {
		out[i] = [2]float64{p[0] + shift, p[1]}
	}
	return out
}

// alignLon moves p by whole turns of longitude to the copy of the world
// holding the nearest point of the lines.
func alignLon(lines []mapLine, p [2]float64) [2]float64 {
	near, best := p, math.Inf(1)
	for _, l := range lines {
		for _, c := range l.C {
			if d := math.Abs(wrapLon(c[0]-p[0])) + math.Abs(c[1]-p[1]); d < best {
				near, best = c, d
			}
		}
	}
	p[0] += 360 * math.Round((near[0]-p[0])/360)
	return p
}

// networkMapV draws a set of routes (an aircraft's month, an airline's week)
// with the busiest drawn strongest, plus an optional last position.
func (s *Server) networkMapV(routes []routeCount, plane *mapPlane, label string, svg func() *geo.Map) *MapV {
	if len(routes) == 0 && plane == nil {
		return nil
	}
	m := &MapV{Label: label}
	if s.cfg.MapTiles == "" {
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
	if s.cfg.MapTiles == "" {
		m.SVG = svg()
		return m
	}
	d := mapData{Focus: &[2]float64{math.Round(center.Lon*1e4) / 1e4, math.Round(center.Lat*1e4) / 1e4}, Zoom: 6.8}
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
	if s.cfg.MapTiles == "" {
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
