package web

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/mikedamm/bermuda/internal/api"
	"github.com/mikedamm/bermuda/internal/geo"
)

type AircraftPageV struct {
	Key          string
	Registration string
	Hex          string
	Name         string // Boeing 737-890
	Operator     string
	OperatorURL  string
	OperatorICAO string
	Owner        string
	OwnerPlace   string
	Status       string
	StatusClass  string
	StatusDetail string
	Band         StatusBand
	LastSeen     []Fact // where and when it was last seen, when not flying
	Live         *LegV
	LiveURL      string
	Map          *MapV
	Specs        []Fact
	Registry     []Fact
	Stats        []Fact
	TopAirports  []AirportCountV
	MaxCount     int
	Recent       []LegV
	Upcoming     []LegV
	History      []api.RegistryVersion
	LADD         bool
	Days         int
	Summary      []string
}

type AirportCountV struct {
	Airport AirportV
	Count   int
}

var aircraftKeyClean = regexp.MustCompile(`[^A-Z0-9-]`)

func (s *Server) aircraftPage(ctx context.Context, r *http.Request) (resp, error) {
	raw := r.PathValue("key")
	key := aircraftKeyClean.ReplaceAllString(strings.ToUpper(raw), "")
	if key == "" || len(key) > 36 {
		return s.notFound("That doesn't look like a registration or Mode S address.", raw), nil
	}
	p0, err := s.api.Aircraft(ctx, key, 30, 50)
	if errors.Is(err, api.ErrNotFound) || errors.Is(err, api.ErrBadRequest) {
		return s.notFound("No aircraft with registration or address "+key+" is on record.", key), nil
	}
	if err != nil {
		return resp{}, err
	}
	canon := p0.Aircraft.Registration
	if canon == "" {
		canon = p0.Aircraft.Hex
	}
	if canon == "" {
		canon = key
	}
	if canon != raw {
		return redirect("/aircraft/" + canon), nil
	}
	s.index.NoteAircraftPage(p0)

	now := time.Now()
	a := p0.Aircraft
	v := AircraftPageV{
		Key: canon, Registration: a.Registration, Hex: a.Hex, Name: aircraftName(a),
		LADD: p0.LADD.Listed, Days: p0.Days,
	}
	if v.Registration == "" {
		v.Registration = a.Hex
	}
	if p0.Operator.Name != "" {
		v.Operator = titleCase(p0.Operator.Name)
		if p0.Operator.ICAO != "" {
			v.OperatorURL = "/airline/" + p0.Operator.ICAO
			v.OperatorICAO = p0.Operator.ICAO
		}
	}
	if o := p0.Owner; o != nil && o.Name != "" {
		v.Owner = titleCase(o.Name)
		v.OwnerPlace = strings.Join(nonEmpty(titleCase(o.City), o.State, geo.CountryName(o.Country)), ", ")
	}

	// Redaction is decided once: everything below reads these filtered lists,
	// never the raw answer.
	recent, upcoming := visibleFlights(p0)
	rf := refs{}
	rf.addHistory(recent)
	for _, ac := range p0.Stats.Airports {
		rf.add(ac.Airport)
	}
	if p0.Status.Last != nil {
		rf.add(p0.Status.Last.Airport)
	}

	// status line. LADD aircraft show identity and owner only: whatever the
	// API returns, no position, map, route or flight leaves this page.
	st := p0.Status
	switch {
	case v.LADD:
		v.Status, v.StatusClass = "Tracking limited", "muted"
		v.StatusDetail = "Flight details withheld at the owner's request"
	case st.Flight != nil && !st.Flight.LADDRedacted:
		l := legFromLive(*st.Flight, rf, now)
		v.Live = &l
		v.LiveURL = l.URL
		v.Status, v.StatusClass = l.PhaseWord, l.StatusClass
		if v.Status == "" {
			v.Status = "Flying"
		}
		v.StatusDetail = l.Headline
		if l.Dep.Code != "" && l.Arr.Code != "" {
			v.StatusDetail = fmt.Sprintf("%s, %s to %s", l.Headline, l.Dep.City, l.Arr.City)
		}
	case st.Last != nil && st.Flight != nil && st.Flight.LADDRedacted:
		v.Status, v.StatusClass = "Tracking limited", "muted"
		v.StatusDetail = "Position withheld at the owner's request"
	case st.Last != nil:
		v.Status, v.StatusClass = "On the ground", "landed"
		if !st.Last.OnGround {
			v.Status, v.StatusClass = "Not tracked now", "muted"
		}
		at := relTime(st.Last.At, now)
		if st.Last.Airport != nil && st.Last.OnGround {
			ap := airportV("", st.Last.Airport)
			v.StatusDetail = fmt.Sprintf("Last seen at %s (%s), %s", ap.Name, ap.Code, at)
			v.Band = StatusBand{"idle", "On the ground", "At " + ap.Code + ", " + at}
			v.LastSeen = append(v.LastSeen, Fact{Label: "Last seen at", Value: ap.Name, URL: ap.URL})
		} else {
			v.StatusDetail = "Last position reported " + at
			v.Band = StatusBand{"idle", "Not tracked now", "Last position " + at}
		}
		v.LastSeen = append(v.LastSeen, Fact{Label: "Last position", Value: at})
	default:
		v.Status, v.StatusClass = "No recent activity", "muted"
		v.StatusDetail = fmt.Sprintf("No positions on record in the last %d days", p0.Days)
	}

	switch {
	case v.Live != nil:
		v.Band = v.Live.Band()
	case v.Band.Title == "":
		v.Band = StatusBand{"idle", v.Status, ""}
	}
	v.History = p0.RegistryHistory
	for _, h := range recent {
		v.Recent = append(v.Recent, legFromHistory(h, now))
	}
	for _, u := range upcoming {
		v.Upcoming = append(v.Upcoming, legFromLive(u, rf, now))
	}
	if !v.LADD {
		// network map: every route of the window, the busiest drawn strongest
		if v.Live != nil {
			v.Map = s.legMapV(*v.Live, s.legShape(ctx, v.Live.FlightID), "Map of "+v.Registration+"'s current flight")
		} else {
			var plane *mapPlane
			if last := p0.Status.Last; last != nil && (st.Flight == nil || !st.Flight.LADDRedacted) {
				if pos := (geo.LL{Lat: last.Lat, Lon: last.Lon}); pos.Valid() {
					plane = &mapPlane{P: pt(pos), H: math.Round(last.Hdg)}
				}
			}
			v.Map = s.networkMapV(aircraftRoutes(recent, rf), plane,
				fmt.Sprintf("Map of the routes %s flew in the last %d days", v.Registration, p0.Days),
				func() *geo.Map { return aircraftMap(p0, recent, v, rf) })
		}
	}

	// facts
	add := func(fs *[]Fact, label, val string) {
		if val != "" && val != "0" {
			*fs = append(*fs, Fact{Label: label, Value: val})
		}
	}
	// the model is in the title band and the opening sentence; the type code
	// says only what the engines row does not (a helicopter, a seaplane)
	if a.ICAOType != "" {
		code := a.ICAOType
		if k := typeKind(a.TypeDescription); k != "" && k != "landplane" {
			code += ", " + k
		}
		add(&v.Specs, "Type code", code)
	}
	add(&v.Specs, "Engines", engines(a.Engines, a.EngineType))
	if a.Seats > 0 {
		add(&v.Specs, "Seats", fmt.Sprint(a.Seats))
	}
	if a.YearBuilt > 0 {
		age := ""
		if a.AgeYears > 0 {
			age = fmt.Sprintf(" (%d years old)", a.AgeYears)
		}
		add(&v.Specs, "Built", fmt.Sprintf("%d%s", a.YearBuilt, age))
	}
	add(&v.Specs, "Serial number", a.SerialNumber)
	add(&v.Specs, "Registered in", a.Country.Name)
	if a.Wake != "" {
		add(&v.Specs, "Wake category", map[string]string{"L": "Light", "M": "Medium", "H": "Heavy", "J": "Super"}[a.Wake])
	}

	if reg := a.Registry; reg != nil {
		add(&v.Registry, "Registered owner", v.Owner)
		add(&v.Registry, "Owner location", v.OwnerPlace)
		add(&v.Registry, "Registrant type", sentence(reg.RegistrantType))
		add(&v.Registry, "Certificate issued", fmtDate(reg.Registered))
		add(&v.Registry, "Registration status", sentence(reg.Status))
		add(&v.Registry, "Category", sentence(reg.Category))
		add(&v.Registry, "Engine", sentence(reg.EngineType))
	} else if v.Owner != "" {
		add(&v.Registry, "Registered owner", v.Owner)
		add(&v.Registry, "Owner location", v.OwnerPlace)
	}

	stt := p0.Stats
	if !v.LADD {
		add(&v.Stats, "Flights", commas(stt.Flights))
		if stt.Hours > 0 {
			add(&v.Stats, "Hours flown", fmt.Sprintf("%.1f", stt.Hours))
		}
		if stt.Diverted > 0 {
			add(&v.Stats, "Diversions", fmt.Sprint(stt.Diverted))
		}
		for _, ac := range stt.Airports {
			av := airportV(ac.Code, ac.Airport)
			v.TopAirports = append(v.TopAirports, AirportCountV{av, ac.Count})
			v.MaxCount = max(v.MaxCount, ac.Count)
		}
	}
	v.Summary = aircraftSummary(v, p0)

	title := v.Registration
	if v.Name != "" {
		title += " " + v.Name
	}
	if v.Operator != "" {
		title += ", " + v.Operator
	}
	title += ": flight history and owner"
	desc := fmt.Sprintf("%s is a %s", v.Registration, firstNonEmpty(v.Name, "aircraft"))
	if v.Operator != "" {
		desc += " flown by " + v.Operator
	}
	desc += fmt.Sprintf(". Live position, the last %d days of flights, registration and ownership records.", p0.Days)

	p := s.basePage(http.StatusOK, title, desc, "/aircraft/"+canon)
	p.Section = "aircraft"
	p.Query = v.Registration
	p.NoIndex = v.LADD // respect the owner's request to limit tracking
	if v.Live != nil && !v.LADD {
		p.AppURL, p.AppLabel = s.appLink("flight", v.Live.FlightID), "Follow this aircraft live"
	}
	p.Crumbs = []Crumb{{"Home", "/"}}
	if v.OperatorURL != "" {
		p.Crumbs = append(p.Crumbs, Crumb{v.Operator, v.OperatorURL})
	}
	p.Crumbs = append(p.Crumbs, Crumb{v.Registration, ""})
	maxAge := 2 * time.Minute
	if v.Live == nil {
		maxAge = 5 * time.Minute
	}
	return s.html("aircraft", p, v, maxAge)
}

