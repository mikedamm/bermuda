package web

// The flight page's live refresh. A page showing a flight that is still
// under way carries a URL to this endpoint; its script polls it every 30
// seconds and swaps in the parts of the page that change.
//
// What keeps it from being worth scraping:
//   - the URL is signed and expires. It is bound to one flight and handed
//     out only by that flight's page, so there is nothing to enumerate: no
//     callsigns, no registrations, no lists.
//   - the token is a path segment, so a CDN that leaves query strings out of
//     its cache key still cannot hand a signed answer to an unsigned URL.
//   - an answer is the page's own HTML for the parts that change, plus the
//     map's moving pieces: nothing the page does not already show.
//   - one render per flight answers every poller for a few seconds and the
//     CDN shares it for a few more, so more viewers cost the API nothing.
//   - no CORS, a same-origin resource policy, noindex, and robots.txt.

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mikedamm/bermuda/internal/api"
	"github.com/mikedamm/bermuda/internal/cache"
)

const (
	// liveTokenLife is how long a signed URL works. Every answer carries a
	// renewed one, so a page left open on a long flight never runs out.
	liveTokenLife = 6 * time.Hour
	liveShare     = 5 * time.Second // one render answers every poller this long
	liveCDN       = 10              // seconds the CDN may reuse an answer
	jsonType      = "application/json"
)

var flightIDRe = regexp.MustCompile(`^[A-Za-z0-9-]{4,64}$`)

// liveParts are the regions of the flight page the refresh replaces; each
// is a template named flight-<part> in flight.html.
var liveParts = []string{"meta", "dock", "times", "track"}

// liveURL signs a refresh URL for one flight. Expiry is rounded to the hour
// so every page rendered in the same hour polls the same URL and shares the
// CDN's copy.
func (s *Server) liveURL(flightID string, now time.Time) string {
	exp := strconv.FormatInt(now.Truncate(time.Hour).Add(liveTokenLife).Unix(), 36)
	return Prefix + "/live/" + flightID + "/" + exp + "." + s.liveSig(flightID, exp)
}

func (s *Server) liveSig(flightID, exp string) string {
	m := hmac.New(sha256.New, s.cfg.LiveKey)
	m.Write([]byte("live\x00" + flightID + "\x00" + exp))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil)[:16])
}

func (s *Server) liveValid(flightID, token string, now time.Time) bool {
	exp, sig, ok := strings.Cut(token, ".")
	if !ok {
		return false
	}
	t, err := strconv.ParseInt(exp, 36, 64)
	if err != nil || now.Unix() >= t || t > now.Add(liveTokenLife+time.Hour).Unix() {
		return false
	}
	return hmac.Equal([]byte(sig), []byte(s.liveSig(flightID, exp)))
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
	if !flightIDRe.MatchString(id) || !s.liveValid(id, r.PathValue("token"), now) {
		s.write(w, r, resp{status: http.StatusForbidden, body: []byte(`{"error":"expired"}`), ctype: jsonType})
		return
	}
	out, err := cache.Do(s.pages, "live "+id, func() (resp, time.Duration, error) {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 8*time.Second)
		defer cancel()
		a, err := s.liveFlightAnswer(ctx, id, now)
		if err != nil {
			return resp{}, 0, err
		}
		b, err := json.Marshal(a)
		if err != nil {
			return resp{}, 0, err
		}
		return resp{status: http.StatusOK, body: b, ctype: jsonType, cache: fmt.Sprintf("public, max-age=%d", liveCDN)}, liveShare, nil
	})
	if err != nil {
		s.log.Warn("live refresh failed", "flight", id, "err", err)
		out = resp{status: http.StatusServiceUnavailable, body: []byte(`{"error":"unavailable"}`), ctype: jsonType}
	}
	s.write(w, r, out)
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
