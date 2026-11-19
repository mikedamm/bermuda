package web

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
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
	Arrivals   BoardV       // landed here lately, then inbound: earliest first
	Departures BoardV       // left here lately, then scheduled: earliest first
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
	Day    string    // the weekday of its time when that is not today at the airport ("Wed"): a paged or searched board runs a day each way
	Search string    // what the board's search looks through, lower case
	t      time.Time // the time it shows, which the board sorts by
}

// BoardV is an arrivals or departures board as the page shows it.
type BoardV struct {
	Rows           []BoardRowV
	Earlier, Later bool   // there is more to show that way
	Q              string // the search it is narrowed to, if any
	Matches        int    // with a search, the flights of the day that match it
}

// boardView is how a reader has a board: how many pages of morePage rows
// they have asked for before and after the ones around now, and what they
// searched it for. The page draws it around now; the live refresh draws it
// as the reader has it, from the whole day's board when it is not around now.
type boardView struct {
	Earlier, Later int
	Q              string
	Whole          bool // draw it from the whole day's board even around now (see buried)
}

func (v boardView) aroundNow() bool { return v.Earlier == 0 && v.Later == 0 && v.Q == "" && !v.Whole }

// buried says a board's read from now back ran out before it found the rows
// that have happened that the board shows. The API dates hundreds of a
// hub's planned arrivals at their last report (ATL, 2026-11-18), which sorts
// them all ahead of what has landed: at night 500 rows held six landings.
// The whole day's board has the rest.
func buried(b *api.Board, depth int, shown BoardV) bool {
	if b == nil || len(b.Rows) < depth {
		return false
	}
	n := 0
	for _, r := range shown.Rows {
		if r.Past {
			n++
		}
	}
	return n < flownRows
}

