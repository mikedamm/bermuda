package web

import (
	"net/http"
	"sort"
	"strconv"
)

// The Flights panel on flight and aircraft pages: one list, newest first.
// Flights still to come sit on top, grayed and Scheduled; the flight under
// way (else the latest one flown) is highlighted; the past runs down from
// it, the whole history window, a page at a time.

const flightsPerPage = 25

type FlightsV struct {
	Rows       []FlightRowV
	ShowFlight bool // a Flight column (aircraft pages); else an Aircraft column
	Days       int
	Page       int
	Pages      int
	First      int // the rows shown, counted from 1, of Total
	Last       int
	Total      int
	Newer      string // the neighbouring pages' URLs, empty at either end
	Older      string
	Links      []PageLink
}

type PageLink struct {
	N       int
	URL     string
	Current bool
	Gap     bool // pages are skipped before this one
}

type FlightRowV struct {
	LegV
	When   string   // future, current or past
	DepAt  TimeCell // what happened, else what is planned
	ArrAt  TimeCell
	ArrEst bool // ArrAt is an estimate (the current flight, still to land)
}

// flightRows orders future, current and past flights into one list,
// latest first. current may be nil.
func flightRows(future []LegV, current *LegV, past []LegV) []FlightRowV {
	latestFirst := func(ls []LegV) []LegV {
		out := append([]LegV(nil), ls...)
		sort.SliceStable(out, func(i, j int) bool { return out[i].sort.After(out[j].sort) })
		return out
	}
	var rows []FlightRowV
	for _, l := range latestFirst(future) {
		l.Phase, l.PhaseWord, l.StatusClass = "scheduled", "Scheduled", "sched"
		rows = append(rows, FlightRowV{LegV: l, When: "future", DepAt: l.DepPlanned, ArrAt: firstCell(l.ArrPlanned, l.ArrEst)})
	}
	flown := func(l LegV, when string) FlightRowV {
		r := FlightRowV{LegV: l, When: when, DepAt: firstCell(l.DepOff, l.DepOut), ArrAt: firstCell(l.ArrOn, l.ArrIn)}
		if when == "current" {
			if !r.DepAt.Set {
				r.DepAt = l.DepPlanned
			}
			if !r.ArrAt.Set {
				r.ArrAt = firstCell(l.ArrEst, l.ArrPlanned)
				r.ArrEst = r.ArrAt.Set
			}
		}
		return r
	}
	if current != nil {
		rows = append(rows, flown(*current, "current"))
	}
	for _, l := range latestFirst(past) {
		rows = append(rows, flown(l, "past"))
	}
	return rows
}

// pageFlights cuts the page asked for (?page=N) out of the rows. Page 1 is
// the bare URL. A page number that is not one, or names page 1, answers
// with the bare URL to redirect to; a page past the end, with the last
// page's (flights age out of the window, so old links run short).
func pageFlights(r *http.Request, rows []FlightRowV, showFlight bool, days int) (FlightsV, string) {
	pages := max(1, (len(rows)+flightsPerPage-1)/flightsPerPage)
	at := func(n int) string {
		if n == 1 {
			return r.URL.Path
		}
		return r.URL.Path + "?page=" + strconv.Itoa(n)
	}
	page := 1
	if q := r.URL.Query(); q.Has("page") {
		n, err := strconv.Atoi(q.Get("page"))
		switch {
		case err != nil || n < 2:
			return FlightsV{}, at(1)
		case n > pages:
			return FlightsV{}, at(pages)
		}
		page = n
	}
	from, to := (page-1)*flightsPerPage, min(page*flightsPerPage, len(rows))
	v := FlightsV{Rows: rows[from:to], ShowFlight: showFlight, Days: days, Page: page, Pages: pages,
		First: from + 1, Last: to, Total: len(rows)}
	if page > 1 {
		v.Newer = at(page-1) + "#flights"
	}
	if page < pages {
		v.Older = at(page+1) + "#flights"
	}
	// every page near this one, the first and the last; gaps between
	for n := 1; n <= pages; n++ {
		if n == 1 || n == pages || (n >= page-2 && n <= page+2) {
			gap := len(v.Links) > 0 && v.Links[len(v.Links)-1].N != n-1
			v.Links = append(v.Links, PageLink{N: n, URL: at(n) + "#flights", Current: n == page, Gap: gap})
		}
	}
	return v, ""
}
