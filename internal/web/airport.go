package web

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mikedamm/bermuda/internal/api"
	"github.com/mikedamm/bermuda/internal/geo"
	"github.com/mikedamm/bermuda/internal/store"
)

type AirportPageV struct {
	Airport    geo.Airport
	Code       string
	Region     string
	Country    string
	Clock      ClockV
	Map        *MapV
	Boards     bool         // the arrivals and departures boards answered
	Arrivals   []BoardRowV  // landed here lately, then inbound: earliest first
	Departures []BoardRowV  // left here lately, then scheduled: earliest first
	Live       bool         // the sweep of aircraft around the field answered
	Parked     []TrafficRow // on the ground at the field
	ParkedN    int
	Overhead   []TrafficRow // passing within airportRadius, not landing or leaving here
	Airlines   []AirlineCountV
	Nearby     []NearbyV
	Summary    []string
	Facts      []Fact
	Wikipedia  string
}

// BoardRowV is one line of an arrivals or departures board.
type BoardRowV struct {
	LegV
	Place  AirportV // where it comes from (arrivals) or goes to (departures)
	At     TimeCell // what happened, else what is expected, else the schedule
	Status string
	Class  string // the status tag's
	Note   string // how late or early, when known
	Past   bool   // it has landed or left; the first row that has not starts the "now" line
}

// ClockV is the band's split-flap clock: the airport's local time as four
// digit flaps, HH:MM (01:46), and an AM/PM flap.
type ClockV struct {
	Digits [4]string
	Period string
	Zone   string // PST
	TZ     string // IANA, for the script that keeps it turning
	Label  string // 12:26 AM PST, for screen readers
}

func clockAt(t time.Time, tz string) ClockV {
	if tz == "" {
		tz = "UTC"
	}
	lt := t.In(loadZone(tz))
	h, m := lt.Format("03"), lt.Format("04")
	c := ClockV{Digits: [4]string{h[:1], h[1:], m[:1], m[1:]}, Period: lt.Format("PM"), Zone: timeCell(t, tz, "").Zone, TZ: tz}
	c.Label = lt.Format("3:04 PM") + " " + c.Zone
	return c
}

type TrafficRow struct {
	Callsign, Number, URL string
	Reg, RegURL           string
	From, To              AirportV // a passing flight's route, when found
	Alt                   string
	Dist                  string
	State                 string // parked: Parked, Just landed, Departing
	StateClass            string
	Type                  string // parked: aircraft type from the index
	Airline               string
	Op                    string // operator ICAO code, for its logo
	distMi                float64
	pos                   geo.LL // live position, when resolved
	hdg                   float64
}

type AirlineCountV struct {
	ICAO, Name, URL string
	Count           int
}

type NearbyV struct {
	Code, Name, URL, Dist string
}

const (
	airportRadius = 25 // miles: the sweep for what is parked at the field and what passes over it
	fieldMi       = 3  // on the ground within this of the reference point is on the field
)

