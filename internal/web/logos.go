package web

// Operator logos live on the CDN as {base}/{ICAO}/icon.png: 50×50 marks on
// a transparent ground. Not every operator has one, and a missing one is an
// HTML 404, so a page shows a logo only once it is known to exist. The
// first render that asks for a code queues a background check and renders
// without it; later renders have it. Pages never wait on the CDN.

import (
	"context"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	logoFresh    = 24 * time.Hour   // look again at a known logo daily (it may be removed)
	logoMissing  = 6 * time.Hour    // and at a missing one every six hours (it may be added)
	logoRetry    = 10 * time.Minute // after a check that failed
	logoWorkers  = 4
	maxLogoCodes = 4096
)

var logoCode = regexp.MustCompile(`^[A-Z0-9]{2,4}$`)

type logoState struct {
	ok     bool
	next   time.Time // when to check again; zero means now
	queued bool
}

type logoSet struct {
	base   string // no trailing slash
	client *http.Client
	log    *slog.Logger
	queue  chan string
	mu     sync.Mutex
	known  map[string]*logoState
}

// newLogoSet returns nil (no logos) for an empty base.
func newLogoSet(base string, log *slog.Logger) *logoSet {
	if base == "" {
		return nil
	}
	l := &logoSet{
		base: strings.TrimRight(base, "/"), log: log,
		client: &http.Client{Timeout: 5 * time.Second},
		queue:  make(chan string, 1024),
		known:  map[string]*logoState{},
	}
	for range logoWorkers {
		go func() {
			for code := range l.queue {
				l.check(code)
			}
		}()
	}
	return l
}

func (l *logoSet) url(code string) string { return l.base + "/" + code + "/icon.png" }

// URL is an operator's logo, or "" while it is unknown or missing.
func (l *logoSet) URL(code string) string {
	if l == nil || !logoCode.MatchString(code) {
		return ""
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	st := l.known[code]
	if st == nil {
		if len(l.known) >= maxLogoCodes {
			return ""
		}
		st = &logoState{}
		l.known[code] = st
	}
	if !st.queued && time.Now().After(st.next) {
		select {
		case l.queue <- code:
			st.queued = true
		default: // a backlog: a later render asks again
		}
	}
	if st.ok {
		return l.url(code)
	}
	return ""
}

func (l *logoSet) check(code string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ok, wait := false, logoRetry
	req, _ := http.NewRequestWithContext(ctx, http.MethodHead, l.url(code), nil)
	res, err := l.client.Do(req)
	if err == nil {
		res.Body.Close()
		switch {
		case res.StatusCode == http.StatusOK && strings.HasPrefix(res.Header.Get("Content-Type"), "image/"):
			ok, wait = true, logoFresh
		case res.StatusCode < 500:
			wait = logoMissing
		}
	} else {
		l.log.Debug("logo check failed", "code", code, "err", err)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	st := l.known[code]
	st.queued = false
	st.next = time.Now().Add(wait)
	if wait != logoRetry { // a failed check keeps what was known
		st.ok = ok
	}
}

// Status counts the codes checked and the logos found, for /statusz.
func (l *logoSet) Status() map[string]int {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, st := range l.known {
		if st.ok {
			n++
		}
	}
	return map[string]int{"codes": len(l.known), "found": n}
}
