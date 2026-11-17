package web

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mikedamm/bermuda/internal/api"
	"github.com/mikedamm/bermuda/internal/store"
)

type FlightPageV struct {
	Ident       string
	Number      string
	Airline     string
	AirlineURL  string
	AirlineICAO string
	Featured    LegV
	Map         *MapV
	History     []LegV
	Days        int
	Summary     []string
	Facts       []Fact
	SameRoute   []FlightLink
	AppURL      string
	Updated     TimeCell // when the live parts were read
}

// Fact is one row of a key-value list; Note is a quieter line under the value.
type Fact struct{ Label, Value, URL, Note string }

type FlightLink struct {
	Number, URL, Route string
}

var identClean = regexp.MustCompile(`[^A-Z0-9]`)
var iataFlight = regexp.MustCompile(`^([A-Z0-9]{2})([0-9]{1,4}[A-Z]?)$`)

const flightHistoryDays = 30

var flightMatchKinds = map[string]bool{"callsign": true, "iata_flight": true, "callsign_history": true}

func canonicalCallsign(res *api.Search) string {
	for _, r := range res.Results {
		if flightMatchKinds[r.Matched.Kind] && r.Matched.Value != "" {
			return strings.ToUpper(r.Matched.Value)
		}
	}
	return ""
}

