package basemap

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// the tail of Protomaps' list on 2026-11-18
var listed = []Build{
	{"20260720.pmtiles", 136853246056, "4.14.11"},
	{"20260722.pmtiles", 136951449547, "4.15.0"},
	{"20260811.pmtiles", 137295889397, "4.15.1"},
	{"20261112.pmtiles", 139437771201, "4.15.2"},
	{"20261113.pmtiles", 139431874442, "4.15.2"},
	{"20261117.pmtiles", 139547428331, "4.15.2"},
}

func TestFinal(t *testing.T) {
	with := func(extra ...Build) []Build { return append(append([]Build(nil), listed...), extra...) }
	for _, c := range []struct {
		name   string
		builds []Build
		want   string
	}{
		{"the newest version is still being built", listed, "20260811.pmtiles"},
		{"a new patch version finishes the last one's last build", with(Build{"20261119.pmtiles", 1, "4.15.3"}), "20261117.pmtiles"},
		{"a new major version finishes the last of ours", with(Build{"20261201.pmtiles", 1, "5.0.0"}), "20261117.pmtiles"},
		{"versions compare as numbers", []Build{{"a.pmtiles", 1, "4.9.0"}, {"b.pmtiles", 1, "4.10.0"}, {"c.pmtiles", 1, "4.11.0"}}, "b.pmtiles"},
		{"unreadable versions are left out", []Build{{"a.pmtiles", 1, "4.1"}, {"b.pmtiles", 1, "4.2.0"}, {"c.pmtiles", 1, "x"}}, ""},
		{"one version has nothing final", []Build{{"a.pmtiles", 1, "4.2.0"}, {"b.pmtiles", 1, "4.2.0"}}, ""},
		{"an old major's builds are not ours", []Build{{"a.pmtiles", 1, "3.9.0"}, {"b.pmtiles", 1, "4.0.0"}}, ""},
	} {
		got, ok := Final(c.builds, Major)
		if got.Key != c.want || ok != (c.want != "") {
			t.Errorf("%s: got %q %v, want %q", c.name, got.Key, ok, c.want)
		}
	}
}

// a list and a CDN serving the builds' header ranges, each answer settable
func fakeCDN(t *testing.T, builds []Build, serve func(key string, w http.ResponseWriter)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/builds.json" {
			fmt.Fprint(w, "[")
			for i, b := range builds {
				if i > 0 {
					fmt.Fprint(w, ",")
				}
				fmt.Fprintf(w, `{"key":%q,"size":%d,"uploaded":"2026-11-17T09:09:23.297Z","version":%q}`, b.Key, b.Size, b.Version)
			}
			fmt.Fprint(w, "]")
			return
		}
		if r.Header.Get("Range") != "bytes=0-126" {
			t.Errorf("%s asked for %q", r.URL.Path, r.Header.Get("Range"))
		}
		serve(strings.TrimPrefix(r.URL.Path, "/"), w)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func archive(size int64, magic string) func(string, http.ResponseWriter) {
	return func(_ string, w http.ResponseWriter) {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-126/%d", size))
		w.WriteHeader(http.StatusPartialContent)
		io.WriteString(w, magic+strings.Repeat("\x00", 120))
	}
}

func TestRefresh(t *testing.T) {
	builds := append(append([]Build(nil), listed...), Build{"20261119.pmtiles", 1, "4.15.3"}) // 20261117 is final now
	for _, c := range []struct {
		name  string
		serve func(string, http.ResponseWriter)
		moved bool
	}{
		{"a good archive", archive(139547428331, "PMTiles"), true},
		{"a cached 404", func(_ string, w http.ResponseWriter) { http.NotFound(w, nil) }, false},
		{"another file under the name", archive(139535414652, "PMTiles"), false},
		{"not an archive", archive(139547428331, "<html>"), false},
	} {
		srv := fakeCDN(t, builds, c.serve)
		s := New(srv.URL+"/", slog.New(slog.DiscardHandler))
		s.ListURL = srv.URL + "/builds.json"
		err := s.Refresh(context.Background())
		want := srv.URL + "/" + Fallback
		if c.moved {
			want = srv.URL + "/20261117.pmtiles"
		}
		if s.URL() != want || (err == nil) != c.moved {
			t.Errorf("%s: drawing %s (err %v), want %s", c.name, s.URL(), err, want)
		}
	}
}

// The fallback is today's final build, so a server that cannot read the
// list draws the same archive as one that can.
func TestFallbackIsFinal(t *testing.T) {
	srv := fakeCDN(t, listed, func(key string, _ http.ResponseWriter) { t.Errorf("checked %s, already drawing it", key) })
	s := New(srv.URL+"/", slog.New(slog.DiscardHandler))
	s.ListURL = srv.URL + "/builds.json"
	if err := s.Refresh(context.Background()); err != nil || s.URL() != srv.URL+"/"+Fallback {
		t.Errorf("drawing %s (err %v)", s.URL(), err)
	}
	if b, _ := Final(listed, Major); b.Key != Fallback {
		t.Errorf("final %s, fallback %s", b.Key, Fallback)
	}
}
