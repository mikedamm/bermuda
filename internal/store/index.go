// Package store keeps what the site has seen: flights, aircraft, airlines and
// airport activity. It feeds sitemaps, hub pages and internal links, and is
// snapshotted to disk so a restart does not forget the long tail.
package store

import (
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mikedamm/bermuda/internal/api"
)

type Flight struct {
	Ident    string    `json:"i"`           // ICAO callsign, UAL1275
	IATA     string    `json:"a,omitempty"` // UA1275
	Airline  string    `json:"o,omitempty"` // ICAO operator
	Dep      string    `json:"d,omitempty"` // ICAO airport
	Arr      string    `json:"r,omitempty"`
	Reg      string    `json:"g,omitempty"`
	Type     string    `json:"t,omitempty"`
	LastSeen time.Time `json:"s"`
}

type Aircraft struct {
	Reg      string    `json:"g"`
	Hex      string    `json:"h,omitempty"`
	Type     string    `json:"t,omitempty"`
	Model    string    `json:"m,omitempty"`
	Airline  string    `json:"o,omitempty"`
	LADD     bool      `json:"l,omitempty"`
	LastSeen time.Time `json:"s"`
}

type Airline struct {
	ICAO     string    `json:"c"`
	IATA     string    `json:"a,omitempty"`
	Name     string    `json:"n,omitempty"`
	LastSeen time.Time `json:"s"`
}

// AirportActivity is the latest sweep of one airport.
type AirportActivity struct {
	ICAO      string    `json:"c"`
	Swept     time.Time `json:"s"`
	Inbound   int       `json:"in"`
	Outbound  int       `json:"out"`
	OnGround  int       `json:"g"`
	Overhead  int       `json:"o"`
	Total     int       `json:"t"`
	EmptyRuns int       `json:"e,omitempty"`
}

type Index struct {
	saveMu   sync.Mutex // one snapshot write at a time
	mu       sync.RWMutex
	flights  map[string]*Flight
	aircraft map[string]*Aircraft
	airlines map[string]*Airline
	airports map[string]*AirportActivity
	routes   map[string]map[string]*Route // airport, then "AIRLINE OTHER"
	dirty    bool
	saved    time.Time // last successful snapshot
}

func NewIndex() *Index {
	return &Index{
		flights:  map[string]*Flight{},
		aircraft: map[string]*Aircraft{},
		airlines: map[string]*Airline{},
		airports: map[string]*AirportActivity{},
		routes:   map[string]map[string]*Route{},
	}
}

var airlineCallsign = regexp.MustCompile(`^([A-Z]{3})([0-9][0-9A-Z]{0,4})$`)

// AirlineCallsign splits UAL1275 into ("UAL", "1275"); ok is false for
// registrations used as callsigns (N123AB, CFABC) and other free text.
func AirlineCallsign(cs string) (airline, number string, ok bool) {
	m := airlineCallsign.FindStringSubmatch(strings.ToUpper(strings.TrimSpace(cs)))
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}