// visibleFlights drops everything LADD withholds: all of it for a listed
// aircraft, and any individually redacted row otherwise.
func visibleFlights(p0 *api.AircraftPage) (recent []api.HistoryEntry, upcoming []api.FlightSummary) {
	if p0.LADD.Listed {
		return nil, nil
	}
	for _, h := range p0.RecentFlights {
		if !h.LADDRedacted {
			recent = append(recent, h)
		}
	}
	for _, u := range p0.Status.Upcoming {
		if !u.LADDRedacted {
			upcoming = append(upcoming, u)
		}
	}
	return recent, upcoming
}

// aircraftRoutes counts each airport pair flown, in a stable order.
func aircraftRoutes(recent []api.HistoryEntry, rf refs) []routeCount {
	type pair struct{ a, b string }
	counts := map[pair]int{}
	for _, h := range recent {
		if h.Dep == nil || h.Arr == nil || h.Dep.ICAO == h.Arr.ICAO {
			continue
		}
		k := pair{h.Dep.ICAO, h.Arr.ICAO}
		if k.b < k.a {
			k = pair{k.b, k.a}
		}
		counts[k]++
	}
	var out []routeCount
	for k, n := range counts {
		a, b := rf.airport(k.a), rf.airport(k.b)
		if a.LL.Valid() && b.LL.Valid() {
			out = append(out, routeCount{a, b, n})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].n != out[j].n {
			return out[i].n > out[j].n
		}
		return out[i].a.ICAO+out[i].b.ICAO < out[j].a.ICAO+out[j].b.ICAO
	})
	return out
}

