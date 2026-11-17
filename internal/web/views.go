package web

import (
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/mikedamm/bermuda/internal/api"
	"github.com/mikedamm/bermuda/internal/geo"
	"github.com/mikedamm/bermuda/internal/store"
)

// AirportV is an airport as pages show it.
type AirportV struct {
	Code    string // IATA, else ICAO
	ICAO    string
	Name    string
	City    string
	Place   string
	URL     string // empty when the site has no page for it
	TZ      string
	Country string
	LL      geo.LL
	Known   bool
}

// refs collects airport references from API answers so a bare code (live
// summaries carry codes only) can borrow the reference's time zone and name.
type refs map[string]*api.AirportRef

func (r refs) add(a *api.AirportRef) {
	if a != nil && a.ICAO != "" {
		r[a.ICAO] = a
		if a.IATA != "" {
			r[a.IATA] = a
		}
	}
}

func (r refs) addHistory(hs []api.HistoryEntry) {
	for _, h := range hs {
		r.add(h.Dep)
		r.add(h.Arr)
		r.add(h.ActualArr)
	}
}

func airportV(code string, ref *api.AirportRef) AirportV {
	if code == "" && ref != nil {
		code = ref.ICAO
	}
	if code == "" {
		return AirportV{}
	}
	if a, ok := geo.LookupAirport(code); ok {
		v := AirportV{Code: a.Code(), ICAO: a.ICAO, Name: a.Name, City: a.City, Place: a.Place(), URL: "/airport/" + a.ICAO,
			TZ: a.TZ, Country: a.Country, LL: geo.LL{Lat: a.Lat, Lon: a.Lon}, Known: true}
		if v.TZ == "" && ref != nil {
			v.TZ = ref.TZ
		}
		if v.City == "" {
			v.City = v.Name
		}
		return v
	}
	v := AirportV{Code: code, ICAO: code, Name: code, City: code}
	if ref != nil && ref.Name != "" {
		v.Name = titleCase(ref.Name)
		v.City = titleCase(ref.City)
		if v.City == "" {
			v.City = v.Name
		}
		v.Place = v.City
		v.TZ = ref.TZ
		v.Country = ref.Country
		v.LL = geo.LL{Lat: ref.Lat, Lon: ref.Lon}
		if ref.IATA != "" {
			v.Code = ref.IATA
		}
		v.Known = true
	}
	return v
}

func (r refs) airport(code string) AirportV { return airportV(code, r[code]) }

// LegV is one flight (live or archived) as pages show it.
type LegV struct {
	FlightID   string
	Ident      string // ICAO callsign
	Number     string // IATA flight number, else the callsign
	URL        string // flight page, empty for non-airline callsigns
	Airline    string
	AirlineURL string
	Op         string // operator ICAO code, for its logo

	Dep, Arr   AirportV
	DivertedTo *AirportV

	Phase       string
	PhaseWord   string
	StatusClass string
	Headline    string
	Live        bool
	Airborne    bool

	Date                       string
	DepPlanned, DepOut, DepOff TimeCell
	ArrPlanned, ArrEst, ArrOn  TimeCell
	ArrIn                      TimeCell
	DepDelay, DepDelayClass    string
	ArrDelay, ArrDelayClass    string
	Runway                     string
	DepGate, ArrGate           string // gate numbers, once the API sends them

	Duration   string
	flightTime time.Duration
	DistMi     float64
	Dist       string

	Reg, RegURL, Type, Model string

	HasPos                       bool
	Pos                          geo.LL
	Hdg                          float64
	Alt, Speed, Heading, Squawk  string
	PosAge                       string
	Progress                     int
	FlownMi, LeftMi              string
	sort                         time.Time
	departed, landed, plannedArr time.Time
}

var statusClass = map[string]string{
	"scheduled": "sched", "taxiing": "taxi", "en_route": "air", "landed": "landed", "arrived": "landed",
	"diverted": "alert", "cancelled": "alert", "unknown": "muted",
}

func flightURL(callsign string) string {
	if _, _, ok := store.AirlineCallsign(callsign); ok {
		return "/flight/" + callsign
	}
	return ""
}

