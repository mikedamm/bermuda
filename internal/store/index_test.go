package store

import (
	"sync"
	"testing"
	"time"

	"github.com/mikedamm/bermuda/internal/api"
)

func TestAirlineCallsign(t *testing.T) {
	for cs, want := range map[string]bool{"UAL1275": true, "ASA14": true, "SWA12AB": true, "N527AS": false, "CFABC": false, "GABCD": false, "": false} {
		if _, _, ok := AirlineCallsign(cs); ok != want {
			t.Errorf("AirlineCallsign(%q) = %v, want %v", cs, ok, want)
		}
	}
}

// A sweep that sees the callsign arriving somewhere new must not splice the
// old origin onto the new destination.
func TestRouteNotSpliced(t *testing.T) {
	x := NewIndex()
	t0 := time.Now().Add(-time.Hour)
	x.NoteLive(api.FlightSummary{Callsign: "ASA14", Dep: "KSAN", Arr: "KDCA", LastSeen: t0})
	x.NoteSweep("KSEA", &api.AirportTraffic{Aircraft: []api.NearbyAircraft{{Callsign: "ASA14", Direction: "arriving", Status: "active"}}}, t0.Add(30*time.Minute))
	f, _ := x.Flight("ASA14")
	if f.Arr != "KSEA" || f.Dep != "" {
		t.Fatalf("route = %s-%s, want -KSEA", f.Dep, f.Arr)
	}
	x.NoteSweep("KSEA", &api.AirportTraffic{Aircraft: []api.NearbyAircraft{{Callsign: "ASA14", Direction: "arriving", Status: "active"}}}, t0.Add(40*time.Minute))
	x.NoteLive(api.FlightSummary{Callsign: "ASA14", Dep: "KPDX", Arr: "KSEA", LastSeen: t0.Add(45 * time.Minute)})
	if f, _ = x.Flight("ASA14"); f.Dep != "KPDX" || f.Arr != "KSEA" {
		t.Fatalf("route = %s-%s, want KPDX-KSEA", f.Dep, f.Arr)
	}
}

func TestSnapshotRoundTrip(t *testing.T) {
	dir := t.TempDir()
	x := NewIndex()
	now := time.Now()
	x.NoteLive(api.FlightSummary{Callsign: "UAL1275", IATAFlight: "UA1275", Dep: "MMUN", Arr: "KLAX", Registration: "N37555",
		Operator: api.Operator{ICAO: "UAL", IATA: "UA", Name: "United Airlines"}, LastSeen: now})
	var wg sync.WaitGroup
	for range 4 { // concurrent saves must serialise, not tear the file
		wg.Add(1)
		go func() { defer wg.Done(); _ = x.Save(dir) }()
	}
	wg.Wait()
	y := NewIndex()
	if err := y.Load(dir); err != nil {
		t.Fatal(err)
	}
	f, ok := y.Flight("UAL1275")
	if !ok || f.Dep != "MMUN" || f.IATA != "UA1275" {
		t.Fatalf("loaded flight = %+v, %v", f, ok)
	}
	if a, ok := y.AirlineByIATA("UA"); !ok || a.ICAO != "UAL" {
		t.Fatalf("airline by IATA = %+v, %v", a, ok)
	}
}