func aircraftMap(p0 *api.AircraftPage, recent []api.HistoryEntry, v AircraftPageV, rf refs) *geo.Map {
	spec := geo.MapSpec{W: 960, H: 400}
	if v.Live != nil {
		m := legMap(*v.Live, 960, 400)
		return m
	}
	type pair struct{ a, b string }
	counts := map[pair]int{}
	maxN := 0
	for _, h := range recent {
		if h.Dep == nil || h.Arr == nil || h.Dep.ICAO == h.Arr.ICAO {
			continue
		}
		k := pair{h.Dep.ICAO, h.Arr.ICAO}
		if k.b < k.a {
			k = pair{k.b, k.a}
		}
		counts[k]++
		maxN = max(maxN, counts[k])
	}
	uses := map[string]int{}
	pinAt := map[string]AirportV{}
	for k, n := range counts {
		a, b := rf.airport(k.a), rf.airport(k.b)
		if !a.LL.Valid() || !b.LL.Valid() {
			continue
		}
		spec.Routes = append(spec.Routes, geo.MapRoute{From: a.LL, To: b.LL, Flown: true, Weight: float64(n) / float64(maxN)})
		for _, ap := range []AirportV{a, b} {
			uses[ap.ICAO] += n
			pinAt[ap.ICAO] = ap
		}
	}
	// busiest first, so they win label space
	codes := make([]string, 0, len(pinAt))
	for c := range pinAt {
		codes = append(codes, c)
	}
	sort.Slice(codes, func(i, j int) bool {
		if uses[codes[i]] != uses[codes[j]] {
			return uses[codes[i]] > uses[codes[j]]
		}
		return codes[i] < codes[j]
	})
	for _, c := range codes {
		spec.Pins = append(spec.Pins, geo.MapPin{At: pinAt[c].LL, Label: pinAt[c].Code})
	}
	// routes in a stable order so the SVG (and the CDN's copy) is deterministic
	sort.Slice(spec.Routes, func(i, j int) bool {
		a, b := spec.Routes[i], spec.Routes[j]
		if a.From.Lat != b.From.Lat {
			return a.From.Lat < b.From.Lat
		}
		return a.To.Lat < b.To.Lat
	})
	if last := p0.Status.Last; last != nil && (p0.Status.Flight == nil || !p0.Status.Flight.LADDRedacted) {
		pos := geo.LL{Lat: last.Lat, Lon: last.Lon}
		if pos.Valid() {
			spec.Plane, spec.PlaneHdg = &pos, last.Hdg
		}
	}
	if len(spec.Routes) == 0 && spec.Plane == nil {
		return nil
	}
	if len(spec.Routes) == 0 {
		spec.Center, spec.RadiusMi = spec.Plane, 60
	}
	m := geo.RenderMap(spec)
	return &m
}

