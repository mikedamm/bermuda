package web

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"time"

	"github.com/mikedamm/bermuda/internal/geo"
	"github.com/mikedamm/bermuda/internal/store"
)

const sitemapChunk = 40000

func (s *Server) robots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	if !s.canonicalHost(r) {
		fmt.Fprint(w, "User-agent: *\nDisallow: /\n")
		return
	}
	fmt.Fprintf(w, "User-agent: *\nAllow: /\nDisallow: %s/search\nDisallow: %s/live/\n\nSitemap: %s%s/sitemap.xml\n", Prefix, Prefix, s.cfg.SiteURL, Prefix)
}

type urlset struct {
	XMLName xml.Name `xml:"urlset"`
	NS      string   `xml:"xmlns,attr"`
	URLs    []smURL  `xml:"url"`
}

type smURL struct {
	Loc     string `xml:"loc"`
	LastMod string `xml:"lastmod,omitempty"`
}

type smIndex struct {
	XMLName  xml.Name `xml:"sitemapindex"`
	NS       string   `xml:"xmlns,attr"`
	Sitemaps []smURL  `xml:"sitemap"`
}

func xmlResp(v any) (resp, error) {
	b, err := xml.Marshal(v)
	if err != nil {
		return resp{}, err
	}
	return resp{status: http.StatusOK, body: append([]byte(xml.Header), b...), ctype: "application/xml; charset=utf-8", maxAge: time.Hour}, nil
}

func day(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format("2006-01-02")
}

func (s *Server) sitemapIndex(ctx context.Context, r *http.Request) (resp, error) {
	now := time.Now()
	idx := smIndex{NS: "http://www.sitemaps.org/schemas/sitemap/0.9"}
	add := func(name string) {
		idx.Sitemaps = append(idx.Sitemaps, smURL{Loc: s.cfg.SiteURL + Prefix + "/sitemaps/" + name, LastMod: day(now)})
	}
	add("pages.xml")
	add("airports.xml")
	for i := range chunks(len(s.index.SitemapFlights(now.Add(-14 * 24 * time.Hour)))) {
		add("flights-" + strconv.Itoa(i+1) + ".xml")
	}
	for i := range chunks(len(s.index.SitemapAircraft(now.Add(-30 * 24 * time.Hour)))) {
		add("aircraft-" + strconv.Itoa(i+1) + ".xml")
	}
	return xmlResp(idx)
}

func chunks(n int) int {
	if n == 0 {
		return 0
	}
	return (n + sitemapChunk - 1) / sitemapChunk
}

var sitemapName = regexp.MustCompile(`^(pages|airports|flights|aircraft)(?:-(\d+))?\.xml$`)

func (s *Server) sitemapFile(ctx context.Context, r *http.Request) (resp, error) {
	m := sitemapName.FindStringSubmatch(r.PathValue("name"))
	if m == nil {
		return s.notFound("No such sitemap.", ""), nil
	}
	now := time.Now()
	set := urlset{NS: "http://www.sitemaps.org/schemas/sitemap/0.9"}
	add := func(path string, t time.Time) {
		set.URLs = append(set.URLs, smURL{Loc: s.cfg.SiteURL + path, LastMod: day(t)})
	}
	page := func(es []store.Entry, prefix string) bool {
		n, _ := strconv.Atoi(m[2])
		if n < 1 || (n-1)*sitemapChunk >= len(es) {
			return false
		}
		es = es[(n-1)*sitemapChunk : min(n*sitemapChunk, len(es))]
		for _, e := range es {
			add(prefix+e.Key, e.LastSeen)
		}
		return true
	}
	switch m[1] {
	case "pages":
		add(Prefix+"/", now)
		for _, e := range s.index.SitemapAirlines(now.Add(-14 * 24 * time.Hour)) {
			add("/airline/"+e.Key, e.LastSeen)
		}
	case "airports":
		// airports with scheduled service or traffic we have seen: pages with
		// something to say, not every grass strip in the reference data
		active := s.index.ActiveAirports(now.Add(-14 * 24 * time.Hour))
		seen := map[string]bool{}
		var codes []string
		for code := range active {
			if a, ok := geo.LookupAirport(code); ok && !seen[a.ICAO] {
				seen[a.ICAO] = true
				codes = append(codes, a.ICAO)
			}
		}
		for _, a := range geo.LargeAirports() {
			if !seen[a.ICAO] {
				seen[a.ICAO] = true
				codes = append(codes, a.ICAO)
			}
		}
		sort.Strings(codes)
		for _, c := range codes {
			add("/airport/"+c, active[c])
		}
	case "flights":
		if !page(s.index.SitemapFlights(now.Add(-14*24*time.Hour)), "/flight/") {
			return s.notFound("No such sitemap.", ""), nil
		}
	case "aircraft":
		if !page(s.index.SitemapAircraft(now.Add(-30*24*time.Hour)), "/aircraft/") {
			return s.notFound("No such sitemap.", ""), nil
		}
	}
	return xmlResp(set)
}
