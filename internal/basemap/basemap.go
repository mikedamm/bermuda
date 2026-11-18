// Package basemap picks the Protomaps build the maps draw: the last build of
// the newest version Protomaps has finished with. Protomaps keeps the past
// week's daily builds and, for good, the last build of every patch version
// (docs.protomaps.com/basemaps/downloads), so that build never leaves its
// URL, and the CDN's cache of it stays good until a newer version is
// finished, every few weeks to few months.
package basemap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const (
	// List is Protomaps' list of the builds it holds.
	List = "https://build-metadata.protomaps.dev/builds.json"
	// Fallback is drawn until the list has been read: the newest final
	// build when this was written (4.15.1, final when 4.15.2 came out).
	Fallback = "20260811.pmtiles"
	// Major is the schema the site's map style is written for; a build of
	// another major version would draw nothing it knows.
	Major = 4
	// Every is how often the list is read again.
	Every = 6 * time.Hour
)

// Build is one entry of the list.
type Build struct {
	Key     string `json:"key"`  // "20260811.pmtiles"
	Size    int64  `json:"size"` // bytes
	Version string `json:"version"`
}

type version [3]int

func parseVersion(s string) (version, bool) {
	var v version
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return v, false
		}
		v[i] = n
	}
	return v, true
}

func (a version) less(b version) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

// Final is the last build of the newest version of the major one that a
// later version, of any major, has replaced; false when there is none. The
// newest version is still being built daily, and its builds are deleted
// after a week.
func Final(builds []Build, major int) (Build, bool) {
	var newest version
	for _, b := range builds {
		if v, ok := parseVersion(b.Version); ok && newest.less(v) {
			newest = v
		}
	}
	var best Build
	var bestV version
	found := false
	for _, b := range builds {
		v, ok := parseVersion(b.Version)
		if !ok || v[0] != major || !v.less(newest) {
			continue
		}
		if !found || bestV.less(v) || (v == bestV && b.Key > best.Key) {
			best, bestV, found = b, v, true
		}
	}
	return best, found
}

// Source is the build the maps draw now, read again from the list every
// six hours.
type Source struct {
	Base    string // where the builds are served, ending in "/"
	ListURL string
	Client  *http.Client
	Log     *slog.Logger
	url     atomic.Pointer[string]
}

// New draws the fallback build from base until Run has read the list.
func New(base string, log *slog.Logger) *Source {
	s := &Source{Base: base, ListURL: List, Client: &http.Client{Timeout: 30 * time.Second}, Log: log}
	u := base + Fallback
	s.url.Store(&u)
	return s
}

// URL is the archive the maps draw now.
func (s *Source) URL() string { return *s.url.Load() }

// Run reads the list now and every six hours after, until ctx ends.
func (s *Source) Run(ctx context.Context) {
	go func() {
		for {
			if err := s.Refresh(ctx); err != nil {
				s.Log.Warn("basemap: keeping "+s.URL(), "err", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(Every):
			}
		}
	}()
}

// Refresh moves to the list's final build once its first bytes come back
// through the CDN as that build.
func (s *Source) Refresh(ctx context.Context) error {
	builds, err := s.list(ctx)
	if err != nil {
		return err
	}
	b, ok := Final(builds, Major)
	if !ok {
		return errors.New("no final build in the list")
	}
	u := s.Base + b.Key
	if u == s.URL() {
		return nil
	}
	if err := s.check(ctx, u, b.Size); err != nil {
		return fmt.Errorf("%s: %w", b.Key, err)
	}
	s.url.Store(&u)
	s.Log.Info("basemap", "build", b.Key, "version", b.Version)
	return nil
}

func (s *Source) list(ctx context.Context) ([]Build, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.ListURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("build list: status %d", resp.StatusCode)
	}
	var builds []Build
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&builds); err != nil {
		return nil, fmt.Errorf("build list: %w", err)
	}
	return builds, nil
}

// check reads the archive's header range through the CDN: it must be a
// partial answer from a file of the listed size that starts as a PMTiles
// archive does. The CDN caches a 404 and keeps slices of a file that
// changed, and a bad first range blanks every map, so a build that fails
// is not drawn.
func (s *Source) check(ctx context.Context, u string, size int64) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Range", "bytes=0-126")
	resp, err := s.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("header range: status %d", resp.StatusCode)
	}
	if _, total, _ := strings.Cut(resp.Header.Get("Content-Range"), "/"); size > 0 && total != strconv.FormatInt(size, 10) {
		return fmt.Errorf("header range: file of %s bytes, the list says %d", total, size)
	}
	magic := make([]byte, 7)
	if _, err := io.ReadFull(resp.Body, magic); err != nil || string(magic) != "PMTiles" {
		return errors.New("header range: not a PMTiles archive")
	}
	return nil
}
