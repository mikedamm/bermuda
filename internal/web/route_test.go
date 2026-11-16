package web

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/mikedamm/bermuda/internal/api"
	"github.com/mikedamm/bermuda/internal/geo"
)

func TestTrackPointDecode(t *testing.T) {
	var ps []api.TrackPoint
	in := `[[1763324409000, 38.9012, -77.0371, 31000, "adsb", 450, 85, 0], [1763324410000.0, 38.9, -77.0, null, "mlat", null, null, 1]]`
	if err := json.Unmarshal([]byte(in), &ps); err != nil {
		t.Fatal(err)
	}
	want := []api.TrackPoint{
		{T: 1763324409000, Lat: 38.9012, Lon: -77.0371, Alt: 31000, Class: "adsb", GS: 450, Hdg: 85},
		{T: 1763324410000, Lat: 38.9, Lon: -77.0, Class: "mlat", Interp: true},
	}
	if !reflect.DeepEqual(ps, want) {
		t.Errorf("got %+v", ps)
	}
	if err := json.Unmarshal([]byte(`[[1, 2]]`), &ps); err == nil {
		t.Error("a point without a longitude should not decode")
	}
}

func TestPlanAhead(t *testing.T) {
	plan := [][2]float64{{-117, 32}, {-118, 35}, {-119.5, 38}, {-120.6, 41}, {-122, 47}}
	got := planAhead(plan, geo.LL{Lat: 39.5, Lon: -120.3})
	want := [][2]float64{{-120.3, 39.5}, {-120.6, 41}, {-122, 47}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ahead = %v, want %v", got, want)
	}
	if got := planAhead(plan, geo.LL{Lat: 39.5, Lon: -110}); got != nil {
		t.Errorf("an aircraft 10° off its route should not be joined to it: %v", got)
	}
}

// Houston to Tokyo crosses the antimeridian: the line must stay continuous.
func TestRouteAcrossAntimeridian(t *testing.T) {
	got := unwrap([][2]float64{{170, 30}, {179.5, 31}, {-179.5, 32}, {-170, 33}})
	want := [][2]float64{{170, 30}, {179.5, 31}, {180.5, 32}, {190, 33}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("unwrap = %v", got)
	}
	// the aircraft west of the line, its route continuing east of it
	plan := [][2]float64{{-150, 40}, {-170, 42}, {178, 40}, {150, 36}}
	ahead := planAhead(plan, geo.LL{Lat: 41, Lon: -175})
	if ahead == nil || ahead[0] != [2]float64{-175, 41} || ahead[1] != [2]float64{-182, 40} {
		t.Errorf("ahead across the antimeridian = %v", ahead)
	}
}

func TestTrailCoords(t *testing.T) {
	tr := &api.TrackAnswer{Live: true,
		Trail: []api.TrackPoint{{T: 0, Lat: 30, Lon: -100}, {T: 60_000, Lat: 31, Lon: -101, Class: "est"}, {T: 120_000, Lat: 32, Lon: -102}, {T: 400_000, Lat: 33, Lon: -103}},
		Tail5m: []api.TrackPoint{
			{T: 300_000, Lat: 32.5, Lon: -102.5}, {T: 302_000, Lat: 32.51, Lon: -102.51}, {T: 311_000, Lat: 32.6, Lon: -102.6}, {T: 315_000, Lat: 32.7, Lon: -102.7},
		},
	}
	got := trailCoords(tr)
	// trail before the tail, est left out; the tail every 10 s plus its last point
	want := [][2]float64{{-100, 30}, {-102, 32}, {-102.5, 32.5}, {-102.6, 32.6}, {-102.7, 32.7}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("trail = %v, want %v", got, want)
	}
	archived := trailCoords(&api.TrackAnswer{Track: &api.LineString{Coordinates: [][2]float64{{-100, 30}, {-101, 31}}}})
	if len(archived) != 2 {
		t.Errorf("archived track = %v", archived)
	}
	if trailCoords(&api.TrackAnswer{Live: true}) != nil {
		t.Error("an empty trail should be nil, so the map falls back to a great circle")
	}
}

// A diverted flight's plan leads to the original destination: not drawn.
func TestDivertedIgnoresPlan(t *testing.T) {
	sea, pdx := airportV("KSEA", nil), airportV("KPDX", nil)
	l := LegV{Dep: airportV("KSAN", nil), Arr: sea, DivertedTo: &pdx, Airborne: true, HasPos: true, Phase: "en_route", Pos: geo.LL{Lat: 40, Lon: -120}}
	d, _ := legMapData(l, routeShape{plan: [][2]float64{{-117.2, 32.7}, {-120, 39.9}, {-122.3, 47.4}}})
	var ahead [][2]float64
	for _, ln := range d.Lines {
		if ln.K == "ahead" {
			ahead = ln.C
		}
	}
	if len(ahead) == 0 || ahead[len(ahead)-1] != pt(pdx.LL) {
		t.Errorf("ahead line %v does not end at the diversion airport", ahead)
	}
}

// Houston to Tokyo: the line runs west past -180°, so Narita's pin and the
// aircraft must sit beside it, not one world away (the map would zoom out to
// frame both copies).
func TestPacificLegStaysInOneWorld(t *testing.T) {
	l := LegV{Dep: airportV("KIAH", nil), Arr: airportV("RJAA", nil), Airborne: true, HasPos: true, Phase: "en_route", Pos: geo.LL{Lat: 52, Lon: 175}}
	d, _ := legMapData(l, routeShape{})
	for _, p := range d.Pins {
		if p.L == "NRT" && p.P[0] > -180 {
			t.Errorf("Narita pin at %v, east of the line's copy of the world", p.P)
		}
	}
	if d.Plane == nil || d.Plane.P[0] != 175-360 {
		t.Errorf("aircraft at %v, want it beside the line at -185", d.Plane)
	}
}
