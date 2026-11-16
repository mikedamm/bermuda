package web

import (
	"bytes"
	"encoding/json"
	"html/template"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/mikedamm/bermuda/internal/geo"
)

// Page is the layout's data; Body is the page template's own view model.
type Page struct {
	Status      int
	Title       string
	Description string
	Canonical   string
	NoIndex     bool
	JSONLD      []any
	Crumbs      []Crumb
	AppURL      string // deep link into the WingView app for this page
	AppLabel    string
	Query       string // search box value
	Section     string
	Body        any
	Site        string
	Year        int
	OGImage     string
	Map         *MapAssets // set when interactive maps are on
	Updated     TimeCell   // when the data on this page was read
	Live        string     // signed refresh URL while the page's flight is under way
}

// MapAssets is what the page's script needs to draw maps.
type MapAssets struct {
	Tiles, TilesOrigin, Style, Lib, CSS, PMTiles, Glyphs string
}

func (s *Server) mapAssets() *MapAssets {
	if s.cfg.MapTiles == "" {
		return nil
	}
	return &MapAssets{
		Tiles: s.cfg.MapTiles, TilesOrigin: s.tilesOrigin,
		Style: s.staticURL("map-style.json"), Lib: s.staticURL(mapLibreJS), CSS: s.staticURL(mapLibreCSS),
		PMTiles: s.staticURL(pmtilesJS), Glyphs: Prefix + "/static/" + glyphsPath,
	}
}

type Crumb struct {
	Name, URL string
}

func (s *Server) basePage(status int, title, desc, canonicalPath string) Page {
	p := Page{
		Status: status, Title: title, Description: desc,
		AppURL: s.cfg.AppURL, AppLabel: "Open WingView 3D",
		Site: s.cfg.SiteURL, Year: time.Now().Year(),
		OGImage: s.cfg.SiteURL + s.staticURL("og-default.jpg"),
		Map:     s.mapAssets(),
		Updated: timeCell(time.Now(), "UTC", ""),
	}
	if canonicalPath != "" {
		p.Canonical = s.cfg.SiteURL + canonicalPath
	}
	if status != http.StatusOK {
		p.NoIndex = true
	}
	return p
}

func (s *Server) render(name string, p Page, body any) ([]byte, error) {
	p.Body = body
	if len(p.Crumbs) > 1 {
		p.JSONLD = append(p.JSONLD, breadcrumbLD(s.cfg.SiteURL, p.Crumbs))
	}
	var buf bytes.Buffer
	if err := s.tpl[name].ExecuteTemplate(&buf, "layout", p); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (s *Server) html(name string, p Page, body any, maxAge time.Duration) (resp, error) {
	b, err := s.render(name, p, body)
	if err != nil {
		return resp{}, err
	}
	if p.Status == 0 {
		p.Status = http.StatusOK
	}
	return resp{status: p.Status, body: b, maxAge: maxAge}, nil
}

type notFoundView struct {
	Message string
	Query   string
}

// notFound renders a real 404 (never a soft 404) with a search box.
func (s *Server) notFound(msg, query string) resp {
	p := s.basePage(http.StatusNotFound, "Not found", "", "")
	p.Query = query
	b, err := s.render("notfound", p, notFoundView{Message: msg, Query: query})
	if err != nil {
		b = []byte(msg)
	}
	return resp{status: http.StatusNotFound, body: b, maxAge: 5 * time.Minute}
}

func (s *Server) funcs() template.FuncMap {
	return template.FuncMap{
		"static": s.staticURL,
		"jsonld": func(v any) template.JS {
			b, _ := json.Marshal(v) // escapes <, >, & so </script> cannot occur
			return template.JS(b)
		},
		"commas":  commas,
		"lower":   strings.ToLower,
		"pct":     func(v float64) string { return trimFloat(v) + "%" },
		"rot":     func(v float64) string { return trimFloat(v) + "deg" },
		"svg":     func(s string) template.HTML { return template.HTML(s) }, // only for geo.RenderMap output
		"appLink": s.appLink,
		"plural":  plural,
		"add":     func(a, b int) int { return a + b },
		"country": geo.CountryName,
		"query":   url.QueryEscape,
		"legTable": func(legs []LegV, showFlight bool) any {
			return struct {
				Legs       []LegV
				ShowFlight bool
			}{legs, showFlight}
		},
		"board": func(rows []TrafficRow, other string) any {
			return struct {
				Rows  []TrafficRow
				Other string
			}{rows, other}
		},
		"band": func(class, title, detail string) StatusBand { return StatusBand{class, title, detail} },
		"timeGroups": func(depTitle string, dep []TimeRow, arrTitle string, arr []TimeRow) any {
			type group struct {
				Title string
				Rows  []TimeRow
			}
			return []group{{depTitle, dep}, {arrTitle, arr}}
		},
		"title":    titleCase,
		"date":     fmtDate,
		"barWidth": func(n, top int) string { return trimFloat(float64(n)/float64(max(top, 1))*100) + "%" },
	}
}

func trimFloat(v float64) string {
	return strconv.FormatFloat(math.Round(v*100)/100, 'f', -1, 64)
}

// appLink builds a deep link into the WingView app.
func (s *Server) appLink(kind, val string) string {
	u, _ := url.Parse(s.cfg.AppURL)
	q := url.Values{}
	switch kind {
	case "airport":
		q.Set("view", "airport")
		q.Set("airport", val)
	case "flight":
		q.Set("view", "aircraft")
		q.Set("flight_id", val)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func breadcrumbLD(site string, cs []Crumb) map[string]any {
	items := make([]map[string]any, 0, len(cs))
	for i, c := range cs {
		it := map[string]any{"@type": "ListItem", "position": i + 1, "name": c.Name}
		if c.URL != "" {
			it["item"] = site + c.URL
		}
		items = append(items, it)
	}
	return map[string]any{"@context": "https://schema.org", "@type": "BreadcrumbList", "itemListElement": items}
}
