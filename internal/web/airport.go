package web

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"slices"
	"sort"
	"strconv"
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
	Overhead   []TrafficRow     // passing within airportRadius, not landing or leaving here
	Routes     []AirlineRoutesV // the airlines here and the places they fly to and from, most places first
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
	Class  string    // the status tag's
	Note   string    // how late or early it is, when the status does not say
	City   string    // its place's city as a board names it ("Honolulu", not "Honolulu, Oahu")
	Twin   bool      // another airport of that city is on the board too, so it shows the code
	Gate   string    // the gate it arrives at or leaves from, when known
	Claim  string    // arrivals: the carousel its bags come out on, when known
	Past   bool      // it has landed or left; the first row that has not starts the "now" line
	Fill   int       // how much of its row fills (%): its progress in the air (1-97), 98 landed, 100 at the gate; 0 none
	Rate   float64   // in the air, how fast the fill grows (% a second), for the page to keep it filling
	Air    bool      // the fill is a flight in the air's progress
	t      time.Time // the time it shows, which the board sorts by
}

// RowClass is the board row's classes: is-past for what has landed or left,
// is-filled for a row that fills with the flight's progress.
func (r BoardRowV) RowClass() string {
	var c []string
	if r.Past {
		c = append(c, "is-past")
	}
	if r.Fill > 0 {
		c = append(c, "is-filled")
	}
	return strings.Join(c, " ")
}

// boardsAnswer is what the two boards are drawn from: the airport page's
// inbound and scheduled views for what is coming, and the full arrivals and
// departures boards for what has landed and left (nil when they failed).
type boardsAnswer struct {
	page     *api.AirportBoards
	arr, dep *api.Board
}

// Flown phases: what a board's top half lists. The full boards hold every
// flight of any phase, planned ones ahead of flown ones, and so does the
// page's ten-row preview of them, which is why it is not used.
var (
	landedPhases = []string{"landed", "arrived"}
	leftPhases   = []string{"taxiing", "en_route", "landed", "arrived", "diverted"}
)

const (
	// how deep the full boards are read for ten flown rows: flights in the
	// air without an estimate and plans past their time sort among the
	// landed ones, a few hundred of them at a hub before dawn
	arrivalsDepth   = 500
	departuresDepth = 60
	flownRows       = 10
)

// loadBoards fetches the page's boards and the full arrivals and departures
// boards together, from now back. Only the page's answer is required.
func (s *Server) loadBoards(ctx context.Context, icao string, now time.Time) (boardsAnswer, error) {
	var out boardsAnswer
	var wg sync.WaitGroup
	for _, side := range []struct {
		kind  string
		depth int
		dst   **api.Board
	}{{"arrivals", arrivalsDepth, &out.arr}, {"departures", departuresDepth, &out.dep}} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if b, err := s.api.AirportBoard(ctx, icao, side.kind, 6, side.depth, now); err == nil {
				*side.dst = b
			}
		}()
	}
	var err error
	out.page, err = s.api.AirportBoards(ctx, icao)
	wg.Wait()
	s.noteRoutes(icao, out, now)
	return out, err
}

// noteRoutes has the index learn the airlines and places on the boards.
func (s *Server) noteRoutes(icao string, b boardsAnswer, now time.Time) {
	if p := b.page; p != nil {
		s.index.NoteBoard(icao, p.Boards.Arrivals, true, now)
		s.index.NoteBoard(icao, p.Boards.Enroute, true, now)
		s.index.NoteBoard(icao, p.Boards.Departures, false, now)
		s.index.NoteBoard(icao, p.Boards.Scheduled, false, now)
	}
	if b.arr != nil {
		s.index.NoteBoard(icao, b.arr.Rows, true, now)
	}
	if b.dep != nil {
		s.index.NoteBoard(icao, b.dep.Rows, false, now)
	}
}

