package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/mikedamm/bermuda/internal/cache"
)

var (
	ErrNotFound   = errors.New("not found")
	ErrBadRequest = errors.New("bad request")
	ErrUpstream   = errors.New("upstream unavailable")
)

// Client calls the internal API. It caps concurrent requests (the ingress
// allows 100 req/s per client address; a crawler burst must not get near it)
// and caches answers briefly so page renders share upstream calls.
type Client struct {
	base  string
	token string
	http  *http.Client
	sem   chan struct{}
	cache *cache.Cache
	log   *slog.Logger
	l2    Store         // shared second tier (Bunny Database); nil when not configured
	l2w   chan struct{} // bounds background writes to it
	stats struct{ hits, misses, errors, writes, writeErrors atomic.Int64 }
}

// Store is a shared cache under the in-memory one: answers kept by key with
// the upstream status (200, 404 or 400) until they expire.
type Store interface {
	Get(ctx context.Context, key string) (status int, body []byte, expires time.Time, ok bool, err error)
	Put(ctx context.Context, key string, status int, body []byte, expires time.Time) error
}

// UseStore adds the shared cache tier.
func (c *Client) UseStore(s Store) {
	c.l2 = s
	c.l2w = make(chan struct{}, 8)
}

// Stats reports the shared tier's hit rate since start.
func (c *Client) Stats() map[string]int64 {
	if c.l2 == nil {
		return nil
	}
	return map[string]int64{"hits": c.stats.hits.Load(), "misses": c.stats.misses.Load(), "errors": c.stats.errors.Load(),
		"writes": c.stats.writes.Load(), "write_errors": c.stats.writeErrors.Load()}
}

func New(base, token string, maxConcurrent int, log *slog.Logger) *Client {
	return &Client{
		base:  strings.TrimRight(base, "/"),
		token: token,
		http:  &http.Client{Timeout: 10 * time.Second},
		sem:   make(chan struct{}, maxConcurrent),
		cache: cache.New(20000),
		log:   log,
	}
}

// apiTTL is how long every API answer is reused, misses included. The
// real-time endpoint (FlightLive) is never cached.
const apiTTL = 60 * time.Second

func (c *Client) Aircraft(ctx context.Context, key string, days, limit int) (*AircraftPage, error) {
	q := url.Values{"days": {strconv.Itoa(days)}, "limit": {strconv.Itoa(limit)}}
	return get[AircraftPage](ctx, c, "/aircraft/"+url.PathEscape(key), q)
}

func (c *Client) AircraftFlights(ctx context.Context, key string, days, limit int) (*FlightsPage, error) {
	q := url.Values{"days": {strconv.Itoa(days)}, "limit": {strconv.Itoa(limit)}}
	return get[FlightsPage](ctx, c, "/aircraft/"+url.PathEscape(key)+"/flights", q)
}

// OperatorFlights lists an operator's flights in one status group, soonest
// departure first. "scheduled" is what has not left yet: the plans and
// schedule rows the gateway holds for the day ahead.
func (c *Client) OperatorFlights(ctx context.Context, code, status string, limit int) (*OperatorFlights, error) {
	v := url.Values{"status": {status}, "sort": {"etd"}, "limit": {strconv.Itoa(limit)}}
	return get[OperatorFlights](ctx, c, "/operators/"+url.PathEscape(code)+"/flights", v)
}

func (c *Client) Search(ctx context.Context, q string, limit int) (*Search, error) {
	v := url.Values{"q": {q}, "limit": {strconv.Itoa(limit)}}
	return get[Search](ctx, c, "/search/aircraft", v)
}

// AirportBoards is the airport's arrivals, departures, inbound and scheduled
// boards, ten rows each (the gateway builds the page every 15 seconds).
func (c *Client) AirportBoards(ctx context.Context, code string) (*AirportBoards, error) {
	return get[AirportBoards](ctx, c, "/airports/"+url.PathEscape(code), nil)
}

func (c *Client) AirportTraffic(ctx context.Context, code string, radiusMi int) (*AirportTraffic, error) {
	v := url.Values{"radius_mi": {strconv.Itoa(radiusMi)}}
	return get[AirportTraffic](ctx, c, "/airports/"+url.PathEscape(code)+"/aircraft", v)
}

// FlightLive is one flight's real-time state, read from the gateway's
// memory in milliseconds. It is never cached: every call is one lookup.
// A closed flight is ErrNotFound.
func (c *Client) FlightLive(ctx context.Context, flightID string) (*LiveAnswer, error) {
	body, err := c.fetch(ctx, "/flights/"+url.PathEscape(flightID)+"/live")
	if err != nil {
		return nil, err
	}
	var out LiveAnswer
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("%w: decode: %v", ErrUpstream, err)
	}
	return &out, nil
}

// FlightPlan is the route in force for one flight, resolved to positions.
func (c *Client) FlightPlan(ctx context.Context, flightID string) (*PlanAnswer, error) {
	return get[PlanAnswer](ctx, c, "/flights/"+url.PathEscape(flightID)+"/plan", nil)
}

// FlightTrack is one flight's flown trail, kept in memory only (a trail can
// be large, and a live one changes by the second).
func (c *Client) FlightTrack(ctx context.Context, flightID string) (*TrackAnswer, error) {
	path := "/flights/" + url.PathEscape(flightID) + "/track?res=line"
	return cache.Do(c.cache, path, func() (*TrackAnswer, time.Duration, error) {
		lctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		body, err := c.fetch(lctx, path)
		switch {
		case errors.Is(err, ErrNotFound), errors.Is(err, ErrBadRequest):
			return nil, apiTTL, err
		case err != nil:
			return nil, 0, err
		}
		var out TrackAnswer
		if err := json.Unmarshal(body, &out); err != nil {
			return nil, 0, fmt.Errorf("%w: decode: %v", ErrUpstream, err)
		}
		return &out, apiTTL, nil
	})
}