func later(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func set(dst *string, v string) {
	if v != "" {
		*dst = v
	}
}

func (x *Index) noteFlight(f Flight) {
	if _, _, ok := AirlineCallsign(f.Ident); !ok {
		return
	}
	cur, ok := x.flights[f.Ident]
	if !ok {
		cur = &Flight{Ident: f.Ident}
		x.flights[f.Ident] = cur
	}
	// newer observations of a route replace it; older ones only fill gaps
	newer := !f.LastSeen.Before(cur.LastSeen)
	fill := func(dst *string, v string) {
		if v != "" && (newer || *dst == "") {
			*dst = v
		}
	}
	// A sweep knows one end of a route. If that end moved, the stored other
	// end belongs to a different leg; forget it rather than splice two legs.
	if newer && f.Dep == "" && f.Arr != "" && cur.Arr != "" && cur.Arr != f.Arr {
		cur.Dep = ""
	}
	if newer && f.Arr == "" && f.Dep != "" && cur.Dep != "" && cur.Dep != f.Dep {
		cur.Arr = ""
	}
	fill(&cur.IATA, f.IATA)
	fill(&cur.Airline, f.Airline)
	fill(&cur.Dep, f.Dep)
	fill(&cur.Arr, f.Arr)
	fill(&cur.Reg, f.Reg)
	fill(&cur.Type, f.Type)
	cur.LastSeen = later(cur.LastSeen, f.LastSeen)
	x.dirty = true
}

func (x *Index) noteAircraft(a Aircraft) {
	if a.Reg == "" {
		return
	}
	cur, ok := x.aircraft[a.Reg]
	if !ok {
		cur = &Aircraft{Reg: a.Reg}
		x.aircraft[a.Reg] = cur
	}
	set(&cur.Hex, a.Hex)
	set(&cur.Type, a.Type)
	set(&cur.Model, a.Model)
	set(&cur.Airline, a.Airline)
	cur.LADD = cur.LADD || a.LADD
	cur.LastSeen = later(cur.LastSeen, a.LastSeen)
	x.dirty = true
}

func (x *Index) noteAirline(op api.Operator, seen time.Time) {
	if len(op.ICAO) != 3 {
		return
	}
	cur, ok := x.airlines[op.ICAO]
	if !ok {
		cur = &Airline{ICAO: op.ICAO}
		x.airlines[op.ICAO] = cur
	}
	set(&cur.IATA, op.IATA)
	set(&cur.Name, op.Name)
	cur.LastSeen = later(cur.LastSeen, seen)
	x.dirty = true
}

// NoteLive records a live flight summary.
func (x *Index) NoteLive(fs api.FlightSummary) {
	if fs.LADDRedacted || fs.Callsign == "" {
		return
	}
	seen := later(fs.LastSeen, fs.PositionAt)
	x.mu.Lock()
	defer x.mu.Unlock()
	x.noteFlight(Flight{Ident: fs.Callsign, IATA: fs.IATAFlight, Airline: fs.Operator.ICAO, Dep: fs.Dep, Arr: fs.Arr,
		Reg: fs.Registration, Type: fs.Type, LastSeen: seen})
	x.noteAircraft(Aircraft{Reg: fs.Registration, Hex: fs.Hex, Type: fs.Type, Model: fs.Model, Airline: fs.Operator.ICAO, LastSeen: seen})
	x.noteAirline(fs.Operator, seen)
}

// NoteHistory records archived flights.
func (x *Index) NoteHistory(hs []api.HistoryEntry) {
	x.mu.Lock()
	defer x.mu.Unlock()
	for _, h := range hs {
		if h.LADDRedacted || h.Callsign == "" {
			continue
		}
		code := func(r *api.AirportRef) string {
			if r == nil {
				return ""
			}
			return r.ICAO
		}
		x.noteFlight(Flight{Ident: h.Callsign, IATA: h.IATAFlight, Airline: h.Operator.ICAO, Dep: code(h.Dep), Arr: code(h.Arr),
			Reg: h.Registration, Type: h.Type, LastSeen: h.LastSeen})
		x.noteAirline(h.Operator, h.LastSeen)
	}
}

// NoteAircraftPage records an aircraft page's identity.
func (x *Index) NoteAircraftPage(p *api.AircraftPage) {
	seen := p.Stats.LastFlightAt
	if p.Status.Last != nil {
		seen = later(seen, p.Status.Last.At)
	}
	x.mu.Lock()
	x.noteAircraft(Aircraft{Reg: p.Aircraft.Registration, Hex: p.Aircraft.Hex, Type: p.Aircraft.ICAOType, Model: p.Aircraft.Name,
		Airline: p.Operator.ICAO, LADD: p.LADD.Listed, LastSeen: seen})
	if !p.LADD.Listed {
		x.noteAirline(p.Operator, seen)
	}
	x.mu.Unlock()
	if !p.LADD.Listed {
		x.NoteHistory(p.RecentFlights)
	}
}

// NoteSweep records an airport sweep. Direction tells half a route for free:
// an arrival is going to this airport, a departure is coming from it.
func (x *Index) NoteSweep(icao string, t *api.AirportTraffic, at time.Time) AirportActivity {
	act := AirportActivity{ICAO: icao, Swept: at, Total: len(t.Aircraft)}
	x.mu.Lock()
	defer x.mu.Unlock()
	for _, a := range t.Aircraft {
		switch {
		case a.Direction == "transit":
			act.Overhead++
		case a.OnGround:
			act.OnGround++
		case a.Direction == "arriving":
			act.Inbound++
		case a.Direction == "departing":
			act.Outbound++
		}
		live := a.Status == "active" || a.Status == "landed"
		if live {
			f := Flight{Ident: a.Callsign, Reg: a.Registration, LastSeen: at}
			if al, _, ok := AirlineCallsign(a.Callsign); ok {
				f.Airline = al
			}
			if a.Direction == "arriving" {
				f.Arr = icao
			} else if a.Direction == "departing" {
				f.Dep = icao
			}
			x.noteFlight(f)
		}
		if a.Registration != "" && (live || a.OnGround) {
			x.noteAircraft(Aircraft{Reg: a.Registration, Hex: a.Hex, LastSeen: at})
		}
	}
	if prev, ok := x.airports[icao]; ok && act.Total == 0 {
		act.EmptyRuns = prev.EmptyRuns + 1
	}
	x.airports[icao] = &act
	x.dirty = true
	return act
}

// ---------- queries ----------

func (x *Index) Flight(ident string) (Flight, bool) {
	x.mu.RLock()
	defer x.mu.RUnlock()
	f, ok := x.flights[ident]
	if !ok {
		return Flight{}, false
	}
	return *f, true
}

// AircraftByReg returns what the index knows about one registration.
func (x *Index) AircraftByReg(reg string) (Aircraft, bool) {
	x.mu.RLock()
	defer x.mu.RUnlock()
	a, ok := x.aircraft[reg]
	if !ok {
		return Aircraft{}, false
	}
	return *a, true
}

func (x *Index) Airline(icao string) (Airline, bool) {
	x.mu.RLock()
	defer x.mu.RUnlock()
	a, ok := x.airlines[icao]
	if !ok {
		return Airline{}, false
	}
	return *a, true
}

// AirlineByIATA resolves a two-character IATA code to the airline seen most
// recently with it.
func (x *Index) AirlineByIATA(iata string) (Airline, bool) {
	x.mu.RLock()
	defer x.mu.RUnlock()
	var best *Airline
	for _, a := range x.airlines {
		if a.IATA == iata && (best == nil || a.LastSeen.After(best.LastSeen)) {
			best = a
		}
	}
	if best == nil {
		return Airline{}, false
	}
	return *best, true
}

// AirlinesMissingName lists airline prefixes seen in callsigns whose name
// the index does not know yet, with one callsign to look each up by.
func (x *Index) AirlinesMissingName(since time.Time, n int) map[string]string {
	x.mu.RLock()
	defer x.mu.RUnlock()
	out := map[string]string{}
	for _, f := range x.flights {
		if len(out) >= n || f.LastSeen.Before(since) || f.Airline == "" {
			continue
		}
		if a, ok := x.airlines[f.Airline]; ok && a.Name != "" {
			continue
		}
		if _, ok := out[f.Airline]; !ok {
			out[f.Airline] = f.Ident
		}
	}
	return out
}

// FlightsNeedingRoute lists recently seen airline flights with half a route,
// most recent first.
func (x *Index) FlightsNeedingRoute(since time.Time, n int) []string {
	x.mu.RLock()
	var fs []*Flight
	for _, f := range x.flights {
		if f.LastSeen.After(since) && (f.Dep == "" || f.Arr == "" || f.IATA == "") {
			fs = append(fs, f)
		}
	}
	x.mu.RUnlock()
	sort.Slice(fs, func(i, j int) bool { return fs[i].LastSeen.After(fs[j].LastSeen) })
	var out []string
	for i := 0; i < len(fs) && i < n; i++ {
		out = append(out, fs[i].Ident)
	}
	return out
}

func filterSorted[T any](m map[string]*T, keep func(*T) bool, less func(a, b *T) bool, n int) []T {
	var all []*T
	for _, v := range m {
		if keep(v) {
			all = append(all, v)
		}
	}
	sort.Slice(all, func(i, j int) bool { return less(all[i], all[j]) })
	if n > 0 && len(all) > n {
		all = all[:n]
	}
	out := make([]T, len(all))
	for i, v := range all {
		out[i] = *v
	}
	return out
}

func (x *Index) FlightsByAirline(icao string, since time.Time, n int) []Flight {
	x.mu.RLock()
	defer x.mu.RUnlock()
	return filterSorted(x.flights,
		func(f *Flight) bool { return f.Airline == icao && f.LastSeen.After(since) },
		func(a, b *Flight) bool { return flightNumLess(a.Ident, b.Ident) }, n)
}

func flightNumLess(a, b string) bool {
	if len(a) != len(b) {
		return len(a) < len(b)
	}
	return a < b
}

func (x *Index) AircraftByAirline(icao string, since time.Time, n int) []Aircraft {
	x.mu.RLock()
	defer x.mu.RUnlock()
	return filterSorted(x.aircraft,
		func(a *Aircraft) bool { return a.Airline == icao && a.LastSeen.After(since) && !a.LADD },
		func(a, b *Aircraft) bool { return a.Reg < b.Reg }, n)
}

func (x *Index) FlightsOnRoute(dep, arr string, since time.Time, n int) []Flight {
	x.mu.RLock()
	defer x.mu.RUnlock()
	return filterSorted(x.flights,
		func(f *Flight) bool { return f.Dep == dep && f.Arr == arr && f.LastSeen.After(since) },
		func(a, b *Flight) bool { return a.LastSeen.After(b.LastSeen) }, n)
}

func (x *Index) FlightsAtAirport(icao string, since time.Time, n int) []Flight {
	x.mu.RLock()
	defer x.mu.RUnlock()
	return filterSorted(x.flights,
		func(f *Flight) bool { return (f.Dep == icao || f.Arr == icao) && f.LastSeen.After(since) },
		func(a, b *Flight) bool { return a.LastSeen.After(b.LastSeen) }, n)
}

// BusiestAirports ranks airports by airborne traffic in their latest sweep.
func (x *Index) BusiestAirports(since time.Time, n int) []AirportActivity {
	x.mu.RLock()
	defer x.mu.RUnlock()
	return filterSorted(x.airports,
		func(a *AirportActivity) bool { return a.Swept.After(since) && a.Inbound+a.Outbound > 0 },
		func(a, b *AirportActivity) bool { return a.Inbound+a.Outbound > b.Inbound+b.Outbound }, n)
}

func (x *Index) AirportActivityFor(icao string) (AirportActivity, bool) {
	x.mu.RLock()
	defer x.mu.RUnlock()
	a, ok := x.airports[icao]
	if !ok {
		return AirportActivity{}, false
	}
	return *a, true
}

// AirlineCount pairs an airline with how many of its flights were seen.
type AirlineCount struct {
	Airline
	Flights int
}

func (x *Index) TopAirlines(since time.Time, n int) []AirlineCount {
	x.mu.RLock()
	counts := map[string]int{}
	for _, f := range x.flights {
		if f.Airline != "" && f.LastSeen.After(since) {
			counts[f.Airline]++
		}
	}
	var out []AirlineCount
	for icao, c := range counts {
		a, ok := x.airlines[icao]
		if !ok || a.Name == "" {
			continue
		}
		out = append(out, AirlineCount{*a, c})
	}
	x.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Flights > out[j].Flights })
	if len(out) > n {
		out = out[:n]
	}
	return out
}

