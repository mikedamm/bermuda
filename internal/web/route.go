package web

import (
	"context"
	"math"
	"time"

	"github.com/mikedamm/bermuda/internal/api"
	"github.com/mikedamm/bermuda/internal/geo"
)

// routeShape is the path a leg really takes: the trail it flew and the
// route it filed, each nil when the API has none. The map falls back to
// great circles for whatever is missing.
type routeShape struct {
	flown [][2]float64 // [lon, lat], first report to last
	plan  [][2]float64 // the route in force, departure to arrival
}

const (
	maxTrailPoints = 1500
	tailStepMs     = 10_000 // the last five minutes come at full density; every 10 s is plenty
	offRouteDeg    = 2.5    // farther than this from its route, a flight is not following it
)

// legShape reads a flight's trail and plan in parallel, within the
// live-lookup budget; whatever is late or missing is left out (and still
// fills the caches for the next render).
func (s *Server) legShape(ctx context.Context, flightID string) routeShape {
	var sh routeShape
	if flightID == "" || s.cfg.MapTiles == "" { // the SVG fallback draws great circles only
		return sh
	}
	ch := make(chan routeShape, 2)
	go func() {
		var r routeShape
		if t, err := s.api.FlightTrack(ctx, flightID); err == nil {
			r.flown = trailCoords(t)
		}
		ch <- r
	}()
	go func() {
		var r routeShape
		if p, err := s.api.FlightPlan(ctx, flightID); err == nil {
			r.plan = planCoords(p)
		}
		ch <- r
	}()
	deadline := time.After(liveLookupBudget)
	for range 2 {
		select {
		case r := <-ch:
			if r.flown != nil {
				sh.flown = r.flown
			}
			if r.plan != nil {
				sh.plan = r.plan
			}
		case <-deadline:
			return sh
		case <-ctx.Done():
			return sh
		}
	}
	return sh
}

// trailCoords is the flown line: an archived flight's track, or a live
// flight's thinned trail with its last five minutes in finer detail.
// Dead-reckoned points are left out: the line shows where it was seen.
func trailCoords(t *api.TrackAnswer) [][2]float64 {
	var out [][2]float64
	if t.Track != nil {
		for _, c := range t.Track.Coordinates {
			if p := (geo.LL{Lat: c[1], Lon: c[0]}); p.Valid() {
				out = append(out, pt(p))
			}
		}
		return finishLine(thin(out, maxTrailPoints))
	}
	var tailFrom int64 = math.MaxInt64
	if len(t.Tail5m) > 0 {
		tailFrom = t.Tail5m[0].T
	}
	add := func(p api.TrackPoint) {
		if ll := (geo.LL{Lat: p.Lat, Lon: p.Lon}); p.Class != "est" && ll.Valid() {
			out = append(out, pt(ll))
		}
	}
	for _, p := range t.Trail {
		if p.T < tailFrom {
			add(p)
		}
	}
	var last int64
	for i, p := range t.Tail5m {
		if i == 0 || i == len(t.Tail5m)-1 || p.T-last >= tailStepMs {
			add(p)
			last = p.T
		}
	}
	return finishLine(thin(out, maxTrailPoints))
}

// planCoords is the plan's line through its waypoints.
func planCoords(p *api.PlanAnswer) [][2]float64 {
	if p.Plan == nil || p.Plan.Line == nil {
		return nil
	}
	var out [][2]float64
	for _, c := range p.Plan.Line.Coordinates {
		if ll := (geo.LL{Lat: c[1], Lon: c[0]}); ll.Valid() {
			out = append(out, pt(ll))
		}
	}
	return finishLine(out)
}

// planAhead is the rest of the route from where the aircraft is now: its
// position, then every point after the route segment nearest to it. Nil
// when the aircraft is far off the route (rerouted, or the route is only
// the likely one): a line from it to a route it is not on would mislead.
func planAhead(plan [][2]float64, pos geo.LL) [][2]float64 {
	if len(plan) < 2 || !pos.Valid() {
		return nil
	}
	best, bestD := 0, math.Inf(1)
	for i := 0; i+1 < len(plan); i++ {
		if d := segDistDeg(pos, plan[i], plan[i+1]); d < bestD {
			best, bestD = i, d
		}
	}
	if bestD > offRouteDeg {
		return nil
	}
	return unwrap(append([][2]float64{pt(pos)}, plan[best+1:]...))
}

// withEnd extends a trail to the aircraft's latest position, which may be
// newer than the trail's last point.
func withEnd(trail [][2]float64, pos geo.LL) [][2]float64 {
	if trail == nil || !pos.Valid() {
		return trail
	}
	p := pt(pos)
	if last := trail[len(trail)-1]; math.Abs(wrapLon(last[0]-p[0])) < 1e-4 && math.Abs(last[1]-p[1]) < 1e-4 {
		return trail
	}
	out := append(append([][2]float64(nil), trail...), p)
	return unwrap(out)
}

// segDistDeg is the distance from p to the segment a–b in degrees of
// latitude, on a local flat projection around p (enough to choose the
// nearest segment).
func segDistDeg(p geo.LL, a, b [2]float64) float64 {
	k := math.Cos(p.Lat * math.Pi / 180)
	ax, ay := wrapLon(a[0]-p.Lon)*k, a[1]-p.Lat
	bx, by := wrapLon(b[0]-p.Lon)*k, b[1]-p.Lat
	dx, dy := bx-ax, by-ay
	t := 0.0
	if l2 := dx*dx + dy*dy; l2 > 0 {
		t = math.Max(0, math.Min(1, -(ax*dx+ay*dy)/l2))
	}
	return math.Hypot(ax+t*dx, ay+t*dy)
}

func wrapLon(d float64) float64 {
	for d > 180 {
		d -= 360
	}
	for d < -180 {
		d += 360
	}
	return d
}

// unwrap keeps a line continuous across the antimeridian, the way
// geo.GreatCircle does: each longitude within 180° of the one before, so
// MapLibre draws Houston–Tokyo across the Pacific, not across the world.
func unwrap(c [][2]float64) [][2]float64 {
	for i := 1; i < len(c); i++ {
		for c[i][0]-c[i-1][0] > 180 {
			c[i][0] -= 360
		}
		for c[i][0]-c[i-1][0] < -180 {
			c[i][0] += 360
		}
	}
	return c
}

// thin keeps at most n points, always the first and last.
func thin(c [][2]float64, n int) [][2]float64 {
	if len(c) <= n || n < 2 {
		return c
	}
	out := make([][2]float64, 0, n)
	step := float64(len(c)-1) / float64(n-1)
	for i := range n {
		out = append(out, c[int(math.Round(float64(i)*step))])
	}
	return out
}

func finishLine(c [][2]float64) [][2]float64 {
	if len(c) < 2 {
		return nil
	}
	return unwrap(c)
}
