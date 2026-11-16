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
	Airport        geo.Airport
	Code           string
	Region         string
	Country        string
	LocalTime      TimeCell
	Map            *MapV
	RadiusMi       int
	Inbound        []TrafficRow
	Outbound       []TrafficRow
	Ground         []TrafficRow
	Overhead       []TrafficRow
	Parked         []TrafficRow
	ParkedN        int
	ParkedShown    int
	GroundN        int
	GroundHasReg   bool
	GroundHasRoute bool
	Airlines       []AirlineCountV
	Nearby         []NearbyV
	Live           bool
	Summary        []string
	Facts          []Fact
	Wikipedia      string
}

type TrafficRow struct {
	Callsign, Number, URL string
	Reg, RegURL           string
	Other                 AirportV // origin for arrivals, destination for departures
	Alt                   string
	Dist                  string
	Status                string
	State                 string // on the ground: Landed, Departing
	Type                  string // parked: aircraft type from the index
	Airline               string
	Op                    string // operator ICAO code, for its logo
	distMi                float64
	pos                   geo.LL // live position, when resolved
	hdg                   float64
	direction             string
}

type AirlineCountV struct {
	ICAO, Name, URL string
	Count           int
}

type NearbyV struct {
	Code, Name, URL, Dist string
}

const airportRadius = 50

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
	v := AirportPageV{Airport: a, Code: a.Code(), Country: geo.CountryName(a.Country), RadiusMi: airportRadius, Wikipedia: a.Wiki}
	if rn := geo.RegionName(a.Region); rn != "" {
		v.Region = rn
	}
	v.LocalTime = timeCell(now, a.TZ, a.Country)

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

	for _, n := range geo.NearbyAirports(a.Lat, a.Lon, 150, 8, a.ICAO) {
		v.Nearby = append(v.Nearby, NearbyV{Code: n.Code(), Name: n.Name, URL: "/airport/" + n.ICAO,
			Dist: fmtMiles(geo.DistanceMi(a.Lat, a.Lon, n.Lat, n.Lon))})
	}

	center := geo.LL{Lat: a.Lat, Lon: a.Lon}
	close := geo.NearbyAirports(a.Lat, a.Lon, airportRadius+10, 6, a.ICAO)
	var closeV []AirportV
	for _, n := range close {
		closeV = append(closeV, airportV(n.ICAO, nil))
	}
	var planes []mapPlane
	for _, rs := range [][]TrafficRow{v.Inbound, v.Outbound, v.Overhead} {
		for _, r := range rs {
			if r.pos.Valid() {
				planes = append(planes, mapPlane{P: pt(r.pos), H: math.Round(r.hdg), L: r.Number, U: r.URL})
			}
		}
	}
	v.Map = s.placeMapV(center, a.Code(), "/airport/"+a.ICAO, []float64{25, airportRadius}, closeV, planes,
		fmt.Sprintf("Map of the area within %d miles of %s", airportRadius, a.Name),
		func() *geo.Map {
			spec := geo.MapSpec{W: 960, H: 360, Center: &center, RadiusMi: airportRadius + 6, Rings: []float64{25, airportRadius},
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
	add("Coordinates", fmt.Sprintf("%.4f, %.4f", a.Lat, a.Lon))
	if a.TZ != "" {
		add("Time zone", strings.ReplaceAll(a.TZ, "_", " "))
	}
	v.Summary = airportSummary(v)

	title := fmt.Sprintf("%s (%s) live arrivals, departures and flight tracker", a.Name, codeSlash(a))
	desc := fmt.Sprintf("Live flights at %s in %s: aircraft arriving, departing and on the ground right now, with airlines, routes and nearby airports.", a.Name, a.Place())
	p := s.basePage(http.StatusOK, title, desc, "/airport/"+a.ICAO)
	p.Section = "airports"
	p.Query = a.Code()
	p.AppURL, p.AppLabel = s.appLink("airport", a.Code()), "See "+a.Code()+" in 3D"
	p.Crumbs = []Crumb{{"Home", "/"}, {a.Name, ""}}
	p.JSONLD = append(p.JSONLD, airportLD(a, s.cfg.SiteURL))
	return s.html("airport", p, v, time.Minute)
}

func codeSlash(a geo.Airport) string {
	if a.IATA != "" {
		return a.IATA + "/" + a.ICAO
	}
	return a.ICAO
}

// classifyTraffic sorts live aircraft into the board's sections and fills in
// the far end of each route from the index, then from the API for a few.
func (s *Server) classifyTraffic(ctx context.Context, v *AirportPageV, t *api.AirportTraffic) {
	airlines := map[string]int{}
	var needRoute []*TrafficRow
	for _, x := range t.Aircraft {
		row := TrafficRow{
			Callsign: x.Callsign, Number: x.Callsign, URL: flightURL(x.Callsign), Op: opCode("", x.Callsign),
			Reg: x.Registration, RegURL: aircraftURL(x.Registration),
			Alt: fmtAlt(x.AltFt, x.OnGround), Dist: fmtMiles(x.DistanceMi), distMi: x.DistanceMi,
			Status: sentence(x.Status),
		}
		if row.Number == "" {
			row.Number = x.Registration
		}
		row.direction = x.Direction
		switch {
		case x.Status == "landed" || x.Direction == "arriving":
			row.State = "Landed"
		case x.Direction == "departing":
			row.State = "Departing"
		default:
			row.State = "On the ground"
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
		// The feed's on_ground flag lags: an aircraft a couple of hundred
		// feet above the field and next to it has landed or not yet left.
		nearField := x.DistanceMi < 3 && x.AltFt-float64(v.Airport.ElevFt) < 250
		onGround := x.OnGround || x.Status == "landed" || nearField
		if onGround && row.Alt != "On ground" {
			row.Alt = "On ground"
		}
		if f, ok := s.index.Flight(x.Callsign); ok {
			if f.IATA != "" {
				row.Number = f.IATA
			}
			switch x.Direction {
			case "arriving":
				if f.Dep != "" && f.Arr == v.Airport.ICAO {
					row.Other = airportV(f.Dep, nil)
				}
			case "departing":
				if f.Arr != "" && f.Dep == v.Airport.ICAO {
					row.Other = airportV(f.Arr, nil)
				}
			}
		}
		switch {
		case x.Direction == "transit" && !onGround:
			v.Overhead = append(v.Overhead, row)
		case x.Status == "completed":
			v.ParkedN++
			if row.RegURL != "" {
				v.Parked = append(v.Parked, row)
			}
		case onGround && live:
			if row.URL != "" || row.RegURL != "" {
				v.Ground = append(v.Ground, row)
			}
		case x.Direction == "arriving" && live:
			v.Inbound = append(v.Inbound, row)
		case x.Direction == "departing" && live:
			v.Outbound = append(v.Outbound, row)
		}
	}
	byDist := func(rs []TrafficRow) {
		sort.SliceStable(rs, func(i, j int) bool { return rs[i].distMi < rs[j].distMi })
	}
	byDist(v.Inbound)
	byDist(v.Outbound)
	byDist(v.Overhead)
	if len(v.Overhead) > 25 {
		v.Overhead = v.Overhead[:25]
	}
	for i := range v.Parked {
		if a, ok := s.index.AircraftByReg(v.Parked[i].Reg); ok {
			v.Parked[i].Type = a.Model
			if v.Parked[i].Type == "" {
				v.Parked[i].Type = a.Type
			}
			if al, ok := s.index.Airline(a.Airline); ok {
				v.Parked[i].Airline, v.Parked[i].Op = al.Name, a.Airline
			}
		}
	}
	// parked aircraft with an identity first, then by registration
	sort.SliceStable(v.Parked, func(i, j int) bool {
		if (v.Parked[i].Type != "") != (v.Parked[j].Type != "") {
			return v.Parked[i].Type != ""
		}
		return v.Parked[i].Reg < v.Parked[j].Reg
	})
	// ground rows that carry more than a flight number first; parked
	// aircraft are listed only when the index can say what they are (the
	// heading keeps the full count)
	sort.SliceStable(v.Ground, func(i, j int) bool { return groundDetail(v.Ground[i]) > groundDetail(v.Ground[j]) })
	v.GroundN = len(v.Ground)
	v.Ground = v.Ground[:min(len(v.Ground), 30)]
	for _, g := range v.Ground {
		v.GroundHasReg = v.GroundHasReg || g.RegURL != ""
		v.GroundHasRoute = v.GroundHasRoute || g.Other.Code != ""
	}
	known := v.Parked[:0]
	for _, p := range v.Parked {
		if p.Type != "" {
			known = append(known, p)
		}
	}
	v.Parked = known[:min(len(known), 30)]
	v.ParkedShown = len(v.Parked)
	// every airborne row gets a live lookup (cached, shared): its position for
	// the map, and the far end of its route when the index does not know it
	for _, rs := range [][]TrafficRow{v.Inbound, v.Outbound, v.Overhead} {
		for i := range rs {
			if rs[i].Callsign != "" {
				needRoute = append(needRoute, &rs[i])
			}
		}
	}
	s.resolveLive(ctx, v.Airport.ICAO, needRoute, 36)

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

// resolveLive looks up up to n airborne rows in parallel (cached, shared):
// each one's position for the map, its IATA number, and the far end of its
// route when the index does not know it. Results that arrive after the
// budget are ignored for this render.
func (s *Server) resolveLive(ctx context.Context, here string, rows []*TrafficRow, n int) {
	if len(rows) > n {
		rows = rows[:n]
	}
	type found struct {
		ok     bool
		number string
		other  string
		pos    geo.LL
		hdg    float64
	}
	results := make([]found, len(rows))
	var mu sync.Mutex
	closed := false
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i, row := range rows {
		callsign, reg, transit := row.Callsign, row.Reg, row.direction == "transit"
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
				f := found{ok: true, number: x.Live.IATAFlight, hdg: x.Live.Hdg}
				if p := (geo.LL{Lat: x.Live.Lat, Lon: x.Live.Lon}); p.Valid() && !x.Live.OnGround {
					f.pos = p
				}
				if !transit {
					f.other = x.Live.Dep
					if f.other == here {
						f.other = x.Live.Arr
					}
					if f.other == here {
						f.other = ""
					}
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
		if row.Other.Code == "" && f.other != "" {
			row.Other = airportV(f.other, nil)
		}
	}
}

func groundDetail(r TrafficRow) int {
	n := 0
	if r.RegURL != "" {
		n++
	}
	if r.Other.Code != "" {
		n++
	}
	return n
}

func airportSummary(v AirportPageV) []string {
	a := v.Airport
	var out []string
	kind := map[byte]string{'L': "a major airport", 'M': "an airport", 'S': "a small airport"}[a.Size]
	s := fmt.Sprintf("%s (%s) is %s serving %s", a.Name, codeSlash(a), kind, a.Place())
	if a.ElevFt != 0 {
		s += fmt.Sprintf(", at an elevation of %s ft", commas(a.ElevFt))
	}
	out = append(out, s+".")
	if v.Live {
		in, outN, g := len(v.Inbound), len(v.Outbound), v.GroundN
		if in+outN+g > 0 {
			out = append(out, fmt.Sprintf("Within %d miles right now there are %s inbound and %s outbound, with %s on the ground that just landed or are about to leave.",
				v.RadiusMi, plural(in, "flight", "flights"), plural(outN, "flight", "flights"), plural(g, "more", "more")))
		} else {
			out = append(out, fmt.Sprintf("There are no flights arriving or departing within %d miles right now.", v.RadiusMi))
		}
	}
	return out
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
