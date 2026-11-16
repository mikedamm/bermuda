package geo

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
)

// LL is a position in degrees.
type LL struct{ Lat, Lon float64 }

// Valid rejects the (0, 0) the feeds use for "no position".
func (p LL) Valid() bool {
	return !(p.Lat == 0 && p.Lon == 0) && math.Abs(p.Lat) <= 90 && math.Abs(p.Lon) <= 180
}

// MapRoute is a great-circle leg. With Pos set, the part up to Pos is drawn
// as flown and the rest as planned.
type MapRoute struct {
	From, To LL
	Pos      *LL
	Weight   float64 // 0..1, network maps: line emphasis
	Flown    bool    // the whole leg is history
}

// MapPin is an airport (or any labelled place) on the map.
type MapPin struct {
	At      LL
	Label   string
	Primary bool
}

// MapSpec describes one map. With Center set the view is centred there and
// zoomed so RadiusMi fits; otherwise it fits every route, pin and plane.
type MapSpec struct {
	W, H     int
	Routes   []MapRoute
	Pins     []MapPin
	Plane    *LL
	PlaneHdg float64
	Center   *LL
	RadiusMi float64
	Rings    []float64 // range rings in miles around Center
}

// Marker is an HTML overlay positioned in percent of the map box, so labels
// stay crisp and legible at any rendered width.
type Marker struct {
	X, Y    float64
	Kind    string // "pin", "pin primary", "plane"
	Label   string
	Rot     float64
	LabelAt string // "r" or "l"
}

// Map is the rendered result: SVG geometry plus overlay markers.
type Map struct {
	W, H    int
	SVG     string
	Markers []Marker
}

// ---------- projection ----------

type ortho struct {
	lat0, lon0       float64 // radians
	sinLat0, cosLat0 float64
	r                float64 // pixels per earth radius
	cx, cy           float64
	viewAngle        float64 // angular radius of the view (radians), for culling
	w, h             float64
}

func newOrtho(c LL, r float64, w, h int) ortho {
	o := ortho{lat0: c.Lat * rad, lon0: c.Lon * rad, r: r, cx: float64(w) / 2, cy: float64(h) / 2, w: float64(w), h: float64(h)}
	o.sinLat0, o.cosLat0 = math.Sincos(o.lat0)
	return o.withReach()
}

// reach is the farthest viewport corner from the projection centre; it sets
// the culling radius and whether the globe's limb is in view.
func (o ortho) reach() float64 {
	d := 0.0
	for _, c := range [][2]float64{{0, 0}, {o.w, 0}, {0, o.h}, {o.w, o.h}} {
		d = math.Max(d, math.Hypot(c[0]-o.cx, c[1]-o.cy))
	}
	return d
}

func (o ortho) withReach() ortho {
	d := o.reach()
	o.viewAngle = math.Pi / 2
	if d < o.r {
		o.viewAngle = math.Asin(d / o.r)
	}
	return o
}

// project returns screen coordinates; points on the far side of the globe
// are pushed to the horizon so polygons close along the limb.
func (o ortho) project(lat, lon float64) (x, y float64, front bool) {
	sinP, cosP := math.Sincos(lat * rad)
	sinL, cosL := math.Sincos(lon*rad - o.lon0)
	px := cosP * sinL
	py := o.cosLat0*sinP - o.sinLat0*cosP*cosL
	cosc := o.sinLat0*sinP + o.cosLat0*cosP*cosL
	if cosc < 0 {
		if n := math.Hypot(px, py); n > 0 {
			px, py = px/n, py/n
		}
	}
	return o.cx + px*o.r, o.cy - py*o.r, cosc >= 0
}

// visible bounds in degrees, for cheap culling before projecting.
func (o ortho) bounds() (minLat, maxLat, minLon, maxLon float64, allLon bool) {
	a := o.viewAngle/rad + 2
	lat0 := o.lat0 / rad
	minLat, maxLat = math.Max(-90, lat0-a), math.Min(90, lat0+a)
	if o.viewAngle >= math.Pi/2-1e-9 || maxLat >= 89 || minLat <= -89 {
		return minLat, maxLat, -180, 180, true
	}
	cl := math.Cos(math.Max(math.Abs(minLat), math.Abs(maxLat)) * rad)
	s := math.Sin(o.viewAngle) / cl
	if s >= 1 {
		return minLat, maxLat, -180, 180, true
	}
	d := math.Asin(s)/rad + 2
	return minLat, maxLat, o.lon0/rad - d, o.lon0/rad + d, false
}

