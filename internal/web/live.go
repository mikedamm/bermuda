package web

// The live refresh. A flight page showing a flight that is still under way,
// and every airport page, carries a URL to this endpoint; its script
// (live.js) polls it every 30 seconds and swaps in the parts of the page
// that change: a flight's status, dock, times and track; an airport's
// arrivals and departures boards.
//
// What keeps it from being worth scraping:
//   - the URL is signed and expires. It is bound to one flight or one airport
//     and handed out only by that page, so there is nothing to enumerate: no
//     callsigns, no registrations, no lists.
//   - the token is a path segment, so a CDN that leaves query strings out of
//     its cache key still cannot hand a signed answer to an unsigned URL.
//   - an answer is the page's own HTML for the parts that change, plus the
//     map's moving pieces: nothing the page does not already show.
//   - answers are rendered per poll and never stored by a browser or the
//     CDN. A flight's comes from one real-time lookup (never cached); an
//     airport's from its boards, which the API client keeps for 60 seconds.
//   - no CORS, a same-origin resource policy, noindex, and robots.txt.

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mikedamm/bermuda/internal/api"
	"github.com/mikedamm/bermuda/internal/geo"
)

const (
	// liveTokenLife is how long a signed URL works. Every answer carries a
	// renewed one, so a page left open on a long flight never runs out.
	liveTokenLife = 6 * time.Hour
	jsonType      = "application/json"
)

var flightIDRe = regexp.MustCompile(`^[A-Za-z0-9-]{4,64}$`)

// liveParts are the regions of the flight page the refresh replaces; each
// is a template named flight-<part> in flight.html.
var liveParts = []string{"status", "dock", "times", "track", "bar"}

// airportParts are the airport page's: templates airport-<part> in airport.html.
var airportParts = []string{"arrivals", "departures"}

// What a signature is bound to. Each kind signs under its own label, so a
// token for a flight can never pass for an airport whose code reads the same.
const (
	liveFlightKind  = "live"
	liveAirportKind = "airport"
)

// liveURL signs a refresh URL for one flight. Expiry is rounded to the hour,
// so every page rendered in the same hour carries the same URL.
func (s *Server) liveURL(flightID string, now time.Time) string {
	exp := liveExpiry(now)
	return Prefix + "/live/" + flightID + "/" + exp + "." + s.liveSig(liveFlightKind, flightID, exp)
}

// liveAirportURL signs a refresh URL for one airport's boards.
func (s *Server) liveAirportURL(icao string, now time.Time) string {
	exp := liveExpiry(now)
	return Prefix + "/live/airport/" + icao + "/" + exp + "." + s.liveSig(liveAirportKind, icao, exp)
}

func liveExpiry(now time.Time) string {
	return strconv.FormatInt(now.Truncate(time.Hour).Add(liveTokenLife).Unix(), 36)
}

func (s *Server) liveSig(kind, id, exp string) string {
	m := hmac.New(sha256.New, s.cfg.LiveKey)
	m.Write([]byte(kind + "\x00" + id + "\x00" + exp))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil)[:16])
}

func (s *Server) liveValid(kind, id, token string, now time.Time) bool {
	exp, sig, ok := strings.Cut(token, ".")
	if !ok {
		return false
	}
	t, err := strconv.ParseInt(exp, 36, 64)
	if err != nil || now.Unix() >= t || t > now.Add(liveTokenLife+time.Hour).Unix() {
		return false
	}
	return hmac.Equal([]byte(sig), []byte(s.liveSig(kind, id, exp)))
}

type liveAnswer struct {
	Parts map[string]string `json:"parts,omitempty"` // part name → its HTML
	Map   *mapData          `json:"map,omitempty"`
	Next  string            `json:"next,omitempty"` // the URL to poll next (renewed signature)
	Done  bool              `json:"done,omitempty"` // the flight is over: stop polling
}