func aircraftURL(reg string) string {
	if reg == "" {
		return ""
	}
	return "/aircraft/" + reg
}

// opCode is an operator's ICAO code for its logo: its own, else the airline
// its callsign belongs to.
func opCode(icao, callsign string) string {
	if icao != "" {
		return icao
	}
	if al, _, ok := store.AirlineCallsign(callsign); ok {
		return al
	}
	return ""
}

func legNumber(iata, callsign string) string {
	if iata != "" {
		return iata
	}
	return callsign
}

// legFromLive builds a leg from a live flight summary.
func legFromLive(fs api.FlightSummary, r refs, now time.Time) LegV {
	l := LegV{
		FlightID: fs.FlightID, Ident: fs.Callsign, Number: legNumber(fs.IATAFlight, fs.Callsign), URL: flightURL(fs.Callsign),
		Airline: fs.Operator.Name, Op: opCode(fs.Operator.ICAO, fs.Callsign), Dep: r.airport(fs.Dep), Arr: r.airport(fs.Arr),
		Phase: fs.Phase, PhaseWord: fs.PhaseWord, Live: true,
		Reg: fs.Registration, RegURL: aircraftURL(fs.Registration), Type: fs.Type, Model: fs.Model,
	}
	if runwayRe.MatchString(fs.Runway) {
		l.Runway = fs.Runway
	}
	l.DepGate, l.ArrGate = gateName(fs.DepGate), gateName(fs.ArrGate)
	if fs.ActualArr != "" && fs.ActualArr != fs.Arr {
		d := r.airport(fs.ActualArr)
		l.DivertedTo = &d
	}
	if fs.Operator.ICAO != "" && fs.Operator.Name != "" {
		l.AirlineURL = "/airline/" + fs.Operator.ICAO
	}
	planDep := fs.SchedDep
	if planDep.IsZero() {
		planDep = fs.ETD
	}
	l.DepPlanned = timeCell(planDep, l.Dep.TZ, l.Dep.Country)
	l.DepOut = timeCell(fs.Out, l.Dep.TZ, l.Dep.Country)
	l.DepOff = timeCell(fs.Off, l.Dep.TZ, l.Dep.Country)
	l.ArrPlanned = timeCell(fs.SchedArr, l.Arr.TZ, l.Arr.Country)
	est := fs.ETA
	if est.IsZero() {
		est = fs.STA
	}
	l.ArrEst = timeCell(est, l.Arr.TZ, l.Arr.Country)
	l.ArrOn = timeCell(fs.On, l.Arr.TZ, l.Arr.Country)
	l.ArrIn = timeCell(fs.In, l.Arr.TZ, l.Arr.Country)

	switch {
	case !fs.Out.IsZero():
		l.DepDelay, l.DepDelayClass = fmtDelay(fs.Out, fs.SchedDep)
	case !fs.EDCT.IsZero():
		l.DepDelay, l.DepDelayClass = fmtDelay(fs.EDCT, fs.SchedDep)
	}
	lower := func(t string) string { return strings.ToLower(t[:1]) + t[1:] }
	switch {
	case !fs.In.IsZero():
		l.ArrDelay, l.ArrDelayClass = fmtDelay(fs.In, fs.SchedArr)
	case !fs.On.IsZero():
		if t, c := fmtDelay(fs.On, fs.SchedArr); t != "" {
			l.ArrDelay, l.ArrDelayClass = "Landed "+lower(t), c
		}
	case !est.IsZero():
		if t, c := fmtDelay(est, fs.SchedArr); t != "" {
			l.ArrDelay, l.ArrDelayClass = "Expected "+lower(t), c
		}
	}

	l.departed = firstSet(fs.Off, fs.Out)
	l.landed = firstSet(fs.On, fs.In)
	l.plannedArr = firstSet(est, fs.SchedArr)
	l.sort = firstSet(fs.Off, fs.Out, planDep, fs.LastSeen)
	l.Date = dateLabel(firstSet(planDep, fs.Off, fs.LastSeen), l.Dep.TZ)

	pos := geo.LL{Lat: fs.Lat, Lon: fs.Lon}
	l.Airborne = fs.Phase == "en_route" && !fs.OnGround
	if pos.Valid() && !fs.PositionAt.IsZero() {
		l.HasPos, l.Pos, l.Hdg = true, pos, fs.Hdg
		l.Alt = fmtAlt(fs.Alt, fs.OnGround)
		l.Speed = fmtSpeed(fs.GS)
		if fs.GS > 30 {
			l.Heading = compass(fs.Hdg)
		}
		l.PosAge = relTime(fs.PositionAt, now)
	}
	l.Squawk = fs.Squawk
	l.computeDistance()
	if l.Airborne && l.HasPos && l.Dep.LL.Valid() && l.Arr.LL.Valid() {
		flown := geo.DistanceMi(l.Dep.LL.Lat, l.Dep.LL.Lon, pos.Lat, pos.Lon)
		left := geo.DistanceMi(pos.Lat, pos.Lon, l.Arr.LL.Lat, l.Arr.LL.Lon)
		if flown+left > 0 {
			l.Progress = int(math.Round(flown / (flown + left) * 100))
			l.FlownMi, l.LeftMi = fmtMiles(flown), fmtMiles(left)
		}
	}
	switch fs.Phase {
	case "landed", "arrived":
		l.Progress = 100
	}
	if d := flightTime(fs.Off, fs.On, fs.Out, fs.In); d > 0 {
		l.flightTime = d
		l.Duration = fmtDuration(d)
	} else if !fs.Off.IsZero() && fs.On.IsZero() {
		l.Duration = fmtDuration(now.Sub(fs.Off))
	}
	l.StatusClass = statusClass[fs.Phase]
	l.Headline = headline(l, now)
	return l
}