func lonOverlap(a0, a1, b0, b1 float64) bool {
	for _, k := range []float64{0, -360, 360} {
		if a1 >= b0+k && a0 <= b1+k {
			return true
		}
	}
	return false
}

// ---------- fitting ----------

func centroid(pts []LL) LL {
	var x, y, z float64
	for _, p := range pts {
		sp, cp := math.Sincos(p.Lat * rad)
		sl, cl := math.Sincos(p.Lon * rad)
		x += cp * cl
		y += cp * sl
		z += sp
	}
	h := math.Hypot(x, y)
	return LL{Lat: math.Atan2(z, h) / rad, Lon: math.Atan2(y, x) / rad}
}

func fit(spec MapSpec) ortho {
	w, h := float64(spec.W), float64(spec.H)
	rMin := math.Min(w, h) * 0.46          // whole globe
	rMax := (w / 2) / math.Sin(35/earthMi) // ~70 mi across at most
	if spec.Center != nil {
		r := (h/2 - 18) / math.Sin(math.Max(spec.RadiusMi, 5)/earthMi)
		return newOrtho(*spec.Center, clamp(r, rMin, rMax), spec.W, spec.H)
	}
	var pts []LL
	for _, rt := range spec.Routes {
		pts = append(pts, rt.From, rt.To)
		for _, p := range greatCircle(rt.From, rt.To, 8) { // bulging arcs must fit too
			pts = append(pts, p)
		}
	}
	for _, p := range spec.Pins {
		pts = append(pts, p.At)
	}
	if spec.Plane != nil {
		pts = append(pts, *spec.Plane)
	}
	if len(pts) == 0 {
		return newOrtho(LL{30, -40}, rMin, spec.W, spec.H)
	}
	c := centroid(pts)
	// A great circle through the projection centre draws as a straight line.
	// Nudging the centre toward the equator makes routes bow poleward, the
	// way people expect a flight path to look.
	if len(spec.Routes) > 0 {
		span := 0.0
		for _, rt := range spec.Routes {
			span = math.Max(span, centralAngle(rt.From.Lat, rt.From.Lon, rt.To.Lat, rt.To.Lon)/rad)
		}
		shift := math.Min(span*0.22, 12)
		if c.Lat < 0 {
			shift = -shift
		}
		c.Lat -= shift
	}
	unit := newOrtho(c, 1, spec.W, spec.H)
	padX, padY := 64.0, 44.0
	bx0, by0, bx1, by1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, p := range pts {
		x, y, front := unit.project(p.Lat, p.Lon)
		if !front { // spread over more than a hemisphere: show the globe
			return newOrtho(c, rMin, spec.W, spec.H)
		}
		x, y = x-unit.cx, y-unit.cy
		bx0, by0, bx1, by1 = math.Min(bx0, x), math.Min(by0, y), math.Max(bx1, x), math.Max(by1, y)
	}
	r := rMax
	if bw := bx1 - bx0; bw > 1e-9 {
		r = math.Min(r, (w-2*padX)/bw)
	}
	if bh := by1 - by0; bh > 1e-9 {
		r = math.Min(r, (h-2*padY)/bh)
	}
	r = clamp(r, rMin, rMax)
	o := newOrtho(c, r, spec.W, spec.H)
	// centre the content's box rather than the projection centre
	o.cx -= (bx0 + bx1) / 2 * r
	o.cy -= (by0 + by1) / 2 * r
	return o.withReach()
}

func clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }

// ---------- geometry helpers ----------

func toVec(p LL) [3]float64 {
	sp, cp := math.Sincos(p.Lat * rad)
	sl, cl := math.Sincos(p.Lon * rad)
	return [3]float64{cp * cl, cp * sl, sp}
}

