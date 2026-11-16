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

// ---------- airline ----------

type AirlinePageV struct {
	ICAO, IATA, Name string
	Map              *MapV
	Flights          []AirlineFlightV
	Fleet            []FleetV
	FleetTypes       []TypeCountV
	Routes           int
	Summary          []string
}

type AirlineFlightV struct {
	Number, URL string
	Dep, Arr    AirportV
	Reg, RegURL string
	Seen        string
	Live        bool
}

type FleetV struct {
	Reg, URL, Type string
}

type TypeCountV struct {
	Type  string
	Count int
}

func (s *Server) airlinePage(ctx context.Context, r *http.Request) (resp, error) {
	raw := r.PathValue("code")
	code := strings.ToUpper(raw)
	al, ok := s.index.Airline(code)
	if !ok && len(code) == 2 {
		if a, ok2 := s.index.AirlineByIATA(code); ok2 {
			return redirect("/airline/" + a.ICAO), nil
		}
	}
	if !ok || al.Name == "" {
		return s.notFound("We haven't tracked an airline with the code "+code+" recently.", code), nil
	}
	if code != raw {
		return redirect("/airline/" + code), nil
	}
	now := time.Now()
	v := AirlinePageV{ICAO: al.ICAO, IATA: al.IATA, Name: al.Name}
	routes := map[string]bool{}
	for _, f := range s.index.FlightsByAirline(al.ICAO, now.Add(-7*24*time.Hour), 600) {
		row := AirlineFlightV{Number: legNumber(f.IATA, f.Ident), URL: "/flight/" + f.Ident,
			Dep: airportV(f.Dep, nil), Arr: airportV(f.Arr, nil), Reg: f.Reg, RegURL: aircraftURL(f.Reg),
			Seen: relTime(f.LastSeen, now), Live: now.Sub(f.LastSeen) < 20*time.Minute}
		if f.Dep != "" && f.Arr != "" {
			routes[f.Dep+f.Arr] = true
		}
		v.Flights = append(v.Flights, row)
	}
	types := map[string]int{}
	for _, a := range s.index.AircraftByAirline(al.ICAO, now.Add(-30*24*time.Hour), 2000) {
		v.Fleet = append(v.Fleet, FleetV{Reg: a.Reg, URL: aircraftURL(a.Reg), Type: firstNonEmpty(a.Model, a.Type)})
		if t := firstNonEmpty(a.Type, a.Model); t != "" {
			types[t]++
		}
	}
	for t, n := range types {
		v.FleetTypes = append(v.FleetTypes, TypeCountV{t, n})
	}
	sort.Slice(v.FleetTypes, func(i, j int) bool {
		if v.FleetTypes[i].Count != v.FleetTypes[j].Count {
			return v.FleetTypes[i].Count > v.FleetTypes[j].Count
		}
		return v.FleetTypes[i].Type < v.FleetTypes[j].Type
	})
	v.Routes = len(routes)
	v.Map = s.airlineMap(al.Name, s.index.FlightsByAirline(al.ICAO, now.Add(-7*24*time.Hour), 600))
	if len(v.Flights) == 0 && len(v.Fleet) == 0 {
		return s.notFound("We haven't tracked any "+al.Name+" flights in the last week.", code), nil
	}
	live := 0
	for _, f := range v.Flights {
		if f.Live {
			live++
		}
	}
	codes := al.ICAO
	if al.IATA != "" {
		codes = al.IATA + "/" + al.ICAO
	}
	v.Summary = []string{fmt.Sprintf("In the last 7 days we tracked %s operated by %s (%s) on %s, flown by %s.",
		plural(len(v.Flights), "flight number", "flight numbers"), al.Name, codes, plural(v.Routes, "route", "routes"),
		plural(len(v.Fleet), "aircraft", "aircraft"))}
	if live > 0 {
		v.Summary = append(v.Summary, fmt.Sprintf("%s in the air or on the move right now.", plural(live, "flight is", "flights are")))
	}

	p := s.basePage(http.StatusOK, fmt.Sprintf("%s (%s) flight tracker: live flights and fleet", al.Name, codes),
		fmt.Sprintf("Track %s flights live: every %s flight number we've seen this week with routes, plus the aircraft in its fleet.", al.Name, al.Name),
		"/airline/"+al.ICAO)
	p.Section = "airlines"
	p.Crumbs = []Crumb{{"Home", "/"}, {al.Name, ""}}
	ld := map[string]any{"@context": "https://schema.org", "@type": "Airline", "name": al.Name, "url": s.cfg.SiteURL + "/airline/" + al.ICAO}
	if al.IATA != "" {
		ld["iataCode"] = al.IATA
	}
	p.JSONLD = append(p.JSONLD, ld)
	return s.html("airline", p, v, 5*time.Minute)
}