// legFromHistory builds a leg from an archived flight.
func legFromHistory(h api.HistoryEntry, now time.Time) LegV {
	l := LegV{
		FlightID: h.FlightID, Ident: h.Callsign, Number: legNumber(h.IATAFlight, h.Callsign), URL: flightURL(h.Callsign),
		Airline: h.Operator.Name, Op: opCode(h.Operator.ICAO, h.Callsign), Dep: airportV("", h.Dep), Arr: airportV("", h.Arr),
		Phase: h.Phase, PhaseWord: h.PhaseWord,
		Reg: h.Registration, RegURL: aircraftURL(h.Registration), Type: h.Type,
	}
	l.DepGate, l.ArrGate = gateName(h.DepGate), gateName(h.ArrGate)
	if h.ActualArr != nil && (h.Arr == nil || h.ActualArr.ICAO != h.Arr.ICAO) {
		d := airportV("", h.ActualArr)
		l.DivertedTo = &d
	}
	if h.Operator.ICAO != "" && h.Operator.Name != "" {
		l.AirlineURL = "/airline/" + h.Operator.ICAO
	}
	l.DepPlanned = timeCell(h.ETD, l.Dep.TZ, l.Dep.Country)
	l.DepOut = timeCell(h.Out, l.Dep.TZ, l.Dep.Country)
	l.DepOff = timeCell(h.Off, l.Dep.TZ, l.Dep.Country)
	l.ArrEst = timeCell(h.ETA, l.Arr.TZ, l.Arr.Country)
	l.ArrOn = timeCell(h.On, l.Arr.TZ, l.Arr.Country)
	l.ArrIn = timeCell(h.In, l.Arr.TZ, l.Arr.Country)
	l.departed = firstSet(h.Off, h.Out)
	l.landed = firstSet(h.On, h.In)
	l.sort = firstSet(h.Off, h.Out, h.FirstSeen)
	l.Date = dateLabel(firstSet(h.Off, h.Out, h.ETD, h.FirstSeen), l.Dep.TZ)
	l.flightTime = flightTime(h.Off, h.On, h.Out, h.In)
	l.Duration = fmtDuration(l.flightTime)
	l.computeDistance()
	switch h.Phase {
	case "landed", "arrived":
		l.Progress = 100
	}
	l.StatusClass = statusClass[h.Phase]
	l.Headline = headline(l, now)
	return l
}