func fromVec(v [3]float64) LL {
	return LL{Lat: math.Atan2(v[2], math.Hypot(v[0], v[1])) / rad, Lon: math.Atan2(v[1], v[0]) / rad}
}

// greatCircle samples n+1 points from a to b along the great circle.
func greatCircle(a, b LL, n int) []LL {
	va, vb := toVec(a), toVec(b)
	d := centralAngle(a.Lat, a.Lon, b.Lat, b.Lon)
	out := make([]LL, 0, n+1)
	if d < 1e-9 {
		return append(out, a, b)
	}
	sd := math.Sin(d)
	for i := 0; i <= n; i++ {
		t := float64(i) / float64(n)
		k1, k2 := math.Sin((1-t)*d)/sd, math.Sin(t*d)/sd
		out = append(out, fromVec([3]float64{k1*va[0] + k2*vb[0], k1*va[1] + k2*vb[1], k1*va[2] + k2*vb[2]}))
	}
	return out
}

// Destination is the point dist miles from p along bearing (degrees true).
func Destination(p LL, bearing, distMi float64) LL {
	d := distMi / earthMi
	b := bearing * rad
	p1, l1 := p.Lat*rad, p.Lon*rad
	p2 := math.Asin(math.Sin(p1)*math.Cos(d) + math.Cos(p1)*math.Sin(d)*math.Cos(b))
	l2 := l1 + math.Atan2(math.Sin(b)*math.Sin(d)*math.Cos(p1), math.Cos(d)-math.Sin(p1)*math.Sin(p2))
	return LL{Lat: p2 / rad, Lon: math.Mod(l2/rad+540, 360) - 180}
}

type xy struct{ x, y float64 }

// clipPolygon is Sutherland–Hodgman against an axis-aligned rectangle.
func clipPolygon(pts []xy, x0, y0, x1, y1 float64) []xy {
	type edge struct {
		in  func(xy) bool
		cut func(a, b xy) xy
	}
	lerpX := func(a, b xy, x float64) xy { t := (x - a.x) / (b.x - a.x); return xy{x, a.y + t*(b.y-a.y)} }
	lerpY := func(a, b xy, y float64) xy { t := (y - a.y) / (b.y - a.y); return xy{a.x + t*(b.x-a.x), y} }
	edges := []edge{
		{func(p xy) bool { return p.x >= x0 }, func(a, b xy) xy { return lerpX(a, b, x0) }},
		{func(p xy) bool { return p.x <= x1 }, func(a, b xy) xy { return lerpX(a, b, x1) }},
		{func(p xy) bool { return p.y >= y0 }, func(a, b xy) xy { return lerpY(a, b, y0) }},
		{func(p xy) bool { return p.y <= y1 }, func(a, b xy) xy { return lerpY(a, b, y1) }},
	}
	for _, e := range edges {
		if len(pts) == 0 {
			return nil
		}
		out := make([]xy, 0, len(pts)+4)
		prev := pts[len(pts)-1]
		for _, cur := range pts {
			switch ci, pi := e.in(cur), e.in(prev); {
			case ci && pi:
				out = append(out, cur)
			case ci && !pi:
				out = append(out, e.cut(prev, cur), cur)
			case !ci && pi:
				out = append(out, e.cut(prev, cur))
			}
			prev = cur
		}
		pts = out
	}
	return pts
}

// clipSegment is Liang–Barsky; ok is false when the segment misses the box.
func clipSegment(a, b xy, x0, y0, x1, y1 float64) (xy, xy, bool) {
	t0, t1 := 0.0, 1.0
	dx, dy := b.x-a.x, b.y-a.y
	for _, pq := range [][2]float64{{-dx, a.x - x0}, {dx, x1 - a.x}, {-dy, a.y - y0}, {dy, y1 - a.y}} {
		p, q := pq[0], pq[1]
		if p == 0 {
			if q < 0 {
				return a, b, false
			}
			continue
		}
		t := q / p
		if p < 0 {
			if t > t1 {
				return a, b, false
			}
			t0 = math.Max(t0, t)
		} else {
			if t < t0 {
				return a, b, false
			}
			t1 = math.Min(t1, t)
		}
	}
	return xy{a.x + t0*dx, a.y + t0*dy}, xy{a.x + t1*dx, a.y + t1*dy}, true
}