func (s *Server) flightPage(ctx context.Context, r *http.Request) (resp, error) {
	raw := r.PathValue("ident")
	ident := identClean.ReplaceAllString(strings.ToUpper(raw), "")
	if ident == "" || len(ident) > 10 {
		return s.notFound("That doesn't look like a flight number.", raw), nil
	}
	if ident != raw {
		return redirect("/flight/" + ident), nil
	}
	res, err := s.api.Search(ctx, ident, 50)
	if errors.Is(err, api.ErrBadRequest) || errors.Is(err, api.ErrNotFound) {
		return s.notFound("No flight "+ident+" in the last 30 days.", ident), nil
	}
	if err != nil {
		return resp{}, err
	}
	canon := canonicalCallsign(res)
	// An IATA flight number that is not flying right now: map the airline
	// code through what the index has learned and try the ICAO callsign.
	if canon == "" {
		if m := iataFlight.FindStringSubmatch(ident); m != nil {
			if al, ok := s.index.AirlineByIATA(m[1]); ok {
				alt := al.ICAO + strings.TrimLeft(m[2], "0")
				if res2, err := s.api.Search(ctx, alt, 50); err == nil {
					if c := canonicalCallsign(res2); c != "" {
						res, canon = res2, c
					}
				}
			}
		}
	}
	if canon == "" {
		for _, x := range res.Results {
			if (x.Matched.Kind == "registration" || x.Matched.Kind == "hex") && x.Registration != "" {
				return redirect("/aircraft/" + x.Registration), nil
			}
		}
		return s.notFound("We haven't tracked a flight "+ident+" in the last 30 days.", ident), nil
	}
	if canon != ident {
		return redirect("/flight/" + canon), nil
	}

	now := time.Now()
	var live *api.FlightSummary
	airframes := []string{}
	seen := map[string]bool{}
	for _, x := range res.Results {
		if !flightMatchKinds[x.Matched.Kind] {
			continue
		}
		if x.Live != nil && x.Live.Callsign == canon && !x.Live.LADDRedacted && live == nil {
			l := *x.Live
			live = &l
		}
		if x.AirframeID != "" && !seen[x.AirframeID] && !x.LADDRedacted && len(airframes) < 24 {
			seen[x.AirframeID] = true
			airframes = append(airframes, x.AirframeID)
		}
	}

	// ICAO type designators (B39M) read better as names (Boeing 737-9)
	typeNames := map[string]string{}
	for _, x := range res.Results {
		if x.ICAOType != "" && x.Name != "" {
			typeNames[x.ICAOType] = x.Name
		}
	}
	// the live leg's real path (trail and filed route) loads alongside the history
	shape := make(chan routeShape, 1)
	if live != nil {
		go func() { shape <- s.legShape(ctx, live.FlightID) }()
	}
	history := s.callsignHistory(ctx, canon, airframes)
	rf := refs{}
	rf.addHistory(history)
	s.index.NoteHistory(history)
	if live != nil {
		s.index.NoteLive(*live)
	}

	var featured LegV
	var legs []LegV
	for _, h := range history {
		if live != nil && h.FlightID == live.FlightID {
			continue
		}
		legs = append(legs, legFromHistory(h, now))
	}
	sort.Slice(legs, func(i, j int) bool { return legs[i].sort.After(legs[j].sort) })
	switch {
	case live != nil:
		featured = legFromLive(*live, rf, now)
	case len(legs) > 0:
		featured, legs = legs[0], legs[1:]
	default:
		return s.notFound("We haven't tracked a flight "+ident+" in the last 30 days.", ident), nil
	}
	if len(legs) > 60 {
		legs = legs[:60]
	}
	for _, l := range append([]*LegV{&featured}, ptrs(legs)...) {
		if l.Model == "" {
			l.Model = typeNames[l.Type]
		}
	}

	v := FlightPageV{
		Ident: canon, Number: featured.Number, Airline: featured.Airline, AirlineURL: featured.AirlineURL,
		Featured: featured, History: legs, Days: flightHistoryDays, Updated: timeCell(now, "UTC", ""),
	}
	if al, _, ok := store.AirlineCallsign(canon); ok {
		v.AirlineICAO = al
		if v.Airline == "" {
			if a, ok := s.index.Airline(al); ok {
				v.Airline, v.AirlineURL = a.Name, "/airline/"+al
			}
		}
	}
	if live != nil {
		v.AppURL = s.appLink("flight", live.FlightID)
	}
	if live == nil {
		go func() { shape <- s.legShape(ctx, featured.FlightID) }()
	}
	v.Map = s.legMapV(featured, <-shape, fmt.Sprintf("Map of %s from %s to %s", v.Number, featured.Dep.City, featured.Arr.City))
	all := append([]LegV{featured}, legs...)
	v.Summary = flightSummary(v, all, now)
	v.Facts = flightFacts(featured, all, v.Days, now)
	if featured.Dep.ICAO != "" && featured.Arr.ICAO != "" {
		for _, f := range s.index.FlightsOnRoute(featured.Dep.ICAO, featured.Arr.ICAO, now.Add(-7*24*time.Hour), 13) {
			if f.Ident == canon || flightURL(f.Ident) == "" {
				continue
			}
			v.SameRoute = append(v.SameRoute, FlightLink{Number: legNumber(f.IATA, f.Ident), URL: "/flight/" + f.Ident})
		}
	}

	title := flightTitle(v)
	desc := flightDescription(v)
	p := s.basePage(http.StatusOK, title, desc, "/flight/"+canon)
	p.Section = "flights"
	p.Query = v.Number
	if v.AppURL != "" {
		p.AppURL, p.AppLabel = v.AppURL, "Follow this flight live"
	}
	if live != nil && featured.Phase != "arrived" && featured.Phase != "cancelled" {
		p.Live = s.liveURL(live.FlightID, now)
	}
	p.Crumbs = []Crumb{{"Home", "/"}}
	if v.AirlineURL != "" {
		p.Crumbs = append(p.Crumbs, Crumb{v.Airline, v.AirlineURL})
	}
	p.Crumbs = append(p.Crumbs, Crumb{v.Number, ""})
	p.JSONLD = append(p.JSONLD, flightLD(v, s.cfg.SiteURL))

	maxAge := 60 * time.Second
	if live == nil {
		maxAge = 5 * time.Minute
	}
	return s.html("flight", p, v, maxAge)
}

// callsignHistory pages through each airframe that flew the callsign and
// keeps that callsign's legs. Airframe lists are cached and shared with
// every other flight number those airframes fly.
func (s *Server) callsignHistory(ctx context.Context, callsign string, airframes []string) []api.HistoryEntry {
	var mu sync.Mutex
	var out []api.HistoryEntry
	var wg sync.WaitGroup
	for _, id := range airframes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fp, err := s.api.AircraftFlights(ctx, id, flightHistoryDays, 200)
			if err != nil {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			for _, h := range fp.Flights {
				if h.Callsign == callsign && !h.LADDRedacted {
					out = append(out, h)
				}
			}
		}()
	}
	wg.Wait()
	seen := map[string]bool{}
	dedup := out[:0]
	for _, h := range out {
		if !seen[h.FlightID] {
			seen[h.FlightID] = true
			dedup = append(dedup, h)
		}
	}
	return dedup
}

func flightTitle(v FlightPageV) string {
	f := v.Featured
	who := v.Number
	if v.Airline != "" {
		who = v.Number + " " + v.Airline
	}
	if f.Dep.Code != "" && f.Arr.Code != "" {
		return fmt.Sprintf("%s flight tracker: %s to %s", who, f.Dep.City, f.Arr.City)
	}
	return who + " flight tracker and history"
}

