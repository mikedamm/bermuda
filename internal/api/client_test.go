package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type memStore struct {
	mu   sync.Mutex
	rows map[string]struct {
		status  int
		body    []byte
		expires time.Time
	}
	fail bool
}

func (m *memStore) Get(_ context.Context, key string) (int, []byte, time.Time, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail {
		return 0, nil, time.Time{}, false, errors.New("down")
	}
	r, ok := m.rows[key]
	if !ok || time.Now().After(r.expires) {
		return 0, nil, time.Time{}, false, nil
	}
	return r.status, r.body, r.expires, true, nil
}

func (m *memStore) Put(_ context.Context, key string, status int, body []byte, expires time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows[key] = struct {
		status  int
		body    []byte
		expires time.Time
	}{status, body, expires}
	return nil
}

func (m *memStore) has(key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.rows[key]
	return ok
}

// A second instance (fresh memory) answers from the shared store without
// asking the API, misses included; a failing store falls back to the API.
func TestSharedStore(t *testing.T) {
	var calls atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Query().Get("q") == "NOPE" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		io.WriteString(w, `{"q":"UAL1275","count":1,"results":[{"registration":"N37555"}]}`)
	}))
	defer up.Close()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	store := &memStore{rows: map[string]struct {
		status  int
		body    []byte
		expires time.Time
	}{}}
	ctx := context.Background()

	a := New(up.URL, "t", 2, log)
	a.UseStore(store)
	if res, err := a.Search(ctx, "UAL1275", 5); err != nil || res.Results[0].Registration != "N37555" {
		t.Fatalf("first search: %+v %v", res, err)
	}
	if _, err := a.Search(ctx, "NOPE", 5); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing search: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second) // writes are in the background
	for !(store.has("/search/aircraft?limit=5&q=UAL1275") && store.has("/search/aircraft?limit=5&q=NOPE")) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	b := New(up.URL, "t", 2, log)
	b.UseStore(store)
	before := calls.Load()
	if res, err := b.Search(ctx, "UAL1275", 5); err != nil || res.Results[0].Registration != "N37555" {
		t.Fatalf("shared search: %+v %v", res, err)
	}
	if _, err := b.Search(ctx, "NOPE", 5); !errors.Is(err, ErrNotFound) {
		t.Fatalf("shared miss: %v", err)
	}
	if n := calls.Load() - before; n != 0 {
		t.Fatalf("second instance called the API %d times", n)
	}
	if s := b.Stats(); s["hits"] != 2 {
		t.Fatalf("stats = %v", s)
	}

	store.fail = true
	c := New(up.URL, "t", 2, log)
	c.UseStore(store)
	if _, err := c.Search(ctx, "UAL1275", 5); err != nil {
		t.Fatalf("store failure must fall back to the API: %v", err)
	}
}

// The real-time endpoint is never cached: every call is one lookup, and
// nothing reaches the shared store.
func TestFlightLiveUncached(t *testing.T) {
	var calls atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		io.WriteString(w, `{"key":"f-1","kind":"flight","live":true}`)
	}))
	defer up.Close()
	store := &memStore{rows: map[string]struct {
		status  int
		body    []byte
		expires time.Time
	}{}}
	c := New(up.URL, "t", 2, slog.New(slog.NewTextHandler(io.Discard, nil)))
	c.UseStore(store)
	for range 3 {
		if a, err := c.FlightLive(context.Background(), "f-1"); err != nil || !a.Live {
			t.Fatalf("live: %+v %v", a, err)
		}
	}
	if n := calls.Load(); n != 3 {
		t.Fatalf("3 calls made %d upstream lookups, want 3", n)
	}
	time.Sleep(50 * time.Millisecond) // a background write would land by now
	if store.has("/flights/f-1/live") {
		t.Fatal("live state was written to the shared store")
	}
}

// An airport board mixes live flights (codes) with archived ones (airport
// references); each row decodes as what it is.
func TestBoardRows(t *testing.T) {
	var b AirportBoards
	err := json.Unmarshal([]byte(`{"code":"KSEA","counts":{"arrivals":2},"boards":{"arrivals":[
		{"flight_id":"a","callsign":"ASA1","dep":"KPDX","arr":"KSEA","phase":"landed","board_time":"2026-11-17T01:00:00Z"},
		{"flight_id":"b","callsign":"ASA2","dep":{"icao":"KSFO"},"arr":{"icao":"KSEA"},"phase":"arrived","first_seen":"2026-11-16T20:00:00Z","board_time":"2026-11-17T00:30:00Z"}]}}`), &b)
	if err != nil {
		t.Fatal(err)
	}
	rows := b.Boards.Arrivals
	if len(rows) != 2 || rows[0].Live == nil || rows[0].Live.Dep != "KPDX" || rows[1].Archived == nil || rows[1].Archived.Dep.ICAO != "KSFO" {
		t.Fatalf("rows: %+v", rows)
	}
	if b.Counts.Arrivals != 2 || rows[1].BoardTime.IsZero() {
		t.Errorf("counts %+v, board time %v", b.Counts, rows[1].BoardTime)
	}
}