func (s *Server) airportPage(ctx context.Context, r *http.Request) (resp, error) {
	raw := r.PathValue("code")
	a, ok := geo.LookupAirport(raw)
	if !ok {
		return s.notFound("We don't have an airport with the code "+strings.ToUpper(raw)+".", raw), nil
	}
	if a.ICAO != raw {
		return redirect("/airport/" + a.ICAO), nil
	}
	now := time.Now()
	v := AirportPageV{Airport: a, Code: a.Code(), Country: geo.CountryName(a.Country), Wikipedia: a.Wiki, Clock: clockAt(now, a.TZ)}
	if rn := geo.RegionName(a.Region); rn != "" {
		v.Region = rn
	}

	// the boards and the sweep around the field load together
	boards := make(chan *api.AirportBoards, 1)
	go func() {
		b, err := s.api.AirportBoards(ctx, a.ICAO)
		if err != nil {
			b = nil // the page stands without its boards
		}
		boards <- b
	}()
	t, err := s.api.AirportTraffic(ctx, a.ICAO, airportRadius)
	switch {
	case errors.Is(err, api.ErrNotFound):
		// the API does not know this field; the page still has its reference data
	case err != nil:
		return resp{}, err
	default:
		v.Live = true
		s.classifyTraffic(ctx, &v, t)
		s.index.NoteSweep(a.ICAO, t, now)
	}

	var planes []mapPlane
	if b := <-boards; b != nil {
		v.Boards = true
		v.Arrivals = boardRows(b.Boards.Arrivals, b.Boards.Enroute, true, now)
		v.Departures = boardRows(b.Boards.Departures, b.Boards.Scheduled, false, now)
		// the boards' flights in the air near the field go on the map
		for _, r := range append(append([]BoardRowV(nil), v.Arrivals...), v.Departures...) {
			if r.Airborne && r.HasPos && geo.DistanceMi(a.Lat, a.Lon, r.Pos.Lat, r.Pos.Lon) < 250 {
				planes = append(planes, mapPlane{P: pt(r.Pos), H: math.Round(r.Hdg), L: r.Number, U: r.URL})
			}
		}
	}

	for _, n := range geo.NearbyAirports(a.Lat, a.Lon, 150, 8, a.ICAO) {
		v.Nearby = append(v.Nearby, NearbyV{Code: n.Code(), Name: n.Name, URL: "/airport/" + n.ICAO,
			Dist: fmtMiles(geo.DistanceMi(a.Lat, a.Lon, n.Lat, n.Lon))})
	}

	center := geo.LL{Lat: a.Lat, Lon: a.Lon}
	close := geo.NearbyAirports(a.Lat, a.Lon, 40, 6, a.ICAO)
	var closeV []AirportV
	for _, n := range close {
		closeV = append(closeV, airportV(n.ICAO, nil))
	}
	for _, r := range v.Overhead {
		if r.pos.Valid() {
			planes = append(planes, mapPlane{P: pt(r.pos), H: math.Round(r.hdg), L: r.Number, U: r.URL})
		}
	}
	v.Map = s.placeMapV(center, a.Code(), "/airport/"+a.ICAO, nil, closeV, planes,
		"Map of "+a.Name+" and the airports and traffic around it",
		func() *geo.Map {
			spec := geo.MapSpec{W: 960, H: 360, Center: &center, RadiusMi: 40,
				Pins: []geo.MapPin{{At: center, Label: a.Code(), Primary: true}}}
			for _, n := range close {
				spec.Pins = append(spec.Pins, geo.MapPin{At: geo.LL{Lat: n.Lat, Lon: n.Lon}, Label: n.Code()})
			}
			m := geo.RenderMap(spec)
			return &m
		})

	add := func(label, val string) {
		if val != "" {
			v.Facts = append(v.Facts, Fact{Label: label, Value: val})
		}
	}
	add("Elevation", fmt.Sprintf("%s ft (%s m)", commas(a.ElevFt), commas(int(float64(a.ElevFt)*0.3048))))
	add("Coordinates", fmtLatLon(a.Lat, a.Lon))
	if a.TZ != "" {
		add("Time zone", zoneLabel(a.TZ, now))
	}
	v.Summary = airportSummary(v)

	title := fmt.Sprintf("%s (%s) live arrivals, departures and flight tracker", a.Name, codeSlash(a))
	desc := fmt.Sprintf("Live flights at %s in %s: aircraft arriving, departing and on the ground right now, with airlines, routes and nearby airports.", a.Name, a.Place())
	p := s.basePage(http.StatusOK, title, desc, "/airport/"+a.ICAO)
	p.Section = "airports"
	p.Query = a.Code()
	p.AppURL = s.appLink("airport", a.Code())
	p.Crumbs = []Crumb{{"Home", "/"}, {a.Name, ""}}
	p.JSONLD = append(p.JSONLD, airportLD(a, s.cfg.SiteURL))
	return s.html("airport", p, v)
}

func codeSlash(a geo.Airport) string {
	if a.IATA != "" {
		return a.IATA + "/" + a.ICAO
	}
	return a.ICAO
}