func flightDescription(v FlightPageV) string {
	f := v.Featured
	var b strings.Builder
	b.WriteString("Track ")
	if v.Airline != "" {
		b.WriteString(v.Airline + " ")
	}
	b.WriteString("flight " + v.Number)
	if v.Number != v.Ident {
		b.WriteString(" (" + v.Ident + ")")
	}
	if f.Dep.Code != "" && f.Arr.Code != "" {
		fmt.Fprintf(&b, " from %s (%s) to %s (%s)", f.Dep.City, f.Dep.Code, f.Arr.City, f.Arr.Code)
	}
	b.WriteString(": live status, departure and arrival times, the aircraft flying it, and 30 days of flight history.")
	return b.String()
}

// flightFacts summarises the history: usual aircraft, typical time aloft.
// flightFacts are what a traveller asks of a flight number, from the legs
// that flew its featured route in the last days: when it usually leaves,
// how long it takes, how far it goes and what flies it. Live status is the
// dock's, so none of it is repeated here.
func flightFacts(f LegV, legs []LegV, days int, now time.Time) []Fact {
	var facts []Fact
	on := routeLegs(f, legs)
	if t := usualTakeoff(on, f.Dep.TZ, f.Dep.Country, now); t != "" {
		facts = append(facts, Fact{Label: "Usual take-off", Value: t})
	}
	var durs []time.Duration
	types, name, typed := map[string]int{}, map[string]string{}, 0
	dist := f.DistMi
	for _, l := range on {
		if l.flightTime > 0 {
			durs = append(durs, l.flightTime)
		}
		if l.Type != "" {
			types[l.Type]++
			typed++
			if l.Model != "" {
				name[l.Type] = l.Model
			}
		}
		if dist == 0 {
			dist = l.DistMi
		}
	}
	if len(durs) > 0 {
		sort.Slice(durs, func(i, j int) bool { return durs[i] < durs[j] })
		facts = append(facts, Fact{Label: "Typical flight time", Value: fmtDuration(durs[len(durs)/2])})
	}
	if dist > 0 {
		facts = append(facts, Fact{Label: "Distance", Value: fmtMiles(dist) + " (" + fmtKm(dist) + ")"})
	}
	if t, n := topKey(types); t != "" {
		fact := Fact{Label: "Usual aircraft", Value: firstNonEmpty(name[t], t)}
		switch {
		case typed > 1 && n == typed:
			fact.Note = fmt.Sprintf("on all %d flights", typed)
		case typed > 1:
			fact.Note = fmt.Sprintf("on %d of %d flights", n, typed)
		}
		facts = append(facts, fact)
	}
	routes := map[string]bool{}
	for _, l := range legs {
		if l.Dep.Code != "" && l.Arr.Code != "" {
			routes[l.Dep.Code+"-"+l.Arr.Code] = true
		}
	}
	if len(routes) > 1 {
		facts = append(facts, Fact{Label: "Routes flown", Value: fmt.Sprint(len(routes)), Note: fmt.Sprintf("in the last %d days", days)})
	}
	return facts
}

// routeLegs are the legs that flew the featured leg's route; all of them
// when it has none.
func routeLegs(f LegV, legs []LegV) []LegV {
	if f.Dep.Code == "" || f.Arr.Code == "" {
		return legs
	}
	var out []LegV
	for _, l := range legs {
		if l.Dep.Code == f.Dep.Code && l.Arr.Code == f.Arr.Code {
			out = append(out, l)
		}
	}
	return out
}

// usualTakeoff is the local time a route usually leaves, when it keeps to
// one: the circular mean of its departures (so 11:50 PM and 12:10 AM
// average to midnight), to five minutes. Empty with fewer than three, or
// when they scatter by more than about 40 minutes.
func usualTakeoff(legs []LegV, tz, country string, now time.Time) string {
	loc := loadZone(tz)
	var sx, sy float64
	n := 0
	for _, l := range legs {
		if l.departed.IsZero() {
			continue
		}
		t := l.departed.In(loc)
		a := float64(t.Hour()*60+t.Minute()) / 1440 * 2 * math.Pi
		sx, sy, n = sx+math.Cos(a), sy+math.Sin(a), n+1
	}
	if n < 3 || math.Hypot(sx, sy)/float64(n) < 0.985 {
		return ""
	}
	m := math.Atan2(sy, sx) / (2 * math.Pi) * 1440
	if m < 0 {
		m += 1440
	}
	mins := int(math.Round(m/5)) * 5 % 1440
	y, mo, d := now.In(loc).Date()
	c := timeCell(time.Date(y, mo, d, mins/60, mins%60, 0, 0, loc), tz, country)
	return c.Clock + " " + c.Zone
}

