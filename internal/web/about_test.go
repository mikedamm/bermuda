package web

import (
	"testing"
	"time"
)

func legAt(t time.Time) LegV { return LegV{departed: t} }

func TestUsualTakeoff(t *testing.T) {
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	day := func(d, h, m int) time.Time { return time.Date(2026, 11, d, h, m, 0, 0, time.UTC) }
	// around midnight: 11:50 PM, 12:05 AM and 11:55 PM average to 11:57 PM
	// (a plain mean of the clock times would say 4 PM)
	got := usualTakeoff([]LegV{legAt(day(10, 23, 50)), legAt(day(12, 0, 5)), legAt(day(12, 23, 55))}, "UTC", "GB", now)
	if got != "11:55 PM UTC" {
		t.Errorf("around midnight: %q", got)
	}
	// to five minutes, in the departure's own zone
	ny := []LegV{legAt(day(10, 15, 2)), legAt(day(11, 14, 58)), legAt(day(12, 15, 9))} // 10:02, 9:58, 10:09 AM EST
	if got := usualTakeoff(ny, "America/New_York", "US", now); got != "10:05 AM EST" {
		t.Errorf("New York: %q", got)
	}
	// too few, or no habit
	if got := usualTakeoff(ny[:2], "UTC", "GB", now); got != "" {
		t.Errorf("two departures: %q", got)
	}
	scattered := []LegV{legAt(day(10, 6, 0)), legAt(day(11, 13, 0)), legAt(day(12, 19, 0))}
	if got := usualTakeoff(scattered, "UTC", "GB", now); got != "" {
		t.Errorf("scattered: %q", got)
	}
}

func TestFrequency(t *testing.T) {
	now := time.Date(2026, 11, 30, 12, 0, 0, 0, time.UTC)
	every := func(step, n int) []LegV {
		var ls []LegV
		for i := 0; i < n; i++ {
			ls = append(ls, legAt(now.Add(-time.Duration(1+i*step)*24*time.Hour)))
		}
		return ls
	}
	for _, c := range []struct {
		legs []LegV
		want string
	}{
		{every(1, 29), "daily"},
		{every(2, 15), "4 times a week"},
		{every(7, 5), "weekly"},
		{every(1, 4), ""}, // under a week of record
	} {
		if got := frequency(c.legs, "UTC", 30, now); got != c.want {
			t.Errorf("%d legs: %q, want %q", len(c.legs), got, c.want)
		}
	}
}

func TestAboutWords(t *testing.T) {
	for in, want := range map[string]string{"2026 Boeing 787-9": "a", "1856 balloon": "an", "Airbus A320": "an", "Embraer 175": "an", "aircraft": "an", "Cessna 172": "a", "8 seat": "an"} {
		if got := article(in); got != want {
			t.Errorf("article(%q) = %q", in, got)
		}
	}
	for _, c := range []struct {
		n    int
		typ  string
		want string
	}{{2, "jet", "Twin jet"}, {1, "Piston", "Single piston"}, {4, "turboprop", "4 turboprops"}, {0, "jet", "Jet"}, {2, "", "2"}} {
		if got := engines(c.n, c.typ); got != c.want {
			t.Errorf("engines(%d, %q) = %q", c.n, c.typ, got)
		}
	}
}

func TestZoneAndCoordinates(t *testing.T) {
	winter := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	summer := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		tz   string
		at   time.Time
		want string
	}{
		{"America/Los_Angeles", winter, "PST · UTC−8"},
		{"America/Los_Angeles", summer, "PDT · UTC−7"},
		{"Asia/Kolkata", winter, "IST · UTC+5:30"},
		{"Europe/Istanbul", winter, "UTC+3"},
		{"Europe/London", winter, "GMT"},
	} {
		if got := zoneLabel(c.tz, c.at); got != c.want {
			t.Errorf("zoneLabel(%s) = %q, want %q", c.tz, got, c.want)
		}
	}
	if got := fmtLatLon(47.4479, -122.3103); got != "47.4479° N, 122.3103° W" {
		t.Errorf("Seattle: %q", got)
	}
	if got := fmtLatLon(-33.9461, 151.1772); got != "33.9461° S, 151.1772° E" {
		t.Errorf("Sydney: %q", got)
	}
}
