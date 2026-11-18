package store

import (
	"slices"
	"sort"
	"time"

	"github.com/mikedamm/bermuda/internal/api"
	"github.com/mikedamm/bermuda/internal/geo"
)

// Route is an airline's flights between an airport and another place, as
// the airport's boards have listed them: the days they did. A board is read
// when a page or its live refresh asks for it, so the days are a floor, not
// a timetable, and a field nobody looks at gathers none.
type Route struct {
	Airport string  `json:"a"`
	Airline string  `json:"o"` // ICAO designator
	Other   string  `json:"x"` // the other end, as the API names it
	Days    []int32 `json:"d"` // UTC days since 1970 a flight was listed on, ascending
}

// RouteDays is how many days back a listed flight keeps its route.
const RouteDays = 14

func dayOf(t time.Time) int32 { return int32(t.UTC().Unix() / 86400) }

func designator(s string) bool {
	if len(s) != 3 {
		return false
	}
	for _, c := range s {
		if c < 'A' || c > 'Z' {
			return false
		}
	}
	return true
}

// boardRoute is a board row's airline and its other end: where it comes from
// on an arrivals board, where it went (a diversion's field) or goes on a
// departures board.
func boardRoute(r api.BoardRow, arriving bool) (op api.Operator, other string) {
	switch {
	case r.Live != nil && !r.Live.LADDRedacted:
		op, other = r.Live.Operator, r.Live.Arr
		if arriving {
			other = r.Live.Dep
		}
	case r.Archived != nil && !r.Archived.LADDRedacted:
		op = r.Archived.Operator
		ref := r.Archived.Arr
		if r.Archived.ActualArr != nil {
			ref = r.Archived.ActualArr
		}
		if arriving {
			ref = r.Archived.Dep
		}
		if ref != nil {
			other = ref.ICAO
		}
	}
	if !designator(op.ICAO) {
		op = api.Operator{}
	}
	return op, placeKey(other)
}

// placeKey is one key per airport, whichever code the API used for it.
func placeKey(code string) string {
	if a, ok := geo.LookupAirport(code); ok {
		return a.ICAO
	}
	return code
}

// loadRoute puts a saved route back, under its place's key now (merging
// the days of two that turn out to be one place). The caller holds x.mu.
func (x *Index) loadRoute(v *Route) {
	if v.Airport == "" || v.Airline == "" || v.Other == "" {
		return
	}
	v.Other = placeKey(v.Other)
	m := x.routes[v.Airport]
	if m == nil {
		m = map[string]*Route{}
		x.routes[v.Airport] = m
	}
	k := v.Airline + " " + v.Other
	if cur := m[k]; cur != nil {
		for _, d := range v.Days {
			if i, found := slices.BinarySearch(cur.Days, d); !found {
				cur.Days = slices.Insert(cur.Days, i, d)
			}
		}
		return
	}
	slices.Sort(v.Days)
	m[k] = v
}

// NoteBoard records the routes an airport's board lists, each on the day of
// its flight (its board time, else at), and the names of airlines it did
// not know. arriving says the board's flights are bound here.
func (x *Index) NoteBoard(icao string, rows []api.BoardRow, arriving bool, at time.Time) {
	if icao == "" || len(rows) == 0 {
		return
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	for _, r := range rows {
		op, other := boardRoute(r, arriving)
		airline := op.ICAO
		if airline == "" || other == "" || other == icao {
			continue
		}
		if al, ok := x.airlines[airline]; !ok || al.Name == "" {
			x.noteAirline(op, at)
		}
		t := r.BoardTime
		if t.IsZero() {
			t = at
		}
		day := dayOf(t)
		m := x.routes[icao]
		if m == nil {
			m = map[string]*Route{}
			x.routes[icao] = m
		}
		k := airline + " " + other
		rt := m[k]
		if rt == nil {
			rt = &Route{Airport: icao, Airline: airline, Other: other}
			m[k] = rt
		}
		if i, found := slices.BinarySearch(rt.Days, day); !found {
			rt.Days = slices.Insert(rt.Days, i, day)
			x.dirty = true
		}
	}
}

// AirlineRoutes is an airline at an airport and the places its listed
// flights went to or came from.
type AirlineRoutes struct {
	Airline string
	Others  []string // unordered
	Days    int      // flight-days across its routes, for ordering
}

// RoutesAt is the airlines an airport's boards have listed since a time,
// the most places first.
func (x *Index) RoutesAt(icao string, since time.Time) []AirlineRoutes {
	from := dayOf(since)
	x.mu.RLock()
	by := map[string]*AirlineRoutes{}
	for _, rt := range x.routes[icao] {
		i, _ := slices.BinarySearch(rt.Days, from)
		n := len(rt.Days) - i
		if n == 0 {
			continue
		}
		a := by[rt.Airline]
		if a == nil {
			a = &AirlineRoutes{Airline: rt.Airline}
			by[rt.Airline] = a
		}
		a.Others = append(a.Others, rt.Other)
		a.Days += n
	}
	x.mu.RUnlock()
	out := make([]AirlineRoutes, 0, len(by))
	for _, a := range by {
		out = append(out, *a)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if len(a.Others) != len(b.Others) {
			return len(a.Others) > len(b.Others)
		}
		if a.Days != b.Days {
			return a.Days > b.Days
		}
		return a.Airline < b.Airline
	})
	return out
}

// pruneRoutes drops the days past the window, and routes left with none.
// The caller holds x.mu.
func (x *Index) pruneRoutes(now time.Time) {
	from := dayOf(now) - RouteDays
	for icao, m := range x.routes {
		for k, rt := range m {
			i, _ := slices.BinarySearch(rt.Days, from)
			if i > 0 {
				rt.Days = slices.Clone(rt.Days[i:])
			}
			if len(rt.Days) == 0 {
				delete(m, k)
			}
		}
		if len(m) == 0 {
			delete(x.routes, icao)
		}
	}
}

func (x *Index) routeCount() int {
	n := 0
	for _, m := range x.routes {
		n += len(m)
	}
	return n
}