// unbury sets a board around now to be drawn from the whole day's when its
// read was buried; on the page, which draws now, it starts that loading
// for the page's first refresh instead.
func (s *Server) unbury(v *AirportPageV, b boardsAnswer, arr, dep *boardView) {
	for _, side := range []struct {
		full  *api.Board
		depth int
		shown BoardV
		kind  string
		view  *boardView
	}{{b.arr, arrivalsDepth, v.Arrivals, "arrivals", arr}, {b.dep, departuresDepth, v.Departures, "departures", dep}} {
		if !buried(side.full, side.depth, side.shown) {
			continue
		}
		if side.view == nil {
			go s.dayBoard(context.Background(), v.Airport.ICAO, side.kind)
		} else if side.view.aroundNow() {
			side.view.Whole = true
		}
	}
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

// boardsAnswer is what the two boards are drawn from: the full arrivals and
// departures boards for what has landed and left, and the full enroute and
// scheduled ones for what is coming (each nil when it failed, when the page's
// ten-row previews of the last two stand in).
type boardsAnswer struct {
	page           *api.AirportBoards
	arr, dep       *api.Board
	enroute, sched *api.Board
}

// rowSide is the half of a board a row belongs in.
type rowSide int

const (
	sideNone   rowSide = iota // lost to coverage: on neither
	sidePast                  // it has landed or left (or was cancelled for a time gone by): above the line at now
	sideComing                // still to come: below it
)

// sideOf places a row by its phase. The full boards hold every flight of
// any phase, planned ones ahead of flown ones, and so does the page's
// ten-row preview of them, which is why it is not used for what happened.
// On an arrivals board a flight taxiing has landed once it has a
// touchdown, else it is taxiing out where it comes from; on a departures
// board, taxiing or in the air, it has left.
func sideOf(r api.BoardRow, arriving bool, now time.Time) rowSide {
	switch r.Phase() {
	case "landed", "arrived", "diverted":
		return sidePast
	case "en_route":
		if arriving {
			return sideComing
		}
		return sidePast
	case "taxiing":
		if arriving && (r.Live == nil || r.Live.On.IsZero()) {
			return sideComing
		}
		return sidePast
	case "scheduled":
		return sideComing
	case "cancelled":
		if r.BoardTime.After(now) {
			return sideComing
		}
		return sidePast
	}
	return sideNone
}

const (
	// how deep the full boards are read for ten flown rows: flights in the
	// air without an estimate and plans past their time sort among the
	// landed ones, a few hundred of them at a hub before dawn
	arrivalsDepth   = 500
	departuresDepth = 60
	flownRows       = 15
	// what is coming: half an hour of a hub's inbound flights, at most ten
	// of them already past their time; read deep enough for those (a hub's
	// scheduled board starts an hour back) and the ones it holds twice
	comingRows  = 30
	dueRows     = 10
	comingDepth = 80
	// paging: each press of Earlier or Later shows this many more, up to
	// maxPages presses; the whole day's board is read in pages of dayDepth
	morePage = 40
	maxPages = 50
	dayDepth = 2000
)

// loadBoards fetches the page's boards and the four full boards together:
// arrivals and departures from now back, enroute and scheduled from now on.
// Only the page's answer is required.
func (s *Server) loadBoards(ctx context.Context, icao string, now time.Time) (boardsAnswer, error) {
	var out boardsAnswer
	var wg sync.WaitGroup
	for _, side := range []struct {
		kind   string
		depth  int
		before time.Time
		dst    **api.Board
	}{
		{"arrivals", arrivalsDepth, now, &out.arr},
		{"departures", departuresDepth, now, &out.dep},
		{"enroute", comingDepth, time.Time{}, &out.enroute},
		{"scheduled", comingDepth, time.Time{}, &out.sched},
	} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if b, err := s.api.AirportBoard(ctx, icao, side.kind, 6, side.depth, side.before); err == nil {
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
	for _, full := range []struct {
		b        *api.Board
		arriving bool
	}{{b.arr, true}, {b.enroute, true}, {b.dep, false}, {b.sched, false}} {
		if full.b != nil {
			s.index.NoteBoard(icao, full.b.Rows, full.arriving, now)
		}
	}
}

// flown is a board's first n rows that have happened, newest first (all
// of them for n < 0).
func flown(b *api.Board, arriving bool, n int, now time.Time) []api.BoardRow {
	if b == nil {
		return nil
	}
	var out []api.BoardRow
	for _, r := range b.Rows {
		if sideOf(r, arriving, now) == sidePast {
			out = append(out, r)
			if len(out) == n {
				break
			}
		}
	}
	return out
}

// toCome is a board's rows still to come, in its order.
func toCome(b *api.Board, arriving bool, now time.Time) []api.BoardRow {
	var out []api.BoardRow
	for _, r := range b.Rows {
		if sideOf(r, arriving, now) == sideComing {
			out = append(out, r)
		}
	}
	return out
}

// coming is a full board's rows, else the page's first ten of it.
func coming(full *api.Board, preview []api.BoardRow) []api.BoardRow {
	if full != nil {
		return full.Rows
	}
	return preview
}

// setBoards fills the arrivals and departures boards around now. Their
// reads stop short of the whole day, so both can always page further.
func (v *AirportPageV) setBoards(b boardsAnswer, now time.Time) {
	v.Boards = true
	v.Arrivals = pickRows(flown(b.arr, true, flownRows, now), coming(b.enroute, b.page.Boards.Enroute), true, v.Airport.TZ, now, boardView{})
	v.Departures = pickRows(flown(b.dep, false, flownRows, now), coming(b.sched, b.page.Boards.Scheduled), false, v.Airport.TZ, now, boardView{})
	v.Arrivals.Earlier, v.Arrivals.Later = true, true
	v.Departures.Earlier, v.Departures.Later = true, true
}

// dayBoard is one of an airport's boards as far as the API holds it, a day
// back to its last plan: read whole (up to three pages of dayDepth, newest
// first) for paging and search, and kept a minute like every answer.
func (s *Server) dayBoard(ctx context.Context, icao, kind string) (*api.Board, error) {
	out := &api.Board{Kind: kind}
	var before time.Time
	for range 3 {
		b, err := s.api.AirportBoard(ctx, icao, kind, 24, dayDepth, before)
		if err != nil {
			if len(out.Rows) == 0 {
				return nil, err
			}
			break // what came is the newest part, the part readers page through first
		}
		out.Rows = append(out.Rows, b.Rows...)
		if b.NextBefore == nil || b.NextBefore.IsZero() {
			break
		}
		before = *b.NextBefore
	}
	return out, nil
}

// pageBoards redraws the boards a reader has paged or searched from the
// whole day's, which also teach the index this field's routes.
func (s *Server) pageBoards(ctx context.Context, v *AirportPageV, arr, dep boardView, now time.Time) error {
	type side struct {
		view     boardView
		kind     string
		arriving bool
		dst      *BoardV
		day      *api.Board
		err      error
	}
	sides := []*side{{view: arr, kind: "arrivals", arriving: true, dst: &v.Arrivals}, {view: dep, kind: "departures", dst: &v.Departures}}
	var wg sync.WaitGroup
	for _, sd := range sides {
		if !sd.view.aroundNow() {
			wg.Add(1)
			go func() {
				defer wg.Done()
				sd.day, sd.err = s.dayBoard(ctx, v.Airport.ICAO, sd.kind)
			}()
		}
	}
	wg.Wait()
	for _, sd := range sides {
		if sd.view.aroundNow() {
			continue
		}
		if sd.err != nil {
			return sd.err
		}
		s.index.NoteBoard(v.Airport.ICAO, sd.day.Rows, sd.arriving, now)
		*sd.dst = pickRows(flown(sd.day, sd.arriving, -1, now), toCome(sd.day, sd.arriving, now), sd.arriving, v.Airport.TZ, now, sd.view)
	}
	return nil
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
		s.unbury(&v, *b, nil, nil)
		// the boards' flights in the air near the field go on the map
		for _, side := range []struct {
			rows []BoardRowV
			kind string
		}{{v.Arrivals.Rows, "arr"}, {v.Departures.Rows, "dep"}} {
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
// into the board around now (pickRows with nothing paged or searched).
func boardRows(recent, coming []api.BoardRow, arriving bool, tz string, now time.Time) []BoardRowV {
	return pickRows(recent, coming, arriving, tz, now, boardView{}).Rows
}

// pickRows draws a board from its rows that have happened (newest first)
// and those to come (any order), in the order an airport's screens show
// them: what has landed (or left), then what is on its way. Each row shows
// the time the board sorts it by (wheels-on or gate-in, wheels-off or
// gate-out, the estimate, the schedule), so the column reads in order and
// is never blank. It shows the newest flownRows of the first and the
// soonest comingRows of the second, morePage more for each page the reader
// has asked for, of the rows matching what they searched for. A flight the
// data holds twice shows once: the same flight number within half an hour
// on the board, or still listed as coming within three hours of landing or
// leaving.
func pickRows(recent, coming []api.BoardRow, arriving bool, tz string, now time.Time, view boardView) BoardV {
	out := BoardV{Q: view.Q}
	terms := searchTerms(view.Q)
	wantPast, wantNext := flownRows+morePage*view.Earlier, comingRows+morePage*view.Later
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
	keep := func(r BoardRowV, within time.Duration) bool {
		if fresh(firstNonEmpty(r.Ident, r.FlightID), firstSet(r.t, now), within) {
			out.Rows = append(out.Rows, r)
			return true
		}
		return false
	}
	// a search counts every match, so it reads every row; paging stops a
	// row past what it shows
	var past []BoardRowV
	for _, b := range recent {
		if terms == nil && len(past) > wantPast {
			break
		}
		if r, ok := boardRow(b, true, arriving, tz, now); ok && r.matches(terms) {
			past = append(past, r)
		}
	}
	if len(past) > wantPast {
		out.Earlier = true
	}
	out.Matches = len(past)
	for i := min(len(past), wantPast) - 1; i >= 0; i-- {
		keep(past[i], 30*time.Minute)
	}
	// The API lists the inbound flights with an estimate or a metered time
	// ahead of those with only a schedule, which it gives no board time, so
	// the coming rows go in the order of the time each one shows (one with
	// none last) before a flight held twice is dropped.
	var next []BoardRowV
	for _, b := range coming {
		if r, ok := boardRow(b, false, arriving, tz, now); ok && r.matches(terms) {
			next = append(next, r)
		}
	}
	sort.SliceStable(next, func(i, j int) bool {
		a, b := next[i].t, next[j].t
		return !a.IsZero() && (b.IsZero() || a.Before(b))
	})
	// Plans past their time (most about to push back, some never closed)
	// head the list; at ORD three dozen of them filled the half before it
	// reached the next minute, so only the latest few stay.
	due := 0
	for due < len(next) && !next[due].t.IsZero() && next[due].t.Before(now) {
		due++
	}
	if due > dueRows {
		next = next[due-dueRows:]
	}
	out.Matches += len(next)
	shown := 0
	for _, r := range next {
		if shown == wantNext {
			out.Later = true
			break
		}
		if keep(r, 3*time.Hour) {
			shown++
		}
	}
	markTwins(out.Rows)
	return out
}

// boardRow is one board row as the page shows it; past says it has landed
// or left. A row the data withholds (LADD) has none.
func boardRow(b api.BoardRow, past, arriving bool, tz string, now time.Time) (BoardRowV, bool) {
	var l LegV
	var op api.Operator
	switch {
	case b.Live != nil && !b.Live.LADDRedacted:
		l, op = legFromLive(*b.Live, refs{}, now), b.Live.Operator
	case b.Archived != nil && !b.Archived.LADDRedacted:
		l, op = legFromHistory(*b.Archived, now), b.Archived.Operator
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
	// The board's own time, unless a flight still to come has only its last
	// report there: the API falls back to that for a plan it has no time
	// for, and gives it to hundreds of planned arrivals that have a schedule
	// (ATL, 2026-11-18), which would put them all at now. What has happened
	// keeps the board's time, which places it among the others.
	if !b.BoardTime.IsZero() && (past || !r.At.Set || b.Live == nil || !b.BoardTime.Equal(b.Live.LastSeen)) {
		r.At = timeCell(b.BoardTime, tz, "")
	}
	r.t, _ = time.Parse(time.RFC3339, r.At.ISO)
	if loc := loadZone(tz); !r.t.IsZero() {
		if y, m, d := r.t.In(loc).Date(); time.Date(y, m, d, 0, 0, 0, 0, loc) != dayStart(now, loc) {
			r.Day = r.t.In(loc).Format("Mon")
		}
	}
	r.Search = searchText(r, op)
	return r, true
}

func dayStart(t time.Time, loc *time.Location) time.Time {
	y, m, d := t.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}

// searchText is what a board's search looks through for a row: everything
// it shows (the flight, the airline, where from or to by city, name and
// code, the gate and carousel, the status, the time) in lower case.
func searchText(r BoardRowV, op api.Operator) string {
	var b strings.Builder
	for _, s := range []string{r.Number, r.Ident, op.Name, op.ICAO, r.City, r.Place.Name, r.Place.Code, r.Place.ICAO,
		r.Gate, r.Claim, r.Status, r.Note, r.At.Clock} {
		if s != "" {
			if b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(s)
		}
	}
	return strings.ToLower(b.String())
}

// searchTerms is a search split into its words, lower case: a row matches
// when its text holds every one ("dl 1234" finds DL1234). None for none.
func searchTerms(q string) []string {
	return strings.Fields(strings.ToLower(q))
}

func (r BoardRowV) matches(terms []string) bool {
	for _, t := range terms {
		if !strings.Contains(r.Search, t) {
			return false
		}
	}
	return true
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
