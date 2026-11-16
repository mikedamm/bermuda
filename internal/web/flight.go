package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mikedamm/bermuda/internal/api"
	"github.com/mikedamm/bermuda/internal/geo"
	"github.com/mikedamm/bermuda/internal/store"
)

type FlightPageV struct {
	Ident       string
	Number      string
	Airline     string
	AirlineURL  string
	AirlineICAO string
	Featured    LegV
	Map         *geo.Map
	History     []LegV
	Days        int
	Summary     []string
	Facts       []Fact
	SameRoute   []FlightLink
	AppURL      string
}

type Fact struct{ Label, Value, URL string }

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
		Featured: featured, Map: legMap(featured, 960, 420), History: legs, Days: flightHistoryDays,
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
	all := append([]LegV{featured}, legs...)
	v.Summary = flightSummary(v, all, now)
	v.Facts = flightFacts(all)
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
func flightFacts(legs []LegV) []Fact {
	var facts []Fact
	types := map[string]int{}
	name := map[string]string{}
	var durs []time.Duration
	var dist float64
	for _, l := range legs {
		if l.Type != "" {
			types[l.Type]++
			if l.Model != "" {
				name[l.Type] = l.Model
			}
		}
		if l.DistMi > 0 {
			dist = l.DistMi
		}
		if l.flightTime > 0 {
			durs = append(durs, l.flightTime)
		}
	}
	if len(durs) > 0 {
		sort.Slice(durs, func(i, j int) bool { return durs[i] < durs[j] })
		facts = append(facts, Fact{Label: "Typical flight time", Value: fmtDuration(durs[len(durs)/2])})
	}
	if dist == 0 && len(legs) > 0 {
		dist = legs[0].DistMi
	}
	if dist > 0 {
		facts = append(facts, Fact{Label: "Distance", Value: fmtMiles(dist) + " (" + fmtKm(dist) + ")"})
	}
	if t, n := topKey(types); t != "" {
		label := t
		if nm := name[t]; nm != "" {
			label = nm
		}
		facts = append(facts, Fact{Label: "Usual aircraft", Value: fmt.Sprintf("%s, %d of %d flights", label, n, len(legs))})
	}
	facts = append(facts, Fact{Label: "Flights tracked", Value: fmt.Sprintf("%d in %d days", len(legs), flightHistoryDays)})
	return facts
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

// flightSummary writes the "about this flight" paragraph from the data.
func flightSummary(v FlightPageV, legs []LegV, now time.Time) []string {
	f := v.Featured
	var out []string
	name := "Flight " + v.Number
	if v.Airline != "" {
		name = v.Airline + " flight " + v.Number
	}
	if f.Dep.Code != "" && f.Arr.Code != "" {
		s := fmt.Sprintf("%s flies from %s (%s) to %s (%s)", name, f.Dep.Name, f.Dep.Code, f.Arr.Name, f.Arr.Code)
		if f.DistMi > 0 {
			s += fmt.Sprintf(", a distance of %s", fmtMiles(f.DistMi))
		}
		out = append(out, s+".")
	}
	routes := map[string]int{}
	for _, l := range legs {
		if l.Dep.Code != "" && l.Arr.Code != "" {
			routes[l.Dep.Code+"–"+l.Arr.Code]++
		}
	}
	if len(routes) > 1 {
		out = append(out, fmt.Sprintf("Over the last %d days it flew %d different routes under this number.", v.Days, len(routes)))
	}
	switch {
	case f.Live && f.Airborne && f.Alt != "":
		s := fmt.Sprintf("Right now it is in the air at %s", f.Alt)
		if f.LeftMi != "" {
			s += fmt.Sprintf(", %s from %s", f.LeftMi, f.Arr.City)
		}
		if f.ArrEst.Set {
			s += fmt.Sprintf(", and is expected to land at %s %s local time", f.ArrEst.Clock, f.ArrEst.Zone)
		}
		out = append(out, s+".")
	case f.Phase == "arrived" || f.Phase == "landed":
		if f.ArrOn.Set {
			out = append(out, fmt.Sprintf("Its most recent flight landed in %s at %s %s on %s.", f.Arr.City, f.ArrOn.Clock, f.ArrOn.Zone, f.ArrOn.Day))
		}
	}
	return out
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