type Totals struct {
	Flights24h, Aircraft24h, Airlines, Airports int
	FlightsLive                                 int
}

func (x *Index) Totals(now time.Time) Totals {
	x.mu.RLock()
	defer x.mu.RUnlock()
	var t Totals
	for _, f := range x.flights {
		if now.Sub(f.LastSeen) < 24*time.Hour {
			t.Flights24h++
		}
		if now.Sub(f.LastSeen) < 20*time.Minute {
			t.FlightsLive++
		}
	}
	for _, a := range x.aircraft {
		if now.Sub(a.LastSeen) < 24*time.Hour {
			t.Aircraft24h++
		}
	}
	for _, a := range x.airlines {
		if a.Name != "" {
			t.Airlines++
		}
	}
	for _, a := range x.airports {
		if a.Total > 0 {
			t.Airports++
		}
	}
	return t
}

// ---------- sitemap feeds ----------

type Entry struct {
	Key      string
	LastSeen time.Time
}

func (x *Index) SitemapFlights(since time.Time) []Entry {
	x.mu.RLock()
	defer x.mu.RUnlock()
	var out []Entry
	for _, f := range x.flights {
		if f.LastSeen.After(since) {
			out = append(out, Entry{f.Ident, f.LastSeen})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func (x *Index) SitemapAircraft(since time.Time) []Entry {
	x.mu.RLock()
	defer x.mu.RUnlock()
	var out []Entry
	for _, a := range x.aircraft {
		if a.LastSeen.After(since) && !a.LADD {
			out = append(out, Entry{a.Reg, a.LastSeen})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func (x *Index) SitemapAirlines(since time.Time) []Entry {
	x.mu.RLock()
	defer x.mu.RUnlock()
	var out []Entry
	for _, a := range x.airlines {
		if a.Name != "" && a.LastSeen.After(since) {
			out = append(out, Entry{a.ICAO, a.LastSeen})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// ActiveAirports lists airports seen with traffic or on a known route.
func (x *Index) ActiveAirports(since time.Time) map[string]time.Time {
	x.mu.RLock()
	defer x.mu.RUnlock()
	out := map[string]time.Time{}
	for _, a := range x.airports {
		if a.Total > 0 && a.Swept.After(since) {
			out[a.ICAO] = later(out[a.ICAO], a.Swept)
		}
	}
	for _, f := range x.flights {
		if !f.LastSeen.After(since) {
			continue
		}
		for _, c := range []string{f.Dep, f.Arr} {
			if c != "" {
				out[c] = later(out[c], f.LastSeen)
			}
		}
	}
	return out
}

// ---------- persistence ----------

type snapshot struct {
	Flights  []*Flight          `json:"flights"`
	Aircraft []*Aircraft        `json:"aircraft"`
	Airlines []*Airline         `json:"airlines"`
	Airports []*AirportActivity `json:"airports"`
	Routes   []*Route           `json:"routes,omitempty"`
}

// Prune forgets flights and aircraft not seen for a while.
func (x *Index) Prune(now time.Time) {
	x.mu.Lock()
	defer x.mu.Unlock()
	for k, f := range x.flights {
		if now.Sub(f.LastSeen) > 30*24*time.Hour {
			delete(x.flights, k)
		}
	}
	for k, a := range x.aircraft {
		if now.Sub(a.LastSeen) > 45*24*time.Hour {
			delete(x.aircraft, k)
		}
	}
	x.pruneRoutes(now)
}

// Save writes a gzip JSON snapshot atomically when something changed.
func (x *Index) Save(dir string) (err error) {
	x.saveMu.Lock()
	defer x.saveMu.Unlock()
	x.mu.Lock()
	if !x.dirty {
		x.mu.Unlock()
		return nil
	}
	var s snapshot
	for _, v := range x.flights {
		c := *v
		s.Flights = append(s.Flights, &c)
	}
	for _, v := range x.aircraft {
		c := *v
		s.Aircraft = append(s.Aircraft, &c)
	}
	for _, v := range x.airlines {
		c := *v
		s.Airlines = append(s.Airlines, &c)
	}
	for _, v := range x.airports {
		c := *v
		s.Airports = append(s.Airports, &c)
	}
	for _, m := range x.routes {
		for _, v := range m {
			c := *v
			c.Days = slices.Clone(v.Days)
			s.Routes = append(s.Routes, &c)
		}
	}
	x.dirty = false
	x.mu.Unlock()
	defer func() {
		if err != nil { // try again next time
			x.mu.Lock()
			x.dirty = true
			x.mu.Unlock()
		}
	}()

	tmp, err := os.CreateTemp(dir, "index-*.tmp")
	if err != nil {
		return err
	}
	zw := gzip.NewWriter(tmp)
	if err := json.NewEncoder(zw).Encode(s); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := zw.Close(); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), filepath.Join(dir, "index.json.gz")); err != nil {
		return err
	}
	x.mu.Lock()
	x.saved = time.Now()
	x.mu.Unlock()
	return nil
}

// Status reports the index size and when it was last written to disk.
func (x *Index) Status() map[string]any {
	x.mu.RLock()
	defer x.mu.RUnlock()
	st := map[string]any{"flights": len(x.flights), "aircraft": len(x.aircraft), "airlines": len(x.airlines), "airports": len(x.airports), "routes": x.routeCount()}
	if !x.saved.IsZero() {
		st["saved"] = x.saved.UTC().Format(time.RFC3339)
	}
	return st
}

// Load restores a snapshot; a missing file is not an error.
func (x *Index) Load(dir string) error {
	if stale, _ := filepath.Glob(filepath.Join(dir, "index-*.tmp")); len(stale) > 0 {
		for _, p := range stale {
			os.Remove(p)
		}
	}
	f, err := os.Open(filepath.Join(dir, "index.json.gz"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	var s snapshot
	if err := json.NewDecoder(zr).Decode(&s); err != nil {
		return err
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	for _, v := range s.Flights {
		x.flights[v.Ident] = v
	}
	for _, v := range s.Aircraft {
		x.aircraft[v.Reg] = v
	}
	for _, v := range s.Airlines {
		x.airlines[v.ICAO] = v
	}
	for _, v := range s.Airports {
		x.airports[v.ICAO] = v
	}
	for _, v := range s.Routes {
		x.loadRoute(v)
	}
	return nil
}