// airlineMap draws the week's routes, busiest strongest.
func (s *Server) airlineMap(name string, fs []store.Flight) *MapV {
	type pair struct{ a, b string }
	counts := map[pair]int{}
	for _, f := range fs {
		if f.Dep == "" || f.Arr == "" || f.Dep == f.Arr {
			continue
		}
		k := pair{f.Dep, f.Arr}
		if k.b < k.a {
			k = pair{k.b, k.a}
		}
		counts[k]++
	}
	var routes []routeCount
	for k, n := range counts {
		a, b := airportV(k.a, nil), airportV(k.b, nil)
		if a.LL.Valid() && b.LL.Valid() {
			routes = append(routes, routeCount{a, b, n})
		}
	}
	sort.Slice(routes, func(i, j int) bool {
		if routes[i].n != routes[j].n {
			return routes[i].n > routes[j].n
		}
		return routes[i].a.ICAO+routes[i].b.ICAO < routes[j].a.ICAO+routes[j].b.ICAO
	})
	if len(routes) > 300 {
		routes = routes[:300]
	}
	return s.networkMapV(routes, nil, "Map of "+name+" routes tracked this week", func() *geo.Map {
		spec := geo.MapSpec{W: 960, H: 440}
		for _, r := range routes {
			spec.Routes = append(spec.Routes, geo.MapRoute{From: r.a.LL, To: r.b.LL, Flown: true, Weight: 0.6})
		}
		m := geo.RenderMap(spec)
		return &m
	})
}

// ---------- home ----------

type HomeV struct {
	Totals     store.Totals
	Map        *MapV
	Airports   []HomeAirportV
	Airlines   []AirlineCountV
	Examples   []FlightLink
	MaxTraffic int
}

type HomeAirportV struct {
	Code, Name, Place, URL string
	Inbound, Outbound      int
}

func (s *Server) homePage(ctx context.Context, r *http.Request) (resp, error) {
	now := time.Now()
	v := HomeV{Totals: s.index.Totals(now)}
	spec := geo.MapSpec{W: 960, H: 460}
	var pins []mapPin
	for _, a := range s.index.BusiestAirports(now.Add(-2*time.Hour), 40) {
		ap, ok := geo.LookupAirport(a.ICAO)
		if !ok {
			continue
		}
		n := a.Inbound + a.Outbound
		if len(v.Airports) < 12 {
			v.Airports = append(v.Airports, HomeAirportV{Code: ap.Code(), Name: ap.Name, Place: ap.Place(), URL: "/airport/" + ap.ICAO,
				Inbound: a.Inbound, Outbound: a.Outbound})
			v.MaxTraffic = max(v.MaxTraffic, n)
		}
		at := geo.LL{Lat: ap.Lat, Lon: ap.Lon}
		k := "minor"
		if len(pins) < 8 {
			k = "main"
		}
		pins = append(pins, mapPin{P: pt(at), L: ap.Code(), K: k, U: "/airport/" + ap.ICAO, N: n})
		if len(spec.Pins) < 24 {
			spec.Pins = append(spec.Pins, geo.MapPin{At: at, Label: ap.Code(), Primary: len(spec.Pins) < 5})
		}
	}
	for _, a := range s.index.TopAirlines(now.Add(-24*time.Hour), 16) {
		v.Airlines = append(v.Airlines, AirlineCountV{ICAO: a.ICAO, Name: a.Name, URL: "/airline/" + a.ICAO, Count: a.Flights})
	}
	// a few flights in the air right now, as search examples
	for _, a := range v.Airlines {
		fs := s.index.FlightsByAirline(a.ICAO, now.Add(-15*time.Minute), 40)
		for _, f := range fs {
			if f.Dep != "" && f.Arr != "" {
				d, ar := airportV(f.Dep, nil), airportV(f.Arr, nil)
				v.Examples = append(v.Examples, FlightLink{Number: legNumber(f.IATA, f.Ident), URL: "/flight/" + f.Ident, Route: d.Code + "–" + ar.Code})
				break
			}
		}
		if len(v.Examples) >= 6 {
			break
		}
	}
	v.Map = s.pointsMapV(pins, s.livePlanes(ctx, v.Examples), "Map of the busiest airports right now", func() *geo.Map {
		m := geo.RenderMap(spec)
		return &m
	})
	p := s.basePage(http.StatusOK, "WingView flight tracker: live flight status, aircraft and airports",
		"Track any flight live with WingView: flight status, arrival times, aircraft history and live airport activity.", Prefix+"/")
	p.Section = "home"
	p.JSONLD = append(p.JSONLD,
		map[string]any{"@context": "https://schema.org", "@type": "WebSite", "name": "WingView", "url": s.cfg.SiteURL + "/",
			"potentialAction": map[string]any{"@type": "SearchAction", "target": s.cfg.SiteURL + Prefix + "/search?q={query}", "query-input": "required name=query"}},
		map[string]any{"@context": "https://schema.org", "@type": "Organization", "name": "WingView", "url": s.cfg.SiteURL + "/",
			"logo": "https://i.wvcdn.com/brand/wingview-icon.svg"})
	return s.html("home", p, v, time.Minute)
}