func (l *LegV) computeDistance() {
	if l.Dep.LL.Valid() && l.Arr.LL.Valid() {
		l.DistMi = geo.DistanceMi(l.Dep.LL.Lat, l.Dep.LL.Lon, l.Arr.LL.Lat, l.Arr.LL.Lon)
		l.Dist = fmtMiles(l.DistMi)
	}
}

// flightTime is take-off to landing, else gate to gate, else gate out to
// landing. The API's "seen" duration (first to last report) can span days
// and is not a flight time, so it is never used.
func flightTime(off, on, out, in time.Time) time.Duration {
	ok := func(a, b time.Time) bool { return !a.IsZero() && !b.IsZero() && b.After(a) && b.Sub(a) < 20*time.Hour }
	switch {
	case ok(off, on):
		return on.Sub(off)
	case ok(out, in):
		return in.Sub(out)
	case ok(out, on):
		return on.Sub(out)
	}
	return 0
}

var runwayRe = regexp.MustCompile(`^(0?[1-9]|[12][0-9]|3[0-6])[LRCW]?$`)

// gateRe is what a gate reads like on a screen: B4, N12, 37A, C-17.
var gateRe = regexp.MustCompile(`^[A-Z0-9][A-Z0-9-]{0,5}$`)

// gateName is a gate as boards print it, or nothing for a value that does
// not read like one.
func gateName(g string) string {
	if g = strings.ToUpper(strings.TrimSpace(g)); gateRe.MatchString(g) {
		return g
	}
	return ""
}

func firstSet(ts ...time.Time) time.Time {
	for _, t := range ts {
		if !t.IsZero() {
			return t
		}
	}
	return time.Time{}
}

// headline is the one-line answer to "where is it now?".
func headline(l LegV, now time.Time) string {
	switch l.Phase {
	case "en_route":
		if !l.plannedArr.IsZero() && l.plannedArr.After(now) {
			return "In the air, landing in " + strings.TrimPrefix(relTime(l.plannedArr, now), "in ")
		}
		return "In the air"
	case "taxiing":
		return "Taxiing for departure"
	case "scheduled":
		if t := l.DepPlanned; t.Set {
			if dt, err := time.Parse(time.RFC3339, t.ISO); err == nil && dt.After(now) {
				return "Scheduled to depart " + relTime(dt, now)
			}
		}
		return "Scheduled"
	case "landed":
		if !l.landed.IsZero() {
			return "Landed " + relTime(l.landed, now)
		}
		return "Landed"
	case "arrived":
		if !l.landed.IsZero() {
			return "Arrived " + relTime(l.landed, now)
		}
		return "Arrived"
	case "diverted":
		if l.DivertedTo != nil {
			return "Diverted to " + l.DivertedTo.City
		}
		return "Diverted"
	case "cancelled":
		return "Cancelled"
	}
	if l.PhaseWord != "" {
		return l.PhaseWord
	}
	return "Status unknown"
}

// legMap draws a leg's route; the live position splits flown from ahead.
func legMap(l LegV, w, h int) *geo.Map {
	arr := l.Arr
	if l.DivertedTo != nil && l.DivertedTo.LL.Valid() {
		arr = *l.DivertedTo
	}
	if !l.Dep.LL.Valid() && !arr.LL.Valid() {
		return nil
	}
	spec := geo.MapSpec{W: w, H: h}
	if l.Dep.LL.Valid() {
		spec.Pins = append(spec.Pins, geo.MapPin{At: l.Dep.LL, Label: l.Dep.Code, Primary: true})
	}
	if arr.LL.Valid() {
		spec.Pins = append(spec.Pins, geo.MapPin{At: arr.LL, Label: arr.Code, Primary: true})
	}
	if l.Dep.LL.Valid() && arr.LL.Valid() {
		rt := geo.MapRoute{From: l.Dep.LL, To: arr.LL, Flown: l.Progress >= 100}
		if l.Airborne && l.HasPos {
			p := l.Pos
			rt.Pos = &p
		}
		spec.Routes = []geo.MapRoute{rt}
	}
	if l.HasPos && l.Phase != "arrived" && l.Phase != "landed" {
		p := l.Pos
		spec.Plane, spec.PlaneHdg = &p, l.Hdg
	}
	m := geo.RenderMap(spec)
	return &m
}