func (s *Server) liveFlight(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("X-Robots-Tag", "noindex")
	h.Set("Cross-Origin-Resource-Policy", "same-origin")
	id, now := r.PathValue("id"), time.Now()
	if !flightIDRe.MatchString(id) || !s.liveValid(liveFlightKind, id, r.PathValue("token"), now) {
		s.write(w, r, resp{status: http.StatusForbidden, body: []byte(`{"error":"expired"}`), ctype: jsonType})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	a, err := s.liveFlightAnswer(ctx, id, now)
	s.writeLive(w, r, a, err, "flight", id)
}

// liveAirport answers an airport page's poll with its two boards.
func (s *Server) liveAirport(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("X-Robots-Tag", "noindex")
	h.Set("Cross-Origin-Resource-Policy", "same-origin")
	code, now := r.PathValue("code"), time.Now()
	a, ok := geo.LookupAirport(code)
	if !ok || a.ICAO != code || !s.liveValid(liveAirportKind, code, r.PathValue("token"), now) {
		s.write(w, r, resp{status: http.StatusForbidden, body: []byte(`{"error":"expired"}`), ctype: jsonType})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	ans, err := s.liveAirportAnswer(ctx, a, now)
	s.writeLive(w, r, ans, err, "airport", code)
}

func (s *Server) liveAirportAnswer(ctx context.Context, a geo.Airport, now time.Time) (liveAnswer, error) {
	v := AirportPageV{Airport: a, Code: a.Code()}
	b, err := s.api.AirportBoards(ctx, a.ICAO)
	if err != nil && !errors.Is(err, api.ErrNotFound) {
		return liveAnswer{}, err // keep what the page shows; live.js backs off
	}
	if err == nil {
		v.setBoards(b, now)
	}
	out := liveAnswer{Parts: map[string]string{}, Next: s.liveAirportURL(a.ICAO, now)}
	for _, part := range airportParts {
		var buf bytes.Buffer
		if err := s.tpl["airport"].ExecuteTemplate(&buf, "airport-"+part, v); err != nil {
			return liveAnswer{}, err
		}
		out.Parts[part] = buf.String()
	}
	return out, nil
}

// writeLive sends a refresh answer, or why there is none.
func (s *Server) writeLive(w http.ResponseWriter, r *http.Request, a liveAnswer, err error, kind, id string) {
	var b []byte
	if err == nil {
		b, err = json.Marshal(a)
	}
	if err != nil {
		if r.Context().Err() != nil {
			return // the page went away mid-poll
		}
		s.log.Warn("live refresh failed", kind, id, "err", err)
		s.write(w, r, resp{status: http.StatusServiceUnavailable, body: []byte(`{"error":"unavailable"}`), ctype: jsonType})
		return
	}
	s.write(w, r, resp{status: http.StatusOK, body: b, ctype: jsonType}) // no max-age: no-store
}

// liveFlightAnswer renders the changing parts of the flight page from the
// gateway's live state.
func (s *Server) liveFlightAnswer(ctx context.Context, id string, now time.Time) (liveAnswer, error) {
	a, err := s.api.FlightLive(ctx, id)
	if errors.Is(err, api.ErrNotFound) || errors.Is(err, api.ErrBadRequest) {
		return liveAnswer{Done: true}, nil // closed and gone: the page keeps what it shows
	}
	if err != nil {
		return liveAnswer{}, err
	}
	if !a.Live || a.Flight == nil || a.Flight.LADDRedacted {
		return liveAnswer{Done: true}, nil
	}
	s.index.NoteLive(*a.Flight)
	f := legFromLive(*a.Flight, refs{}, now)
	if f.Model == "" {
		if ac, ok := s.index.AircraftByReg(f.Reg); ok {
			f.Model = ac.Model
		}
	}
	v := FlightPageV{Ident: f.Ident, Number: f.Number, Featured: f, AppURL: s.appLink("flight", id), Updated: timeCell(now, "UTC", "")}
	out := liveAnswer{Parts: map[string]string{}}
	for _, part := range liveParts {
		var b bytes.Buffer
		if err := s.tpl["flight"].ExecuteTemplate(&b, "flight-"+part, v); err != nil {
			return liveAnswer{}, err
		}
		out.Parts[part] = b.String()
	}
	if d, ok := legMapData(f, s.legShape(ctx, id)); ok {
		out.Map = &d
	}
	if f.Phase == "arrived" || f.Phase == "cancelled" {
		out.Done = true
	} else {
		out.Next = s.liveURL(id, now)
	}
	return out, nil
}
