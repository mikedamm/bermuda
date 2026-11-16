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

// TTLs per endpoint: live data short, archives longer.
const (
	ttlSearch   = 45 * time.Second
	ttlAircraft = 90 * time.Second
	ttlFlights  = 8 * time.Minute
	ttlAirport  = 45 * time.Second
	ttlMissing  = 5 * time.Minute
)

func (c *Client) Aircraft(ctx context.Context, key string, days, limit int) (*AircraftPage, error) {
	q := url.Values{"days": {strconv.Itoa(days)}, "limit": {strconv.Itoa(limit)}}
	return get[AircraftPage](ctx, c, "/aircraft/"+url.PathEscape(key), q, ttlAircraft)
}

func (c *Client) AircraftFlights(ctx context.Context, key string, days, limit int) (*FlightsPage, error) {
	q := url.Values{"days": {strconv.Itoa(days)}, "limit": {strconv.Itoa(limit)}}
	return get[FlightsPage](ctx, c, "/aircraft/"+url.PathEscape(key)+"/flights", q, ttlFlights)
}

func (c *Client) Search(ctx context.Context, q string, limit int) (*Search, error) {
	v := url.Values{"q": {q}, "limit": {strconv.Itoa(limit)}}
	return get[Search](ctx, c, "/search/aircraft", v, ttlSearch)
}

func (c *Client) AirportTraffic(ctx context.Context, code string, radiusMi int) (*AirportTraffic, error) {
	v := url.Values{"radius_mi": {strconv.Itoa(radiusMi)}}
	return get[AirportTraffic](ctx, c, "/airports/"+url.PathEscape(code)+"/aircraft", v, ttlAirport)
}

// FreshAirportTraffic bypasses the cache (the crawler wants a new sweep).
func (c *Client) FreshAirportTraffic(ctx context.Context, code string, radiusMi int) (*AirportTraffic, error) {
	var out AirportTraffic
	v := url.Values{"radius_mi": {strconv.Itoa(radiusMi)}}
	if err := c.fetch(ctx, "/airports/"+url.PathEscape(code)+"/aircraft?"+v.Encode(), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func get[T any](ctx context.Context, c *Client, path string, q url.Values, ttl time.Duration) (*T, error) {
	key := path + "?" + q.Encode()
	return cache.Do(c.cache, key, func() (*T, time.Duration, error) {
		// The load is shared by every waiting caller, so one client hanging up
		// must not cancel it for the rest.
		lctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
		defer cancel()
		var out T
		err := c.fetch(lctx, key, &out)
		switch {
		case errors.Is(err, ErrNotFound), errors.Is(err, ErrBadRequest):
			return nil, ttlMissing, err // remember misses, they are common bot traffic
		case err != nil:
			return nil, 0, err
		}
		return &out, ttl, nil
	})
}

func (c *Client) fetch(ctx context.Context, pathAndQuery string, out any) error {
	select {
	case c.sem <- struct{}{}:
		defer func() { <-c.sem }()
	case <-ctx.Done():
		return fmt.Errorf("%w: %v", ErrUpstream, ctx.Err())
	}
	var lastErr error
	for attempt := range 2 {
		if attempt > 0 {
			select {
			case <-time.After(400 * time.Millisecond):
			case <-ctx.Done():
				return fmt.Errorf("%w: %v", ErrUpstream, ctx.Err())
			}
		}
		start := time.Now()
		err := c.once(ctx, pathAndQuery, out)
		c.log.Debug("api", "path", pathAndQuery, "ms", time.Since(start).Milliseconds(), "err", err)
		if err == nil || errors.Is(err, ErrNotFound) || errors.Is(err, ErrBadRequest) {
			return err
		}
		lastErr = err
	}
	c.log.Warn("api failed", "path", pathAndQuery, "err", lastErr)
	return lastErr
}

func (c *Client) once(ctx context.Context, pathAndQuery string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+pathAndQuery, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "wingview-seo/1")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return ErrNotFound
	case resp.StatusCode == http.StatusBadRequest:
		return ErrBadRequest
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("%w: status %d", ErrUpstream, resp.StatusCode)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("%w: decode: %v", ErrUpstream, err)
	}
	return nil
}