// FreshAirportTraffic bypasses both caches (the crawler wants a new sweep).
func (c *Client) FreshAirportTraffic(ctx context.Context, code string, radiusMi int) (*AirportTraffic, error) {
	v := url.Values{"radius_mi": {strconv.Itoa(radiusMi)}}
	body, err := c.fetch(ctx, "/airports/"+url.PathEscape(code)+"/aircraft?"+v.Encode())
	if err != nil {
		return nil, err
	}
	var out AirportTraffic
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("%w: decode: %v", ErrUpstream, err)
	}
	return &out, nil
}

// get returns an API answer, reusing it (from memory, then the shared store)
// for apiTTL.
func get[T any](ctx context.Context, c *Client, path string, q url.Values) (*T, error) {
	key := path + "?" + q.Encode()
	return cache.Do(c.cache, key, func() (*T, time.Duration, error) {
		// The load is shared by every waiting caller, so one client hanging up
		// must not cancel it for the rest.
		lctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
		defer cancel()

		// second tier: an answer another instance (or this one before a
		// restart) already fetched
		if out, left, hit, err := fromStore[T](lctx, c, key); hit {
			return out, left, err
		}

		body, err := c.fetch(lctx, key)
		switch {
		case errors.Is(err, ErrNotFound):
			c.store(key, http.StatusNotFound, nil)
			return nil, apiTTL, err // remember misses, they are common bot traffic
		case errors.Is(err, ErrBadRequest):
			c.store(key, http.StatusBadRequest, nil)
			return nil, apiTTL, err
		case err != nil:
			return nil, 0, err
		}
		var out T
		if err := json.Unmarshal(body, &out); err != nil {
			return nil, 0, fmt.Errorf("%w: decode: %v", ErrUpstream, err)
		}
		c.store(key, http.StatusOK, body)
		return &out, apiTTL, nil
	})
}

// fromStore reads the shared tier within a short budget: a slow or failing
// database is a miss, never a slow page.
// A hit returns the stored answer, or the stored ErrNotFound/ErrBadRequest.
func fromStore[T any](ctx context.Context, c *Client, key string) (out *T, left time.Duration, hit bool, err error) {
	if c.l2 == nil {
		return nil, 0, false, nil
	}
	dctx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	status, body, expires, ok, rerr := c.l2.Get(dctx, key)
	switch {
	case rerr != nil:
		c.stats.errors.Add(1)
		c.log.Debug("cache read failed", "key", key, "err", rerr)
		return nil, 0, false, nil
	case !ok:
		c.stats.misses.Add(1)
		return nil, 0, false, nil
	}
	left = time.Until(expires)
	switch status {
	case http.StatusOK:
		var v T
		if json.Unmarshal(body, &v) != nil {
			c.stats.errors.Add(1)
			return nil, 0, false, nil
		}
		c.stats.hits.Add(1)
		return &v, min(left, apiTTL), true, nil
	case http.StatusNotFound:
		c.stats.hits.Add(1)
		return nil, min(left, apiTTL), true, ErrNotFound
	case http.StatusBadRequest:
		c.stats.hits.Add(1)
		return nil, min(left, apiTTL), true, ErrBadRequest
	}
	return nil, 0, false, nil
}

// store writes an answer to the shared tier in the background; when writes
// back up, new ones are dropped rather than queued.
func (c *Client) store(key string, status int, body []byte) {
	if c.l2 == nil {
		return
	}
	select {
	case c.l2w <- struct{}{}:
	default:
		return
	}
	go func() {
		defer func() { <-c.l2w }()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := c.l2.Put(ctx, key, status, body, time.Now().Add(apiTTL)); err != nil {
			c.stats.writeErrors.Add(1)
			c.log.Debug("cache write failed", "key", key, "err", err)
			return
		}
		c.stats.writes.Add(1)
	}()
}

// fetch returns the upstream body of a 200, or ErrNotFound, ErrBadRequest
// or ErrUpstream (after one retry).
func (c *Client) fetch(ctx context.Context, pathAndQuery string) ([]byte, error) {
	select {
	case c.sem <- struct{}{}:
		defer func() { <-c.sem }()
	case <-ctx.Done():
		return nil, fmt.Errorf("%w: %v", ErrUpstream, ctx.Err())
	}
	var lastErr error
	for attempt := range 2 {
		if attempt > 0 {
			select {
			case <-time.After(400 * time.Millisecond):
			case <-ctx.Done():
				return nil, fmt.Errorf("%w: %v", ErrUpstream, ctx.Err())
			}
		}
		start := time.Now()
		body, err := c.once(ctx, pathAndQuery)
		c.log.Debug("api", "path", pathAndQuery, "ms", time.Since(start).Milliseconds(), "err", err)
		if err == nil || errors.Is(err, ErrNotFound) || errors.Is(err, ErrBadRequest) {
			return body, err
		}
		lastErr = err
	}
	c.log.Warn("api failed", "path", pathAndQuery, "err", lastErr)
	return nil, lastErr
}

func (c *Client) once(ctx context.Context, pathAndQuery string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+pathAndQuery, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "wingview-seo/1")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, ErrNotFound
	case resp.StatusCode == http.StatusBadRequest:
		return nil, ErrBadRequest
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("%w: status %d", ErrUpstream, resp.StatusCode)
	}
	return body, nil
}
