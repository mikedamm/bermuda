package web

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

// TimeCell is a moment shown in an airport's local time.
type TimeCell struct {
	Clock string // "8:07 PM" or "20:07"
	Day   string // "Sun 15 Nov"
	Zone  string // "EST"
	ISO   string // RFC 3339 for <time datetime>
	Set   bool
}

var zones sync.Map

func loadZone(name string) *time.Location {
	if name == "" {
		return time.UTC
	}
	if l, ok := zones.Load(name); ok {
		return l.(*time.Location)
	}
	l, err := time.LoadLocation(name)
	if err != nil {
		l = time.UTC
	}
	zones.Store(name, l)
	return l
}

func timeCell(t time.Time, tz, country string) TimeCell {
	if t.IsZero() {
		return TimeCell{}
	}
	lt := t.In(loadZone(tz))
	// One clock format per page, as FlightAware does: 12-hour with the
	// local zone, whatever the airport's country.
	clock := lt.Format("3:04 PM")
	zone := lt.Format("MST")
	if strings.HasPrefix(zone, "+") || strings.HasPrefix(zone, "-") {
		zone = "UTC" + strings.TrimSuffix(strings.TrimSuffix(zone, "00"), "0")
	}
	return TimeCell{Clock: clock, Day: lt.Format("Mon 2 Jan"), Zone: zone, ISO: t.UTC().Format(time.RFC3339), Set: true}
}

func dateLabel(t time.Time, tz string) string {
	if t.IsZero() {
		return ""
	}
	return t.In(loadZone(tz)).Format("Mon 2 Jan")
}

func fmtDuration(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	m := int(math.Round(d.Minutes()))
	if m < 60 {
		return strconv.Itoa(m) + "m"
	}
	return fmt.Sprintf("%dh %02dm", m/60, m%60)
}

// fmtDelay compares an actual or estimated time with the schedule.
func fmtDelay(actual, sched time.Time) (text, class string) {
	if actual.IsZero() || sched.IsZero() {
		return "", ""
	}
	d := actual.Sub(sched)
	m := int(math.Round(d.Minutes()))
	switch {
	case m >= 15:
		return fmtDuration(d) + " late", "late"
	case m > 0:
		return strconv.Itoa(m) + "m late", "ontime"
	case m <= -5:
		return fmtDuration(-d) + " early", "early"
	}
	return "On time", "ontime"
}

func relTime(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := now.Sub(t)
	future := d < 0
	if future {
		d = -d
	}
	var s string
	switch {
	case d < time.Minute:
		if future {
			return "in under a minute"
		}
		return "just now"
	case d < time.Hour:
		s = strconv.Itoa(int(d.Minutes())) + " min"
	case d < 48*time.Hour:
		h := int(d.Hours())
		m := int(d.Minutes()) % 60
		if m == 0 || h >= 10 {
			s = strconv.Itoa(h) + "h"
		} else {
			s = fmt.Sprintf("%dh %dm", h, m)
		}
	default:
		s = strconv.Itoa(int(d.Hours()/24)) + " days"
	}
	if future {
		return "in " + s
	}
	return s + " ago"
}

func commas(n int) string {
	s := strconv.Itoa(n)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

func fmtMiles(mi float64) string {
	if mi <= 0 {
		return ""
	}
	return commas(int(math.Round(mi))) + " mi"
}

func fmtKm(mi float64) string { return commas(int(math.Round(mi*1.609344))) + " km" }

func fmtAlt(ft float64, onGround bool) string {
	if onGround {
		return "On ground"
	}
	if ft <= 0 {
		return ""
	}
	return commas(int(math.Round(ft/100)*100)) + " ft"
}

func fmtSpeed(kt float64) string {
	if kt <= 0 {
		return ""
	}
	return fmt.Sprintf("%s kt (%s mph)", commas(int(math.Round(kt))), commas(int(math.Round(kt*1.15078))))
}

func compass(hdg float64) string {
	dirs := []string{"north", "northeast", "east", "southeast", "south", "southwest", "west", "northwest"}
	return dirs[int(math.Round(math.Mod(hdg+360, 360)/45))%8]
}

// smallWords stay lower case inside a title-cased name.
var smallWords = map[string]bool{"of": true, "and": true, "the": true, "de": true, "del": true, "la": true, "du": true}

// keepUpper stays upper case (legal suffixes, codes, initialisms).
var keepUpper = map[string]bool{"LLC": true, "INC": false, "LP": true, "LLP": true, "USA": true, "US": true, "II": true, "III": true,
	"IV": true, "FBO": true, "NA": true, "UK": true, "UPS": true, "DHL": true, "FAA": true, "NASA": true, "TSA": true, "JFK": true, "PLC": true, "AG": true, "SA": true}

// titleCase turns register text (ALASKA AIRLINES INC) into Alaska Airlines Inc.
// Text already in mixed case is left alone.
func titleCase(s string) string {
	if s == "" || s != strings.ToUpper(s) {
		return s
	}
	words := strings.Fields(strings.ToLower(s))
	for i, w := range words {
		up := strings.ToUpper(w)
		if keepUpper[up] {
			words[i] = up
			continue
		}
		if i > 0 && smallWords[w] {
			continue
		}
		words[i] = capWord(w)
	}
	return strings.Join(words, " ")
}

// capWord capitalises after hyphens and apostrophes too: Seattle-Tacoma, O'Hare.
func capWord(w string) string {
	rs := []rune(w)
	up := true
	for i, r := range rs {
		if up && unicode.IsLetter(r) {
			rs[i] = unicode.ToUpper(r)
			up = false
		}
		if r == '-' || r == '/' || r == '(' || (r == '\'' && i == 1) {
			up = true
		}
	}
	return string(rs)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return commas(n) + " " + many
}

// fmtDate turns a register date (2009-05-26) into 26 May 2009.
func fmtDate(s string) string {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return s
	}
	return t.Format("2 Jan 2006")
}