// Moment is the most telling time at one end of a leg, with its label.
type Moment struct {
	Label string
	T     TimeCell
	Delay string
	Class string
}

// DepMoment: what happened at the origin most recently, else the plan.
func (l LegV) DepMoment() Moment {
	switch {
	case l.DepOff.Set:
		return Moment{"Took off", l.DepOff, l.DepDelay, l.DepDelayClass}
	case l.DepOut.Set:
		return Moment{"Left the gate", l.DepOut, l.DepDelay, l.DepDelayClass}
	case l.DepPlanned.Set:
		return Moment{"Scheduled departure", l.DepPlanned, l.DepDelay, l.DepDelayClass}
	}
	return Moment{}
}

// shortDelay drops the "Expected " / "Landed " lead-in where a label already says it.
func shortDelay(t string) string {
	return capFirst(strings.TrimPrefix(strings.TrimPrefix(t, "Expected "), "Landed "))
}

// ArrMoment: landing if it happened, else the estimate, else the plan.
func (l LegV) ArrMoment() Moment {
	switch {
	case l.ArrIn.Set && l.ArrOn.Set:
		return Moment{"Landed", l.ArrOn, shortDelay(l.ArrDelay), l.ArrDelayClass}
	case l.ArrOn.Set:
		return Moment{"Landed", l.ArrOn, shortDelay(l.ArrDelay), l.ArrDelayClass}
	case l.ArrIn.Set:
		return Moment{"Reached the gate", l.ArrIn, shortDelay(l.ArrDelay), l.ArrDelayClass}
	case l.ArrEst.Set && l.Phase != "cancelled":
		return Moment{"Expected", l.ArrEst, shortDelay(l.ArrDelay), l.ArrDelayClass}
	case l.ArrPlanned.Set:
		return Moment{"Scheduled arrival", l.ArrPlanned, shortDelay(l.ArrDelay), l.ArrDelayClass}
	}
	return Moment{}
}

// StatusBand is the one-line answer welded to the top of a flight panel,
// coloured by state: ok (green), late (amber), bad (red), live (blue), idle.
type StatusBand struct {
	Class  string
	Title  string
	Detail string
}

// Band answers "where is it, and is it on time" for one leg.
func (l LegV) Band() StatusBand {
	delay := func() (string, string) {
		t := strings.TrimPrefix(strings.TrimPrefix(l.ArrDelay, "Expected "), "Landed ")
		if t == "" {
			return "", ""
		}
		t = strings.ToUpper(t[:1]) + t[1:]
		switch l.ArrDelayClass {
		case "late":
			return t, "late"
		}
		return t, "ok"
	}
	// A late departure outranks an on-time arrival estimate: schedules get
	// re-timed after a delay, and green next to "3h 56m late" is a lie.
	depLate := func() (StatusBand, bool) {
		if l.DepDelayClass == "late" && (l.DepOut.Set || l.DepOff.Set) {
			return StatusBand{"late", "", "Departed " + strings.ToLower(l.DepDelay)}, true
		}
		return StatusBand{}, false
	}
	switch l.Phase {
	case "en_route":
		d, c := delay()
		if c != "late" {
			if b, ok := depLate(); ok {
				b.Title = "En route"
				return b
			}
		}
		if c == "" {
			c = "live"
		}
		if d == "" {
			d = strings.TrimPrefix(l.Headline, "In the air, ")
			if d == "In the air" {
				d = ""
			}
		}
		return StatusBand{c, "En route", capFirst(d)}
	case "taxiing":
		return StatusBand{"live", "Taxiing", l.DepDelay}
	case "scheduled":
		c := "idle"
		if l.DepDelayClass == "late" {
			c = "late"
		}
		return StatusBand{c, "Scheduled", capFirst(strings.TrimPrefix(l.Headline, "Scheduled to depart "))}
	case "landed", "arrived":
		d, c := delay()
		if c == "" {
			if b, ok := depLate(); ok {
				b.Title = map[string]string{"landed": "Landed", "arrived": "Arrived"}[l.Phase]
				return b
			}
		}
		if c == "" {
			c, d = "ok", strings.TrimPrefix(strings.TrimPrefix(l.Headline, "Landed "), "Arrived ")
		}
		title := "Arrived"
		if l.Phase == "landed" {
			title = "Landed"
		}
		return StatusBand{c, title, capFirst(d)}
	case "diverted":
		d := ""
		if l.DivertedTo != nil {
			d = "Now going to " + l.DivertedTo.City
		}
		return StatusBand{"bad", "Diverted", d}
	case "cancelled":
		return StatusBand{"bad", "Cancelled", ""}
	}
	return StatusBand{"idle", "No current data", ""}
}

