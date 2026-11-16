package store

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"time"

	"github.com/mikedamm/bermuda/internal/api"
	"github.com/mikedamm/bermuda/internal/geo"
)

// Crawler discovers what to put in sitemaps and hub pages: it sweeps the
// large airports for live traffic and resolves routes for flights it saw
// with only one end known. Everything runs at a fixed, gentle rate.
type Crawler struct {
	API         *api.Client
	Index       *Index
	Log         *slog.Logger
	DataDir     string
	SweepGap    time.Duration // between two airport sweeps
	SweepEvery  time.Duration // revisit interval for an airport with traffic
	ResolveGap  time.Duration // between two route lookups; 0 disables
	RadiusMi    int
	MaxBackoff  time.Duration // revisit cap for airports that keep coming back empty
	SaveEvery   time.Duration
	sweepQueue  map[string]time.Time
	triedRoutes map[string]time.Time
}

func (c *Crawler) Run(ctx context.Context) {
	c.sweepQueue = map[string]time.Time{}
	now := time.Now()
	for _, a := range geo.LargeAirports() {
		due := now.Add(time.Duration(rand.Int64N(int64(c.SweepGap) * 60))) // spread the first pass
		if act, ok := c.Index.AirportActivityFor(a.ICAO); ok {
			due = act.Swept.Add(c.revisit(act))
		}
		c.sweepQueue[a.ICAO] = due
	}
	c.triedRoutes = map[string]time.Time{}
	go c.loop(ctx, c.SweepGap, c.sweepOne)
	if c.ResolveGap > 0 {
		go c.loop(ctx, c.ResolveGap, c.resolveOne)
	}
	go c.loop(ctx, c.SaveEvery, c.persist)
}

func (c *Crawler) loop(ctx context.Context, every time.Duration, step func(context.Context)) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return // main saves the final snapshot after the server drains
		case <-t.C:
			step(ctx)
		}
	}
}

func (c *Crawler) revisit(act AirportActivity) time.Duration {
	if act.Total > 0 {
		return c.SweepEvery
	}
	d := c.SweepEvery << min(act.EmptyRuns, 6)
	return min(d, c.MaxBackoff)
}

func (c *Crawler) sweepOne(ctx context.Context) {
	now := time.Now()
	next, best := "", now
	for code, due := range c.sweepQueue {
		if !due.After(best) {
			next, best = code, due
		}
	}
	if next == "" {
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	t, err := c.API.FreshAirportTraffic(cctx, next, c.RadiusMi)
	if err != nil {
		c.sweepQueue[next] = now.Add(c.SweepEvery)
		c.Log.Debug("sweep failed", "airport", next, "err", err)
		return
	}
	act := c.Index.NoteSweep(next, t, now)
	c.sweepQueue[next] = now.Add(c.revisit(act))
}

func (c *Crawler) resolveOne(ctx context.Context) {
	now := time.Now()
	ident := ""
	for _, cs := range c.Index.AirlinesMissingName(now.Add(-2*time.Hour), 50) {
		if now.Sub(c.triedRoutes[cs]) > 6*time.Hour {
			ident = cs
			break
		}
	}
	if ident == "" {
		for _, cs := range c.Index.FlightsNeedingRoute(now.Add(-30*time.Minute), 200) {
			if now.Sub(c.triedRoutes[cs]) > 2*time.Hour {
				ident = cs
				break
			}
		}
	}
	if ident == "" {
		return
	}
	c.triedRoutes[ident] = now
	if len(c.triedRoutes) > 200_000 {
		clear(c.triedRoutes)
	}
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	res, err := c.API.Search(cctx, ident, 5)
	if err != nil {
		return
	}
	for _, r := range res.Results {
		if r.Live != nil && r.Live.Callsign == ident {
			c.Index.NoteLive(*r.Live)
			return
		}
	}
}

func (c *Crawler) persist(ctx context.Context) {
	c.Index.Prune(time.Now())
	if c.DataDir == "" {
		return
	}
	if err := c.Index.Save(c.DataDir); err != nil {
		c.Log.Warn("index save failed", "err", err)
	}
}