// livePlanes looks up where each listed flight is now (cached lookups),
// waiting at most the live-lookup budget.
func (s *Server) livePlanes(ctx context.Context, fs []FlightLink) []mapPlane {
	var mu sync.Mutex
	closed := false
	var out []mapPlane
	var wg sync.WaitGroup
	for _, f := range fs {
		ident := strings.TrimPrefix(f.URL, "/flight/")
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := s.api.Search(ctx, ident, 5)
			if err != nil {
				return
			}
			for _, x := range res.Results {
				if x.Live != nil && x.Live.Callsign == ident && !x.Live.OnGround && !x.Live.LADDRedacted {
					if p := (geo.LL{Lat: x.Live.Lat, Lon: x.Live.Lon}); p.Valid() {
						mu.Lock()
						if !closed {
							out = append(out, mapPlane{P: pt(p), H: math.Round(x.Live.Hdg), L: f.Number, U: f.URL})
						}
						mu.Unlock()
					}
					return
				}
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
	planes := append([]mapPlane(nil), out...)
	mu.Unlock()
	sort.Slice(planes, func(i, j int) bool { return planes[i].L < planes[j].L })
	return planes
}

// ---------- search ----------

type SearchV struct {
	Query   string
	Results []SearchRowV
}

type SearchRowV struct {
	Title, URL, Detail string
}

// searchPage resolves what someone typed. One clear answer redirects; an
// ambiguous one lists the candidates.
func (s *Server) searchPage(ctx context.Context, r *http.Request) (resp, error) {
	q := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("q")))
	q = strings.Join(strings.Fields(q), "")
	p := s.basePage(http.StatusOK, "Search", "", "")
	p.NoIndex = true
	p.Query = q
	if q == "" {
		return s.html("search", p, SearchV{}, time.Minute)
	}
	// results below depend on the query string: never let a shared cache keep them
	// airport codes first: three letters (IATA) or four (ICAO) that we know
	if a, ok := geo.LookupAirport(q); ok && (len(q) == 3 || (len(q) == 4 && a.ICAO == q)) {
		res, err := s.api.Search(ctx, q, 5)
		if err != nil || len(res.Results) == 0 {
			return redirect302("/airport/" + a.ICAO), nil
		}
	}
	res, err := s.api.Search(ctx, q, 20)
	if err != nil && !errors.Is(err, api.ErrBadRequest) {
		return resp{}, err
	}
	var rows []SearchRowV
	if res != nil {
		if c := canonicalCallsign(res); c != "" {
			return redirect302("/flight/" + c), nil
		}
		for _, x := range res.Results {
			if x.Registration == "" {
				continue
			}
			rows = append(rows, SearchRowV{Title: x.Registration, URL: "/aircraft/" + x.Registration,
				Detail: strings.TrimSpace(x.Name + " " + titleCase(x.OwnerName))})
		}
	}
	if a, ok := geo.LookupAirport(q); ok {
		rows = append(rows, SearchRowV{Title: a.Code() + " " + a.Name, URL: "/airport/" + a.ICAO, Detail: a.Place()})
	}
	if len(rows) == 1 {
		return redirect302(rows[0].URL), nil
	}
	if len(rows) == 0 {
		if al, ok := s.index.Airline(q); ok && al.Name != "" {
			return redirect302("/airline/" + al.ICAO), nil
		}
		nf := s.notFound("Nothing matched "+q+". Try a flight number like UA1275, a registration like N527AS, or an airport code like SEA.", q)
		nf.maxAge = 0
		return nf, nil
	}
	return s.html("search", p, SearchV{Query: q, Results: rows}, 0)
}

// redirect302 answers a search. It is never stored by a shared cache: a CDN
// keying without the query string would send every search to the first answer.
func redirect302(to string) resp {
	return resp{status: http.StatusFound, redirect: to}
}