// ---------- path writing ----------

type pathWriter struct {
	b   strings.Builder
	tol float64
}

func num(v float64) string { return strconv.FormatFloat(math.Round(v*10)/10, 'f', -1, 64) }

// ring writes a closed subpath, dropping points closer than tol to the last kept.
func (pw *pathWriter) ring(pts []xy) {
	kept := simplify(pts, pw.tol)
	if len(kept) < 3 {
		return
	}
	if pw.polyline(kept) {
		pw.b.WriteByte('z')
	}
}

// polyline writes "M x y l dx dy dx dy…" with one-decimal relative steps.
// Nothing is written when every step rounds to zero: a bare "l" is a path
// syntax error, and browsers drop everything after the first error.
func (pw *pathWriter) polyline(pts []xy) bool {
	if len(pts) < 2 {
		return false
	}
	r1 := func(v float64) float64 { return math.Round(v*10) / 10 }
	px, py := r1(pts[0].x), r1(pts[0].y)
	var steps strings.Builder
	for _, p := range pts[1:] {
		x, y := r1(p.x), r1(p.y)
		dx, dy := r1(x-px), r1(y-py)
		if dx == 0 && dy == 0 {
			continue
		}
		if steps.Len() > 0 && dx >= 0 {
			steps.WriteByte(' ')
		}
		steps.WriteString(num(dx))
		if dy >= 0 {
			steps.WriteByte(' ')
		}
		steps.WriteString(num(dy))
		px, py = x, y
	}
	if steps.Len() == 0 {
		return false
	}
	pw.b.WriteString("M" + num(r1(pts[0].x)) + " " + num(r1(pts[0].y)) + "l" + steps.String())
	return true
}

func simplify(pts []xy, tol float64) []xy {
	if len(pts) < 3 {
		return pts
	}
	t2 := tol * tol
	out := []xy{pts[0]}
	for _, p := range pts[1 : len(pts)-1] {
		l := out[len(out)-1]
		if (p.x-l.x)*(p.x-l.x)+(p.y-l.y)*(p.y-l.y) >= t2 {
			out = append(out, p)
		}
	}
	return append(out, pts[len(pts)-1])
}

// ---------- rendering ----------

var baseCache sync.Map // key → string; bounded by baseCacheReset

var baseCount int64
var baseMu sync.Mutex

const baseCacheMax = 4000