// flown is a full board's first rows in one of the phases, newest first.
func flown(b *api.Board, phases []string) []api.BoardRow {
	if b == nil {
		return nil
	}
	var out []api.BoardRow
	for _, r := range b.Rows {
		if slices.Contains(phases, r.Phase()) {
			out = append(out, r)
			if len(out) == flownRows {
				break
			}
		}
	}
	return out
}

// setBoards fills the arrivals and departures boards.
func (v *AirportPageV) setBoards(b boardsAnswer, now time.Time) {
	v.Boards = true
	v.Arrivals = boardRows(flown(b.arr, landedPhases), b.page.Boards.Enroute, true, v.Airport.TZ, now)
	v.Departures = boardRows(flown(b.dep, leftPhases), b.page.Boards.Scheduled, false, v.Airport.TZ, now)
}

// boardMins writes a number of minutes as a board does, in its largest unit
// alone: 4m, 59m, then to the nearest hour, 1h (to 89m), 2h, 3h.
func boardMins(m int) string {
	if m < 60 {
		return strconv.Itoa(m) + "m"
	}
	return strconv.Itoa((m+30)/60) + "h"
}

// etaStatus is an inbound flight's status in one short phrase, its estimate
// against its schedule: "On time", "4m late", "12m early". As fmtDelay has
// it, up to five minutes early is on time. class is the tag's: ok, late
// (15 minutes or more), or air for a few minutes behind.
func etaStatus(est, sched time.Time) (text, class string) {
	if est.IsZero() || sched.IsZero() {
		return "", ""
	}
	m := int(math.Round(est.Sub(sched).Minutes()))
	switch {
	case m >= 15:
		return boardMins(m) + " late", "late"
	case m > 0:
		return boardMins(m) + " late", "air"
	case m <= -5:
		return boardMins(-m) + " early", "ok"
	}
	return "On time", "ok"
}

// lateWords is a board note under a status, how far off its schedule a
// time is ("12m late", "8m early"), or nothing when on time.
func lateWords(at, sched time.Time) string {
	if at.IsZero() || sched.IsZero() {
		return ""
	}
	switch m := int(math.Round(at.Sub(sched).Minutes())); {
	case m > 0:
		return boardMins(m) + " late"
	case m <= -5:
		return boardMins(-m) + " early"
	}
	return ""
}

// boardFill is how much of a flight's row fills: all of it once it is at
// the gate (arrived), 98% once it is down and taxiing in (landed), and in
// the air how far along its route it is, by its position where the leg has
// one, else by the clock, take-off to the estimate. In the air it stays
// between 1 and 97, so it never reads as not yet left or already down.
// rate is how fast it fills in the air (% a second, by the same measure:
// its ground speed over the route, or the clock over the planned flight),
// so a page can keep it filling between updates; 0 when it is not moving.
func boardFill(l LegV, fs *api.FlightSummary, now time.Time) (fill int, rate float64) {
	switch {
	case l.Phase == "arrived":
		return 100, 0
	case l.Phase == "landed":
		return 98, 0
	case l.Phase != "en_route" || (fs != nil && fs.OnGround):
		return 0, 0
	}
	p, rate := float64(l.Progress), l.ProgressRate
	if p <= 0 && fs != nil {
		off, est := firstSet(fs.Off, fs.Out), firstSet(fs.ETA, fs.STA, fs.SchedArr)
		if off.IsZero() || !est.After(off) {
			return 0, 0
		}
		p = float64(now.Sub(off)) / float64(est.Sub(off)) * 100
		rate = 100 / est.Sub(off).Seconds()
	}
	if p >= 97 {
		rate = 0 // as far as a flight in the air fills
	}
	return int(math.Max(1, math.Min(97, math.Round(p)))), rate
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
	Op                    string  // operator ICAO code, for its logo
	Fill                  int     // passing: how far along its route it is (%), as the boards fill; 0 unknown
	Rate                  float64 // passing: how fast Fill grows (% a second), as the boards
	distMi                float64
	pos                   geo.LL // live position, when resolved
	hdg                   float64
	flightID              string // the live flight found for it, for its trail on the map
}

