package store

import (
	"fmt"
	"testing"
	"time"

	"github.com/mikedamm/bermuda/internal/api"
)

// An airport's boards teach it its airlines and where they fly: arrivals by
// where they came from, departures by where they went (a diversion's field
// over the plan's), each route on the days its flights were listed.
func TestRoutes(t *testing.T) {
	now := time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)
	live := func(op, dep, arr string, at time.Time) api.BoardRow {
		return api.BoardRow{BoardTime: at, Live: &api.FlightSummary{Operator: api.Operator{ICAO: op}, Dep: dep, Arr: arr}}
	}
	ref := func(icao string) *api.AirportRef { return &api.AirportRef{ICAO: icao} }
	day := 24 * time.Hour
	x := NewIndex()
	x.NoteBoard("KSEA", []api.BoardRow{
		live("ASA", "KPDX", "KSEA", now),
		live("ASA", "KPDX", "KSEA", now.Add(-time.Hour)), // the same day again
		live("ASA", "PDX", "KSEA", now),                  // the same airport by another code
		live("ASA", "PANC", "KSEA", now.Add(-2*day)),
		live("DAL", "KATL", "KSEA", now),
		live("", "KBFI", "KSEA", now),    // no airline
		live("N52", "KBFI", "KSEA", now), // not a designator
		{BoardTime: now, Live: &api.FlightSummary{Operator: api.Operator{ICAO: "EJA"}, Dep: "KSUN", Arr: "KSEA", LADDRedacted: true}}, // listed: not shown
	}, true, now)
	x.NoteBoard("KSEA", []api.BoardRow{
		live("ASA", "KSEA", "PHNL", now),
		live("BOE", "KSEA", "KSEA", now), // a test flight back to the field is no route
		{BoardTime: now.Add(-20 * day), Archived: &api.HistoryEntry{Operator: api.Operator{ICAO: "QXE"}, Dep: ref("KSEA"), Arr: ref("KGEG")}}, // too long ago
		{BoardTime: now.Add(-3 * day), Archived: &api.HistoryEntry{Operator: api.Operator{ICAO: "DAL"}, Dep: ref("KSEA"), Arr: ref("KSLC"), ActualArr: ref("KBOI")}},
	}, false, now)

	show := func(rs []AirlineRoutes) string {
		var out []string
		for _, r := range rs {
			others := map[string]bool{}
			for _, o := range r.Others {
				others[o] = true
			}
			out = append(out, fmt.Sprintf("%s:%d/%d", r.Airline, len(others), r.Days))
		}
		return fmt.Sprint(out)
	}
	since := now.Add(-RouteDays * day)
	if got, want := show(x.RoutesAt("KSEA", since)), "[ASA:3/3 DAL:2/2]"; got != want {
		t.Errorf("routes at KSEA: %s, want %s", got, want)
	}
	for _, r := range x.RoutesAt("KSEA", since) {
		if r.Airline == "DAL" && fmt.Sprint(sorted(r.Others)) != "[KATL KBOI]" {
			t.Errorf("Delta's places %v: the diverted flight counts where it went", r.Others)
		}
	}
	// a shorter window leaves the older days out
	if got, want := show(x.RoutesAt("KSEA", now.Add(-time.Hour))), "[ASA:2/2 DAL:1/1]"; got != want {
		t.Errorf("today's routes: %s, want %s", got, want)
	}

	// kept across a restart; the window's end drops a route with no day left
	dir := t.TempDir()
	if err := x.Save(dir); err != nil {
		t.Fatal(err)
	}
	y := NewIndex()
	if err := y.Load(dir); err != nil {
		t.Fatal(err)
	}
	y.Prune(now.Add(13 * day)) // the window starts yesterday: Anchorage (two days back) and the diversion (three) fall out
	if got, want := show(y.RoutesAt("KSEA", since)), "[ASA:2/2 DAL:1/1]"; got != want {
		t.Errorf("after a restart and a prune: %s, want %s", got, want)
	}
	if n := y.Status()["routes"]; n != 3 {
		t.Errorf("status counts %v routes, want 3", n)
	}
}

func sorted(s []string) []string {
	out := append([]string(nil), s...)
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

// A snapshot that holds one place under two codes loads as one route, its
// days merged.
func TestRoutesLoadMerges(t *testing.T) {
	x := NewIndex()
	x.routes["EGLL"] = map[string]*Route{
		"ROU YHZ":  {Airport: "EGLL", Airline: "ROU", Other: "YHZ", Days: []int32{20409, 20410}},
		"ROU CYHZ": {Airport: "EGLL", Airline: "ROU", Other: "CYHZ", Days: []int32{20410, 20411}},
	}
	x.dirty = true
	dir := t.TempDir()
	if err := x.Save(dir); err != nil {
		t.Fatal(err)
	}
	y := NewIndex()
	if err := y.Load(dir); err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(len(y.routes["EGLL"]), y.routes["EGLL"]["ROU CYHZ"]); got != "1 &{EGLL ROU CYHZ [20409 20410 20411]}" {
		t.Errorf("loaded %s", got)
	}
}