// boardRows turns a board's recent rows (newest first) and its coming ones
// (soonest first) into one list, earliest first, as an airport's screens
// show it: what has landed (or left), then what is on its way. arriving says
// which end of each flight is here.
func boardRows(recent, coming []api.BoardRow, arriving bool, now time.Time) []BoardRowV {
	var out []BoardRowV
	add := func(b api.BoardRow, past bool) {
		var l LegV
		switch {
		case b.Live != nil && !b.Live.LADDRedacted:
			l = legFromLive(*b.Live, refs{}, now)
		case b.Archived != nil && !b.Archived.LADDRedacted:
			l = legFromHistory(*b.Archived, now)
		default:
			return
		}
		r := BoardRowV{LegV: l, Past: past, Status: firstNonEmpty(l.PhaseWord, "Unknown"), Class: l.StatusClass}
		switch {
		case arriving && past:
			r.Place, r.At = l.Dep, firstCell(l.ArrIn, l.ArrOn, l.ArrEst)
		case arriving:
			r.Place, r.At, r.Note = l.Dep, firstCell(l.ArrEst, l.ArrPlanned), l.ArrDelay
		default:
			r.Place = l.Arr
			if l.DivertedTo != nil {
				r.Place = *l.DivertedTo
			}
			if past {
				r.At = firstCell(l.DepOff, l.DepOut, l.DepPlanned)
				if l.Phase == "en_route" {
					r.Status = "Departed" // on a departures board, not "en route"
				}
			} else {
				r.At, r.Note = l.DepPlanned, l.DepDelay
			}
		}
		out = append(out, r)
	}
	for i := len(recent) - 1; i >= 0; i-- {
		add(recent[i], true)
	}
	for _, b := range coming {
		add(b, false)
	}
	return out
}

// classifyTraffic sorts the aircraft around the field into what is parked at
// it and what passes over it, counts the airlines flying here now, and looks
// up where the passing flights come from and go to.
func (s *Server) classifyTraffic(ctx context.Context, v *AirportPageV, t *api.AirportTraffic) {
	airlines := map[string]int{}
	for _, x := range t.Aircraft {
		row := TrafficRow{
			Callsign: x.Callsign, Number: x.Callsign, URL: flightURL(x.Callsign), Op: opCode("", x.Callsign),
			Reg: x.Registration, RegURL: aircraftURL(x.Registration),
			Alt: fmtAlt(x.AltFt, x.OnGround), Dist: fmtMiles(x.DistanceMi), distMi: x.DistanceMi,
		}
		if row.Dist == "" {
			row.Dist = "0 mi"
		}
		live := x.Status == "active" || x.Status == "landed"
		if al, num, ok := store.AirlineCallsign(x.Callsign); ok {
			if live {
				airlines[al]++
			}
			if a, ok := s.index.Airline(al); ok && a.IATA != "" {
				row.Number = a.IATA + strings.TrimLeft(num, "0")
			}
		}
		if f, ok := s.index.Flight(x.Callsign); ok && f.IATA != "" {
			row.Number = f.IATA
		}
		// The feed's on_ground flag lags: an aircraft a couple of hundred
		// feet above the field and next to it has landed or not yet left.
		nearField := x.DistanceMi < fieldMi && x.AltFt-float64(v.Airport.ElevFt) < 250
		onGround := x.OnGround || x.Status == "landed" || nearField
		switch {
		case onGround && x.DistanceMi <= fieldMi:
			if row.RegURL == "" && row.URL == "" {
				continue // nothing to name it by
			}
			switch {
			case live && x.Direction == "departing":
				row.State, row.StateClass = "Departing", "air"
			case live && (x.Status == "landed" || x.Direction == "arriving"):
				row.State, row.StateClass = "Just landed", "landed"
			default:
				row.State, row.StateClass = "Parked", "muted"
				row.URL = "" // its callsign is the last flight's, not a flight under way
			}
			v.Parked = append(v.Parked, row)
		case !onGround && (x.Direction == "transit" || x.Direction == "local"):
			v.Overhead = append(v.Overhead, row)
		}
	}
	for i := range v.Parked {
		if a, ok := s.index.AircraftByReg(v.Parked[i].Reg); ok {
			v.Parked[i].Type = firstNonEmpty(a.Model, a.Type)
			if al, ok := s.index.Airline(a.Airline); ok {
				v.Parked[i].Airline, v.Parked[i].Op = al.Name, a.Airline
			}
		}
	}
	// moving first (leaving, then just in), then parked aircraft the index
	// can name, then by registration
	rank := map[string]int{"Departing": 0, "Just landed": 1, "Parked": 2}
	sort.SliceStable(v.Parked, func(i, j int) bool {
		a, b := v.Parked[i], v.Parked[j]
		if rank[a.State] != rank[b.State] {
			return rank[a.State] < rank[b.State]
		}
		if (a.Type != "") != (b.Type != "") {
			return a.Type != ""
		}
		return a.Reg < b.Reg
	})
	v.ParkedN = len(v.Parked)
	v.Parked = v.Parked[:min(len(v.Parked), 20)]

	sort.SliceStable(v.Overhead, func(i, j int) bool { return v.Overhead[i].distMi < v.Overhead[j].distMi })
	v.Overhead = v.Overhead[:min(len(v.Overhead), 20)]
	var passing []*TrafficRow
	for i := range v.Overhead {
		if v.Overhead[i].Callsign != "" {
			passing = append(passing, &v.Overhead[i])
		}
	}
	s.resolveLive(ctx, passing)

	for icao, n := range airlines {
		name := icao
		url := ""
		if al, ok := s.index.Airline(icao); ok && al.Name != "" {
			name, url = al.Name, "/airline/"+icao
		}
		v.Airlines = append(v.Airlines, AirlineCountV{ICAO: icao, Name: name, URL: url, Count: n})
	}
	sort.Slice(v.Airlines, func(i, j int) bool {
		if v.Airlines[i].Count != v.Airlines[j].Count {
			return v.Airlines[i].Count > v.Airlines[j].Count
		}
		return v.Airlines[i].Name < v.Airlines[j].Name
	})
	if len(v.Airlines) > 12 {
		v.Airlines = v.Airlines[:12]
	}
}