type AirlineCountV struct {
	ICAO, Name, URL string
	Count           int
}

// AirlineRoutesV is an airline at the airport and the places its flights
// go to or come from, as the boards name them.
type AirlineRoutesV struct {
	ICAO, Name, URL string
	Places          []RoutePlaceV // by city
}

// RoutePlaceV is one of an airline's places, drawn as a board draws its
// from or to (the "board-place" template).
type RoutePlaceV struct {
	Place AirportV
	City  string // as boards name it ("Honolulu", not "Honolulu, Oahu")
	Twin  bool   // another of the airline's places is in the city too, so it shows the code
}

// routesWindow is how far back the airlines and destinations reach.
const routesWindow = store.RouteDays * 24 * time.Hour

// airlineRoutes is the airlines the airport's boards have listed lately and
// their places.
func (s *Server) airlineRoutes(icao string, now time.Time) []AirlineRoutesV {
	var out []AirlineRoutesV
	for _, r := range s.index.RoutesAt(icao, now.Add(-routesWindow)) {
		v := AirlineRoutesV{ICAO: r.Airline, Name: r.Airline}
		if al, ok := s.index.Airline(r.Airline); ok && al.Name != "" {
			v.Name, v.URL = al.Name, "/airline/"+r.Airline
		}
		cities := map[string]int{}
		for _, code := range r.Others {
			p := RoutePlaceV{Place: airportV(code, nil)}
			p.City = boardCity(p.Place.City)
			cities[p.City]++
			v.Places = append(v.Places, p)
		}
		for i := range v.Places {
			v.Places[i].Twin = cities[v.Places[i].City] > 1
		}
		sort.Slice(v.Places, func(i, j int) bool {
			a, b := v.Places[i], v.Places[j]
			if a.City != b.City {
				return a.City < b.City
			}
			return a.Place.Code < b.Place.Code
		})
		out = append(out, v)
	}
	return out
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
	boards := make(chan *boardsAnswer, 1)
	go func() {
		b, err := s.loadBoards(ctx, a.ICAO, now)
		if err != nil {
			boards <- nil // the page stands without its boards
			return
		}
		boards <- &b
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

	// the map's traffic, coloured by what it is doing here: arriving,
	// departing, or passing over; each flight once, with the flight it is
	// for its trail
	var planes []mapPlane
	var planeIDs []string
	onMap := map[string]bool{}
	addPlane := func(p mapPlane, id string) {
		if p.U != "" && onMap[p.U] {
			return
		}
		onMap[p.U] = true
		planes, planeIDs = append(planes, p), append(planeIDs, id)
	}
	if b := <-boards; b != nil {
		v.setBoards(*b, now)
		// the boards' flights in the air near the field go on the map
		for _, side := range []struct {
			rows []BoardRowV
			kind string
		}{{v.Arrivals, "arr"}, {v.Departures, "dep"}} {
			for _, r := range side.rows {
				if r.Airborne && r.HasPos && geo.DistanceMi(a.Lat, a.Lon, r.Pos.Lat, r.Pos.Lon) < 250 {
					addPlane(mapPlane{P: pt(r.Pos), H: math.Round(r.Hdg), L: r.Number, U: r.URL, K: side.kind}, r.FlightID)
				}
			}
		}
	}

	// after the boards, which teach the index this field's routes
	v.Routes = s.airlineRoutes(a.ICAO, now)

	center := geo.LL{Lat: a.Lat, Lon: a.Lon}
	close := geo.NearbyAirports(a.Lat, a.Lon, 40, 6, a.ICAO)
	var closeV []AirportV
	for _, n := range close {
		closeV = append(closeV, airportV(n.ICAO, nil))
	}
	for _, r := range v.Overhead {
		if r.pos.Valid() {
			addPlane(mapPlane{P: pt(r.pos), H: math.Round(r.hdg), L: r.Number, U: r.URL, K: "pass"}, r.flightID)
		}
	}
	if s.cfg.MapTiles != "" {
		s.addTrails(ctx, planes, planeIDs)
	}
	v.Map = s.placeMapV(center, a.Code(), "/airport/"+a.ICAO, nil, closeV, planes,
		"Map of "+a.Name+" and the airports and traffic around it: arrivals in blue, departures in green, flights passing over in gray",
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
	desc := fmt.Sprintf("Live flights at %s in %s: aircraft arriving, departing and on the ground right now, with the airlines here and where they fly.", a.Name, a.Place())
	p := s.basePage(http.StatusOK, title, desc, "/airport/"+a.ICAO)
	p.Section = "airports"
	p.Query = a.Code()
	p.AppURL = s.appLink("airport", a.Code())
	p.Live = s.liveAirportURL(a.ICAO, now) // live.js keeps the boards current
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
// which end of each flight is here. Each row shows the time the board sorts
// it by (wheels-on or gate-in, wheels-off or gate-out, the estimate, the
// schedule), so the column reads in order and is never blank. A flight the
// data holds twice shows once: the same flight number within half an hour
// on the board, or still listed as coming within three hours of landing or
// leaving.
func boardRows(recent, coming []api.BoardRow, arriving bool, tz string, now time.Time) []BoardRowV {
	var out []BoardRowV
	seen := map[string][]time.Time{}
	fresh := func(id string, t time.Time, within time.Duration) bool {
		for _, k := range seen[id] {
			if d := t.Sub(k); d < within && d > -within {
				return false
			}
		}
		seen[id] = append(seen[id], t)
		return true
	}
	row := func(b api.BoardRow, past bool) (BoardRowV, bool) {
		var l LegV
		switch {
		case b.Live != nil && !b.Live.LADDRedacted:
			l = legFromLive(*b.Live, refs{}, now)
		case b.Archived != nil && !b.Archived.LADDRedacted:
			l = legFromHistory(*b.Archived, now)
		default:
			return BoardRowV{}, false
		}
		r := BoardRowV{LegV: l, Past: past, Status: firstNonEmpty(l.PhaseWord, "Unknown"), Class: l.StatusClass}
		r.Fill, r.Rate = boardFill(l, b.Live, now)
		r.Air = l.Phase == "en_route" && r.Fill > 0
		switch {
		case arriving && past:
			r.Place, r.At = l.Dep, firstCell(l.ArrIn, l.ArrOn, l.ArrEst)
			r.Gate, r.Claim = l.ArrGate, l.Baggage
		case arriving:
			r.Place, r.At = l.Dep, firstCell(l.ArrEst, l.ArrPlanned)
			r.Gate, r.Claim = l.ArrGate, l.Baggage
			// inbound: "En Route" says less than when it gets here, so an
			// airborne flight's status is its estimate against the schedule;
			// one still to leave keeps its phase and notes the estimate
			var eta, class string
			if fs := b.Live; fs != nil {
				eta, class = etaStatus(firstSet(fs.ETA, fs.STA), fs.SchedArr)
			}
			switch {
			case l.Phase == "en_route" && eta != "":
				r.Status, r.Class = eta, class
			case eta != "On time":
				r.Note = eta
			}
		default:
			r.Place, r.Gate = l.Arr, l.DepGate
			if l.DivertedTo != nil {
				r.Place = *l.DivertedTo
			}
			if past {
				r.At = firstCell(l.DepOff, l.DepOut, l.DepPlanned)
				if l.Phase == "en_route" {
					r.Status = "Departed" // on a departures board, not "en route"
				}
			} else {
				r.At = l.DepPlanned
				// in the board's words: off the gate, else the slot it is
				// held to
				fs := b.Live
				switch {
				case fs != nil && !fs.Out.IsZero():
					if l.Phase == "scheduled" {
						r.Status, r.Class = "Left gate", "taxi" // off the gate before its phase moves on
					}
					r.Note = lateWords(fs.Out, fs.SchedDep)
				case fs != nil && !fs.EDCT.IsZero():
					r.Note = lateWords(fs.EDCT, fs.SchedDep)
				}
			}
		}
		r.City = boardCity(r.Place.City)
		if !b.BoardTime.IsZero() {
			r.At = timeCell(b.BoardTime, tz, "")
		}
		r.t, _ = time.Parse(time.RFC3339, r.At.ISO)
		return r, true
	}
	keep := func(r BoardRowV, within time.Duration) {
		if fresh(firstNonEmpty(r.Ident, r.FlightID), firstSet(r.t, now), within) {
			out = append(out, r)
		}
	}
	for i := len(recent) - 1; i >= 0; i-- {
		if r, ok := row(recent[i], true); ok {
			keep(r, 30*time.Minute)
		}
	}
	// The API lists the inbound flights with an estimate or a metered time
	// ahead of those with only a schedule, which it gives no board time, so
	// the coming rows go in the order of the time each one shows (one with
	// none last) before a flight held twice is dropped.
	var next []BoardRowV
	for _, b := range coming {
		if r, ok := row(b, false); ok {
			next = append(next, r)
		}
	}
	sort.SliceStable(next, func(i, j int) bool {
		a, b := next[i].t, next[j].t
		return !a.IsZero() && (b.IsZero() || a.Before(b))
	})
	for _, r := range next {
		keep(r, 3*time.Hour)
	}
	markTwins(out)
	return out
}

// boardCity is a place's city as a board names it, up to its first comma
// ("Honolulu" for "Honolulu, Oahu"), and short of a parenthesis that comma
// would leave open ("Paris" for "Paris (Roissy-en-France, Val-d'Oise)";
// "Shanghai (Pudong)" stays whole).
func boardCity(city string) string {
	c, _, _ := strings.Cut(city, ", ")
	if i := strings.Index(c, " ("); i > 0 && !strings.Contains(c[i:], ")") {
		c = c[:i]
	}
	return c
}

// markTwins marks the rows whose city another airport on the board shares
// (New York's JFK and LaGuardia), which show their code by the city.
func markTwins(rows []BoardRowV) {
	codes := map[string]map[string]bool{}
	for _, r := range rows {
		if r.City != "" {
			if codes[r.City] == nil {
				codes[r.City] = map[string]bool{}
			}
			codes[r.City][r.Place.Code] = true
		}
	}
	for i := range rows {
		rows[i].Twin = len(codes[rows[i].City]) > 1
	}
}

// classifyTraffic sorts the aircraft around the field into what is parked at
// it and what passes over it, and looks up where the passing flights come
// from and go to.
func (s *Server) classifyTraffic(ctx context.Context, v *AirportPageV, t *api.AirportTraffic) {
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
				// a landed flight stays live for hours until it closes, so it
				// has arrived, not "just" landed
				row.State, row.StateClass = "Arrived", "landed"
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
	// leaving first, then arrived, then parked; within each, aircraft the
	// index can name first, then by registration
	rank := map[string]int{"Departing": 0, "Arrived": 1, "Parked": 2}
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
	s.resolveLive(ctx, passing, geo.LL{Lat: v.Airport.Lat, Lon: v.Airport.Lon})
}

// liveLookupBudget bounds how long a page waits for optional live lookups.
// Lookups still running at the deadline finish in the background and fill
// the caches, so the next render has them.
const liveLookupBudget = 3500 * time.Millisecond

// resolveLive looks up the passing flights in parallel (cached, shared):
// each one's position for the map, its IATA number, its route and how far
// along it is. A flight found by its callsign that is nowhere near the field
// is another aircraft's (a stale or reused callsign), not this one's. Results
// that arrive after the budget are left out of this render.
func (s *Server) resolveLive(ctx context.Context, rows []*TrafficRow, field geo.LL) {
	type found struct {
		ok       bool
		id       string
		number   string
		dep, arr string
		pos      geo.LL
		hdg      float64
		fill     int
		rate     float64
	}
	now := time.Now()
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
				if p := (geo.LL{Lat: x.Live.Lat, Lon: x.Live.Lon}); p.Valid() && geo.DistanceMi(field.Lat, field.Lon, p.Lat, p.Lon) > 3*airportRadius {
					continue
				}
				s.index.NoteLive(*x.Live)
				f := found{ok: true, id: x.Live.FlightID, number: x.Live.IATAFlight, dep: x.Live.Dep, arr: x.Live.Arr, hdg: x.Live.Hdg}
				// how far along it is, for a flight in the air between two
				// fields: one back to where it left has no way along to show,
				// and a record that has it down already is a finished leg's
				if x.Live.Phase == "en_route" && f.dep != f.arr {
					f.fill, f.rate = boardFill(legFromLive(*x.Live, refs{}, now), x.Live, now)
				}
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
		row.Fill, row.Rate = f.fill, f.rate
		row.flightID = f.id
	}
}

// trailWindow is how much of its track an aircraft on an airport's map
// draws behind it, a point every trailStep: a short tail that shows the way
// it has come and how it is turning.
const (
	trailWindow = 3 * time.Minute
	trailStep   = 10 * time.Second
	trailBudget = 1500 * time.Millisecond // trails are a finish, not worth a slower page
)

// addTrails gives each plane on an airport's map the end of its track (the
// API's last five minutes, cached a minute), ending where it is now. A track
// not back within trailBudget leaves its plane without a trail this time;
// the lookups carry on and fill the cache for the next view.
func (s *Server) addTrails(ctx context.Context, planes []mapPlane, ids []string) {
	trails := make([][][2]float64, len(planes))
	var mu sync.Mutex
	closed := false
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i, id := range ids {
		if id == "" {
			continue
		}
		at := planes[i].P
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			t, err := s.api.FlightTrack(ctx, id)
			if err != nil || t == nil || !t.Live {
				return
			}
			tr := trail(t.Tail5m, at)
			mu.Lock()
			if !closed {
				trails[i] = tr
			}
			mu.Unlock()
		}()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(trailBudget):
	case <-ctx.Done():
	}
	mu.Lock()
	closed = true
	for i, tr := range trails {
		planes[i].T = tr
	}
	mu.Unlock()
}

// trail is the last trailWindow of a track, a point every trailStep, ending
// at the aircraft (at). Dead-reckoned points are left out, as on the flight
// page's map, and a track whose last report is far from where the aircraft
// is now (a gap in coverage) draws none rather than a false line.
func trail(pts []api.TrackPoint, at [2]float64) [][2]float64 {
	var real []api.TrackPoint
	for _, p := range pts {
		if p.Class != "est" {
			real = append(real, p)
		}
	}
	if len(real) == 0 {
		return nil
	}
	last := real[len(real)-1]
	if geo.DistanceMi(last.Lat, last.Lon, at[1], at[0]) > 5 {
		return nil
	}
	from := last.T - trailWindow.Milliseconds()
	var out [][2]float64
	kept := from - trailStep.Milliseconds()
	for _, p := range real {
		ll := geo.LL{Lat: p.Lat, Lon: p.Lon}
		if p.T < from || p.T-kept < trailStep.Milliseconds() || !ll.Valid() {
			continue
		}
		out, kept = append(out, pt(ll)), p.T
	}
	if len(out) == 0 || out[len(out)-1] != at {
		out = append(out, at)
	}
	if len(out) < 2 {
		return nil
	}
	return out
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
