package web

import (
	"testing"
	"time"
)

func TestTitleCase(t *testing.T) {
	for in, want := range map[string]string{
		"ALASKA AIRLINES INC":   "Alaska Airlines Inc",
		"SEATTLE-TACOMA INTL":   "Seattle-Tacoma Intl",
		"BANK OF UTAH TRUSTEE":  "Bank of Utah Trustee",
		"NETJETS SALES LLC":     "Netjets Sales LLC",
		"Already Mixed Case Co": "Already Mixed Case Co",
	} {
		if got := titleCase(in); got != want {
			t.Errorf("titleCase(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDelay(t *testing.T) {
	s := time.Date(2026, 11, 15, 20, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		d     time.Duration
		text  string
		class string
	}{{0, "On time", "ontime"}, {3 * time.Minute, "3m late", "ontime"}, {225 * time.Minute, "3h 45m late", "late"}, {-12 * time.Minute, "12m early", "early"}} {
		if txt, cls := fmtDelay(s.Add(c.d), s); txt != c.text || cls != c.class {
			t.Errorf("delay %v = %q/%q, want %q/%q", c.d, txt, cls, c.text, c.class)
		}
	}
}

func TestFlightTime(t *testing.T) {
	t0 := time.Date(2026, 11, 15, 20, 0, 0, 0, time.UTC)
	z := time.Time{}
	if d := flightTime(t0, t0.Add(5*time.Hour), z, z); d != 5*time.Hour {
		t.Errorf("off-on = %v", d)
	}
	if d := flightTime(z, t0.Add(5*time.Hour), t0.Add(-20*time.Minute), z); d != 5*time.Hour+20*time.Minute {
		t.Errorf("out-on = %v", d)
	}
	if d := flightTime(z, z, z, z); d != 0 {
		t.Errorf("none = %v", d)
	}
	if d := flightTime(t0, t0.Add(31*time.Hour), z, z); d != 0 { // a "seen" span is not a flight
		t.Errorf("31h span = %v", d)
	}
}

func TestTimeCell(t *testing.T) {
	at := time.Date(2026, 11, 16, 4, 50, 0, 0, time.UTC)
	if c := timeCell(at, "America/Los_Angeles", "US"); c.Clock != "8:50 PM" || c.Zone != "PST" || c.Day != "Sun 15 Nov" {
		t.Errorf("US cell = %+v", c)
	}
	if c := timeCell(at, "Europe/London", "GB"); c.Clock != "4:50 AM" || c.Zone != "GMT" {
		t.Errorf("GB cell = %+v", c)
	}
}

// A late departure must never sit under a green "on time" band, even when the
// re-timed arrival estimate looks on schedule.
func TestBandLateDeparture(t *testing.T) {
	l := LegV{Phase: "en_route", PhaseWord: "En Route", DepDelay: "3h 56m late", DepDelayClass: "late",
		DepOut: TimeCell{Set: true}, ArrDelay: "Expected on time", ArrDelayClass: "ontime"}
	if b := l.Band(); b.Class != "late" || b.Detail != "Departed 3h 56m late" {
		t.Fatalf("band = %+v", b)
	}
	l.DepDelay, l.DepDelayClass = "On time", "ontime"
	if b := l.Band(); b.Class != "ok" || b.Detail != "On time" {
		t.Fatalf("on-time band = %+v", b)
	}
}