func aircraftName(a api.Aircraft) string {
	if a.Name != "" {
		return a.Name
	}
	return strings.TrimSpace(a.Manufacturer + " " + a.Model)
}

// engines reads "Twin jet", "Single piston", "4 turboprops".
func engines(n int, typ string) string {
	typ = strings.ToLower(strings.TrimSpace(strings.ReplaceAll(typ, "_", " ")))
	switch {
	case n <= 0:
		return sentence(typ)
	case typ == "":
		return fmt.Sprint(n)
	case n == 1:
		return "Single " + typ
	case n == 2:
		return "Twin " + typ
	}
	return fmt.Sprintf("%d %ss", n, typ)
}

// typeKind is the airframe kind in an ICAO type description (L2J: landplane).
func typeKind(code string) string {
	if code == "" {
		return ""
	}
	return map[byte]string{'L': "landplane", 'S': "seaplane", 'A': "amphibian", 'H': "helicopter", 'G': "gyrocopter", 'T': "tiltrotor"}[code[0]]
}

func sentence(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "_", " "))
	if s == "" {
		return ""
	}
	s = strings.ToLower(s)
	return strings.ToUpper(s[:1]) + s[1:]
}

func nonEmpty(ss ...string) []string {
	var out []string
	for _, s := range ss {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// aircraftSummary is the one sentence that opens the about panel. Its
// flying record has its own panel, so it says only what the aircraft is
// and who flies it (and, for a LADD aircraft, why its flights are hidden).
func aircraftSummary(v AircraftPageV, p0 *api.AircraftPage) []string {
	what := firstNonEmpty(v.Name, "aircraft")
	if y := p0.Aircraft.YearBuilt; y > 0 {
		what = fmt.Sprintf("%d %s", y, what)
	}
	s := fmt.Sprintf("%s is %s %s", v.Registration, article(what), what)
	if v.Operator != "" {
		s += " flown by " + v.Operator
	}
	out := []string{s + "."}
	if v.LADD {
		out = append(out, "Its owner has opted out of public tracking (the FAA's LADD program), so its flights are not shown.")
	}
	return out
}

// article is "a" or "an" for a word or year ("an 1987", "an A320", "a 2026").
func article(w string) string {
	w = strings.ToLower(w)
	for _, p := range []string{"8", "11", "18", "a", "e", "i", "o"} {
		if strings.HasPrefix(w, p) && !strings.HasPrefix(w, "one") {
			return "an"
		}
	}
	return "a"
}