// RenderMap draws the basemap and overlays for spec. Pure apart from a memo
// of basemaps keyed by projection.
func RenderMap(spec MapSpec) Map {
	o := fit(spec)
	key := fmt.Sprintf("%.3f/%.3f/%.0f/%d/%d", o.lat0/rad, o.lon0/rad, o.r, spec.W, spec.H)
	var base string
	if v, ok := baseCache.Load(key); ok {
		base = v.(string)
	} else {
		base = renderBase(o)
		baseMu.Lock()
		if baseCount++; baseCount > baseCacheMax {
			baseCache.Clear()
			baseCount = 0
		}
		baseMu.Unlock()
		baseCache.Store(key, base)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, `<svg class="map-svg" viewBox="0 0 %d %d" preserveAspectRatio="xMidYMid slice" aria-hidden="true" focusable="false">`, spec.W, spec.H)
	sb.WriteString(base)

	pad := 40.0
	x0, y0, x1, y1 := -pad, -pad, o.w+pad, o.h+pad

	// range rings
	if spec.Center != nil {
		for _, mi := range spec.Rings {
			var pts []xy
			for b := 0.0; b < 360; b += 4 {
				d := Destination(*spec.Center, b, mi)
				x, y, _ := o.project(d.Lat, d.Lon)
				pts = append(pts, xy{x, y})
			}
			pw := pathWriter{tol: 0.5}
			pw.ring(clipPolygon(pts, x0, y0, x1, y1))
			if p := pw.b.String(); p != "" {
				sb.WriteString(`<path class="m-ring" d="` + p + `"/>`)
			}
			lbl := Destination(*spec.Center, 180, mi)
			if x, y, front := o.project(lbl.Lat, lbl.Lon); front && y < o.h-8 {
				fmt.Fprintf(&sb, `<text class="m-ring-label" x="%s" y="%s">%s mi</text>`, num(x+4), num(y-4), strconv.Itoa(int(mi)))
			}
		}
	}

	// routes
	for _, rt := range spec.Routes {
		if !rt.From.Valid() || !rt.To.Valid() {
			continue
		}
		n := int(clamp(centralAngle(rt.From.Lat, rt.From.Lon, rt.To.Lat, rt.To.Lon)/rad*2, 12, 240))
		segs := [][2]LL{{rt.From, rt.To}}
		classes := []string{"m-route"}
		switch {
		case rt.Flown:
			classes[0] = "m-route m-route-flown"
		case rt.Pos != nil && rt.Pos.Valid():
			segs = [][2]LL{{rt.From, *rt.Pos}, {*rt.Pos, rt.To}}
			classes = []string{"m-route m-route-flown", "m-route m-route-ahead"}
		default:
			classes[0] = "m-route m-route-ahead"
		}
		for i, s := range segs {
			var pts []xy
			for _, p := range greatCircle(s[0], s[1], n) {
				x, y, front := o.project(p.Lat, p.Lon)
				if !front {
					continue
				}
				pts = append(pts, xy{x, y})
			}
			pw := pathWriter{tol: 0.4}
			pw.polyline(clipLine(pts, x0, y0, x1, y1))
			if p := pw.b.String(); p != "" {
				style := ""
				if rt.Weight > 0 {
					style = fmt.Sprintf(` style="opacity:%.2f"`, 0.35+0.65*clamp(rt.Weight, 0, 1))
				}
				fmt.Fprintf(&sb, `<path class="%s" d="%s"%s/>`, classes[i], p, style)
			}
		}
	}
	sb.WriteString(`</svg>`)

	m := Map{W: spec.W, H: spec.H, SVG: sb.String()}
	// Labels are placed in order (primary pins first, then as given, so
	// callers list busier places first); one that would collide with a label
	// already placed tries the other side, then is dropped to a bare dot.
	type box struct{ x0, y0, x1, y1 float64 }
	var placed []box
	hits := func(b box) bool {
		for _, p := range placed {
			if b.x0 < p.x1 && b.x1 > p.x0 && b.y0 < p.y1 && b.y1 > p.y0 {
				return true
			}
		}
		return false
	}
	order := make([]int, 0, len(spec.Pins))
	for i, p := range spec.Pins {
		if p.Primary {
			order = append(order, i)
		}
	}
	for i, p := range spec.Pins {
		if !p.Primary {
			order = append(order, i)
		}
	}
	for _, i := range order {
		p := spec.Pins[i]
		x, y, front := o.project(p.At.Lat, p.At.Lon)
		if !front || x < 0 || y < 0 || x > o.w || y > o.h {
			continue
		}
		k := "pin"
		if p.Primary {
			k = "pin primary"
		}
		// labels are HTML at a fixed CSS size; on a phone the map is ~2.5x
		// smaller, so reserve generously in map units
		lw, lh := float64(len(p.Label))*11+18, 26.0
		right := box{x + 6, y - lh/2, x + 6 + lw, y + lh/2}
		left := box{x - 6 - lw, y - lh/2, x - 6, y + lh/2}
		first, second, side, other := right, left, "r", "l"
		if x > o.w*0.72 {
			first, second, side, other = left, right, "l", "r"
		}
		label := p.Label
		switch {
		case !hits(first):
			placed = append(placed, first)
		case !hits(second):
			placed = append(placed, second)
			side = other
		default:
			label = ""
		}
		placed = append(placed, box{x - 6, y - 6, x + 6, y + 6})
		m.Markers = append(m.Markers, Marker{X: pct(x, o.w), Y: pct(y, o.h), Kind: k, Label: label, LabelAt: side})
	}
	if spec.Plane != nil && spec.Plane.Valid() {
		x, y, front := o.project(spec.Plane.Lat, spec.Plane.Lon)
		if front && x >= 0 && y >= 0 && x <= o.w && y <= o.h {
			ahead := Destination(*spec.Plane, spec.PlaneHdg, 20)
			ax, ay, _ := o.project(ahead.Lat, ahead.Lon)
			rot := math.Atan2(ay-y, ax-x)/rad + 90
			if math.Hypot(ax-x, ay-y) < 1e-6 {
				rot = spec.PlaneHdg
			}
			m.Markers = append(m.Markers, Marker{X: pct(x, o.w), Y: pct(y, o.h), Kind: "plane", Rot: math.Round(rot)})
		}
	}
	return m
}