// frequency says how often a route flies, from the local days it left in
// the window: "daily", "5 times a week", "weekly", or empty when the record
// is under a week long or too thin to say.
func frequency(legs []LegV, tz string, days int, now time.Time) string {
	seen := map[string]bool{}
	first := now
	for _, l := range legs {
		if l.departed.IsZero() || now.Sub(l.departed) > time.Duration(days)*24*time.Hour {
			continue
		}
		seen[l.departed.In(loadZone(tz)).Format("2006-01-02")] = true
		if l.departed.Before(first) {
			first = l.departed
		}
	}
	span := math.Ceil(now.Sub(first).Hours() / 24)
	if span < 7 || len(seen) < 2 {
		return ""
	}
	perWeek := float64(len(seen)) * 7 / span
	switch k := int(math.Round(perWeek)); {
	case perWeek >= 6.5:
		return "daily"
	case k >= 2:
		return fmt.Sprintf("%d times a week", k)
	case k == 1:
		return "weekly"
	}
	return ""
}

func ptrs(ls []LegV) []*LegV {
	out := make([]*LegV, len(ls))
	for i := range ls {
		out[i] = &ls[i]
	}
	return out
}

func topKey(m map[string]int) (string, int) {
	best, n := "", 0
	for k, v := range m {
		if v > n || (v == n && k < best) {
			best, n = k, v
		}
	}
	return best, n
}

// flightSummary is the one sentence that opens the about panel: who flies
// it, how often, and between which cities.
func flightSummary(v FlightPageV, legs []LegV, now time.Time) []string {
	f := v.Featured
	if f.Dep.Code == "" || f.Arr.Code == "" {
		return nil
	}
	name := "Flight " + v.Number
	if v.Airline != "" {
		name = v.Airline + " flight " + v.Number
	}
	s := name + " flies"
	if fq := frequency(routeLegs(f, legs), f.Dep.TZ, v.Days, now); fq != "" {
		s += " " + fq
	}
	place := func(a AirportV) string { return firstNonEmpty(a.City, a.Name) }
	return []string{fmt.Sprintf("%s from %s (%s) to %s (%s).", s, place(f.Dep), f.Dep.Code, place(f.Arr), f.Arr.Code)}
}

func flightLD(v FlightPageV, site string) map[string]any {
	f := v.Featured
	ld := map[string]any{
		"@context":     "https://schema.org",
		"@type":        "Flight",
		"name":         strings.TrimSpace(v.Airline + " flight " + v.Number),
		"flightNumber": v.Number,
		"url":          site + "/flight/" + v.Ident,
	}
	if v.Airline != "" {
		prov := map[string]any{"@type": "Airline", "name": v.Airline}
		if len(v.Number) > 2 && v.Number != v.Ident {
			prov["iataCode"] = v.Number[:2]
		}
		ld["provider"] = prov
	}
	ap := func(a AirportV) map[string]any {
		m := map[string]any{"@type": "Airport", "name": a.Name}
		if a.Code != a.ICAO {
			m["iataCode"] = a.Code
		}
		if a.ICAO != "" {
			m["icaoCode"] = a.ICAO
		}
		return m
	}
	if f.Dep.Code != "" {
		ld["departureAirport"] = ap(f.Dep)
	}
	if f.Arr.Code != "" {
		ld["arrivalAirport"] = ap(f.Arr)
	}
	if t := firstCell(f.DepOff, f.DepOut, f.DepPlanned); t.Set {
		ld["departureTime"] = t.ISO
	}
	if t := firstCell(f.ArrOn, f.ArrIn, f.ArrEst, f.ArrPlanned); t.Set {
		ld["arrivalTime"] = t.ISO
	}
	if f.DistMi > 0 {
		ld["flightDistance"] = fmtMiles(f.DistMi)
	}
	if f.Type != "" {
		ld["aircraft"] = f.Type
	}
	return ld
}

func firstCell(cs ...TimeCell) TimeCell {
	for _, c := range cs {
		if c.Set {
			return c
		}
	}
	return TimeCell{}
}