// TimeRow is one line of the scheduled-versus-actual times table.
type TimeRow struct {
	Event     string
	Sched     TimeCell
	Actual    TimeCell
	Estimated bool
	Diff      string
	DiffClass string
}

// DepTimes and ArrTimes are the departure and arrival halves of the table.
func (l LegV) DepTimes() []TimeRow {
	gate := TimeRow{Event: "Gate departure", Sched: l.DepPlanned, Actual: l.DepOut, Diff: l.DepDelay, DiffClass: l.DepDelayClass}
	if !l.DepOut.Set {
		gate.Diff = ""
	}
	return []TimeRow{gate, {Event: "Takeoff", Actual: l.DepOff}}
}

func (l LegV) ArrTimes() []TimeRow {
	landing := TimeRow{Event: "Landing", Actual: l.ArrOn}
	if !l.ArrOn.Set && l.ArrEst.Set && l.Phase != "cancelled" {
		landing.Actual, landing.Estimated = l.ArrEst, true
	}
	gate := TimeRow{Event: "Gate arrival", Sched: l.ArrPlanned, Actual: l.ArrIn}
	if l.ArrIn.Set {
		gate.Diff, gate.DiffClass = fmtDelay2(l.ArrIn, l.ArrPlanned)
	}
	return []TimeRow{landing, gate}
}

func fmtDelay2(actual, sched TimeCell) (string, string) {
	a, err1 := time.Parse(time.RFC3339, actual.ISO)
	s, err2 := time.Parse(time.RFC3339, sched.ISO)
	if err1 != nil || err2 != nil || !actual.Set || !sched.Set {
		return "", ""
	}
	return fmtDelay(a, s)
}

// ArrName is the airport the leg ends at: the diversion field if any.
func (l LegV) ArrName() string {
	if l.DivertedTo != nil {
		return l.DivertedTo.Name
	}
	return l.Arr.Name
}

func capFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// BarV is the condensed band on flight and aircraft pages (band-bar in
// parts.html): the airline's mark with a short code and the tail number by
// it, and the one-line summary of the dock the band takes over.
type BarV struct {
	Logo, Code, Reg string
	Leg             *LegV // the flight the summary sums up; nil for none
	Band            StatusBand
	Part            string // the live part the summary is, so the refresh keeps it current
}

// Bar is the flight page's: its airline, its ICAO ident and its aircraft,
// summed up as live parts so the refresh keeps it current.
func (v FlightPageV) Bar() BarV {
	f := v.Featured
	b := BarV{Logo: v.AirlineICAO, Code: v.Ident, Reg: f.Reg, Leg: &f, Band: f.Band(), Part: "bar"}
	if b.Code == b.Reg {
		b.Code = "" // a private flight flies under its registration
	}
	return b
}

// Bar is the aircraft page's: the flight it is on (else its latest) and
// whose it is, or its operator, by its tail.
func (v AircraftPageV) Bar() BarV {
	b := BarV{Reg: v.Registration, Band: v.Band, Leg: v.Live}
	if b.Leg == nil {
		b.Leg = v.Latest
	}
	if v.Hero != nil {
		b.Logo, b.Code = v.Hero.Logo, v.Hero.Code
	}
	if b.Code == b.Reg {
		b.Code = ""
	}
	return b
}