// liveLookupBudget bounds how long a page waits for optional live lookups.
// Lookups still running at the deadline finish in the background and fill
// the caches, so the next render has them.
const liveLookupBudget = 3500 * time.Millisecond

// resolveLive looks up the passing flights in parallel (cached, shared):
// each one's position for the map, its IATA number and its route. Results
// that arrive after the budget are left out of this render.
func (s *Server) resolveLive(ctx context.Context, rows []*TrafficRow) {
	type found struct {
		ok       bool
		number   string
		dep, arr string
		pos      geo.LL
		hdg      float64
	}
	results := make([]found, len(rows))
	var mu sync.Mutex
	closed := false
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i, row := range rows {
		callsign, reg := row.Callsign, row.Reg
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			res, err := s.api.Search(ctx, callsign, 5)
			if err != nil {
				return
			}
			for _, x := range res.Results {
				if x.Live == nil || x.Live.LADDRedacted || (x.Live.Callsign != callsign && x.Registration != reg) {
					continue
				}
				s.index.NoteLive(*x.Live)
				f := found{ok: true, number: x.Live.IATAFlight, dep: x.Live.Dep, arr: x.Live.Arr, hdg: x.Live.Hdg}
				if p := (geo.LL{Lat: x.Live.Lat, Lon: x.Live.Lon}); p.Valid() && !x.Live.OnGround {
					f.pos = p
				}
				mu.Lock()
				if !closed {
					results[i] = f
				}
				mu.Unlock()
				return
			}
		}()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(liveLookupBudget):
	case <-ctx.Done():
	}
	mu.Lock()
	closed = true
	mu.Unlock()
	for i, f := range results {
		if !f.ok {
			continue
		}
		row := rows[i]
		if f.number != "" {
			row.Number = f.number
		}
		if f.pos.Valid() {
			row.pos, row.hdg = f.pos, f.hdg
		}
		if f.dep != "" {
			row.From = airportV(f.dep, nil)
		}
		if f.arr != "" {
			row.To = airportV(f.arr, nil)
		}
	}
}

// airportSummary is the one sentence that opens the about panel. The live
// counts are the dock's and the codes the title band's, so it says only
// what the airport is and whom it serves.
func airportSummary(v AirportPageV) []string {
	a := v.Airport
	kind := map[byte]string{'L': "a major airport", 'M': "an airport", 'S': "a small airport"}[a.Size]
	return []string{fmt.Sprintf("%s is %s serving %s.", a.Name, firstNonEmpty(kind, "an airport"), a.Place())}
}

func airportLD(a geo.Airport, site string) map[string]any {
	ld := map[string]any{
		"@context": "https://schema.org",
		"@type":    "Airport",
		"name":     a.Name,
		"icaoCode": a.ICAO,
		"url":      site + "/airport/" + a.ICAO,
		"geo":      map[string]any{"@type": "GeoCoordinates", "latitude": a.Lat, "longitude": a.Lon},
		"address": map[string]any{"@type": "PostalAddress", "addressLocality": a.City,
			"addressRegion": a.Region, "addressCountry": a.Country},
	}
	if a.IATA != "" {
		ld["iataCode"] = a.IATA
	}
	if a.Wiki != "" {
		ld["sameAs"] = "https://en.wikipedia.org/wiki/" + a.Wiki
	}
	return ld
}