func pct(v, of float64) float64 { return math.Round(v/of*10000) / 100 }

// clipLine clips a polyline to the box, keeping the longest visible run
// (routes are short enough that a second run never matters).
func clipLine(pts []xy, x0, y0, x1, y1 float64) []xy {
	var best, cur []xy
	for i := 1; i < len(pts); i++ {
		a, b, ok := clipSegment(pts[i-1], pts[i], x0, y0, x1, y1)
		if !ok {
			if len(cur) > len(best) {
				best = cur
			}
			cur = nil
			continue
		}
		if len(cur) == 0 {
			cur = append(cur, a)
		}
		cur = append(cur, b)
	}
	if len(cur) > len(best) {
		best = cur
	}
	return best
}

func renderBase(o ortho) string {
	var sb strings.Builder
	w, h := o.w, o.h
	if o.r < o.reach() {
		fmt.Fprintf(&sb, `<rect class="m-space" width="%d" height="%d"/>`, int(w), int(h))
		fmt.Fprintf(&sb, `<circle class="m-ocean" cx="%s" cy="%s" r="%s"/>`, num(o.cx), num(o.cy), num(o.r))
	} else {
		fmt.Fprintf(&sb, `<rect class="m-ocean" width="%d" height="%d"/>`, int(w), int(h))
	}
	minLat, maxLat, minLon, maxLon, allLon := o.bounds()
	pad := 6.0
	x0, y0, x1, y1 := -pad, -pad, w+pad, h+pad

	// graticule
	step := gratStep(o.r)
	var grat pathWriter
	grat.tol = 0.6
	sample := math.Min(step/4, 2)
	line := func(pts []LL) {
		var run []xy
		flush := func() {
			for _, seg := range splitClip(run, x0, y0, x1, y1) {
				grat.polyline(seg)
			}
			run = nil
		}
		for _, p := range pts {
			x, y, front := o.project(p.Lat, p.Lon)
			if !front {
				flush()
				continue
			}
			run = append(run, xy{x, y})
		}
		flush()
	}
	lo0, lo1 := math.Floor(minLon/step)*step, math.Ceil(maxLon/step)*step
	if allLon {
		lo0, lo1 = -180, 180-step
	}
	la0, la1 := math.Max(-80, math.Floor(minLat/step)*step), math.Min(80, math.Ceil(maxLat/step)*step)
	for lon := lo0; lon <= lo1+1e-9; lon += step {
		var pts []LL
		for lat := la0; lat <= la1+1e-9; lat += sample {
			pts = append(pts, LL{lat, lon})
		}
		line(pts)
	}
	for lat := la0; lat <= la1+1e-9; lat += step {
		var pts []LL
		for lon := lo0; lon <= lo1+step+1e-9; lon += sample {
			pts = append(pts, LL{lat, lon})
		}
		line(pts)
	}
	if s := grat.b.String(); s != "" {
		sb.WriteString(`<path class="m-grat" d="` + s + `"/>`)
	}

	visible := func(r ring) bool {
		if r.maxLat < minLat || r.minLat > maxLat {
			return false
		}
		return allLon || lonOverlap(r.minLon, r.maxLon, minLon, maxLon)
	}

	// Wide views carry far more coastline per pixel; trade detail for size.
	viewMi := w / o.r * earthMi
	tol, minPx := 0.7, 1.5
	if viewMi > 3000 {
		tol, minPx = 1.2, 3.0
	}
	polys := func(layer, class string, minPx float64) {
		var pw pathWriter
		pw.tol = tol
		for _, f := range basemap[layer].features {
			if len(f) == 0 || !visible(f[0]) {
				continue
			}
			for _, r := range f {
				pts := make([]xy, 0, len(r.lon))
				anyFront := false
				bx0, by0, bx1, by1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
				for i := range r.lon {
					x, y, front := o.project(r.lat[i], r.lon[i])
					anyFront = anyFront || front
					pts = append(pts, xy{x, y})
					bx0, by0, bx1, by1 = math.Min(bx0, x), math.Min(by0, y), math.Max(bx1, x), math.Max(by1, y)
				}
				if !anyFront || bx1 < x0 || by1 < y0 || bx0 > x1 || by0 > y1 || (bx1-bx0 < minPx && by1-by0 < minPx) {
					continue
				}
				pw.ring(clipPolygon(pts, x0, y0, x1, y1))
			}
		}
		if s := pw.b.String(); s != "" {
			sb.WriteString(`<path class="` + class + `" fill-rule="evenodd" d="` + s + `"/>`)
		}
	}
	lines := func(layer, class string) {
		var pw pathWriter
		pw.tol = tol + 0.1
		for _, f := range basemap[layer].features {
			for _, r := range f {
				if !visible(r) {
					continue
				}
				var run []xy
				flush := func() {
					for _, seg := range splitClip(run, x0, y0, x1, y1) {
						pw.polyline(simplify(seg, pw.tol))
					}
					run = nil
				}
				for i := range r.lon {
					x, y, front := o.project(r.lat[i], r.lon[i])
					if !front {
						flush()
						continue
					}
					run = append(run, xy{x, y})
				}
				flush()
			}
		}
		if s := pw.b.String(); s != "" {
			sb.WriteString(`<path class="` + class + `" d="` + s + `"/>`)
		}
	}

	polys("land", "m-land", minPx)
	polys("lakes", "m-lake", minPx*2.5)
	// state and province lines only once the view is regional (< ~2500 mi across)
	if viewMi < 3600 {
		lines("states", "m-state")
	}
	lines("borders", "m-border")
	return sb.String()
}

// splitClip clips a polyline into the visible runs.
func splitClip(pts []xy, x0, y0, x1, y1 float64) [][]xy {
	var out [][]xy
	var cur []xy
	for i := 1; i < len(pts); i++ {
		a, b, ok := clipSegment(pts[i-1], pts[i], x0, y0, x1, y1)
		if !ok {
			if len(cur) > 1 {
				out = append(out, cur)
			}
			cur = nil
			continue
		}
		if len(cur) == 0 {
			cur = append(cur, a)
		} else if l := cur[len(cur)-1]; l != a {
			if len(cur) > 1 {
				out = append(out, cur)
			}
			cur = []xy{a}
		}
		cur = append(cur, b)
	}
	if len(cur) > 1 {
		out = append(out, cur)
	}
	return out
}

func gratStep(r float64) float64 {
	pxPerDeg := r * rad
	for _, s := range []float64{0.5, 1, 2, 5, 10, 15, 30} {
		if s*pxPerDeg >= 110 {
			return s
		}
	}
	return 30
}

// GreatCircle samples n+1 points along the great circle from a to b, with
// longitudes kept continuous across the antimeridian (for web map lines).
func GreatCircle(a, b LL, n int) []LL {
	pts := greatCircle(a, b, n)
	for i := 1; i < len(pts); i++ {
		for pts[i].Lon-pts[i-1].Lon > 180 {
			pts[i].Lon -= 360
		}
		for pts[i].Lon-pts[i-1].Lon < -180 {
			pts[i].Lon += 360
		}
	}
	return pts
}

// CentralAngleDeg is the angular distance between two points in degrees.
func CentralAngleDeg(a, b LL) float64 { return centralAngle(a.Lat, a.Lon, b.Lat, b.Lon) / rad }
