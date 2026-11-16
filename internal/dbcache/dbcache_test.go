package dbcache

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeLibSQL answers the pipeline API for the three statements the cache uses.
func fakeLibSQL(t *testing.T) *httptest.Server {
	var mu sync.Mutex
	rows := map[string][]value{}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/pipeline" || r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var in struct{ Requests []request }
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Fatal(err)
		}
		if last := in.Requests[len(in.Requests)-1]; last.Type != "close" {
			t.Errorf("pipeline does not end with close: %+v", last)
		}
		mu.Lock()
		defer mu.Unlock()
		var results []map[string]any
		for _, rq := range in.Requests {
			res := map[string]any{"type": "ok", "response": map[string]any{"type": rq.Type}}
			if rq.Stmt != nil {
				out := [][]value{}
				sql := strings.TrimSpace(rq.Stmt.SQL)
				switch {
				case strings.HasPrefix(sql, "INSERT"):
					rows[rq.Stmt.Args[0].Value] = rq.Stmt.Args[1:]
				case strings.HasPrefix(sql, "SELECT status"):
					if v, ok := rows[rq.Stmt.Args[0].Value]; ok && v[2].int() > rq.Stmt.Args[1].int() {
						// libSQL answers blobs as base64 without padding
						row := append([]value(nil), v...)
						row[1].Base64 = strings.TrimRight(row[1].Base64, "=")
						out = append(out, row)
					}
				}
				res["response"] = map[string]any{"type": "execute", "result": map[string]any{"rows": out}}
			}
			results = append(results, res)
		}
		json.NewEncoder(w).Encode(map[string]any{"results": results})
	}))
}

func TestRoundTrip(t *testing.T) {
	srv := fakeLibSQL(t)
	defer srv.Close()
	db, err := Open(strings.Replace(srv.URL, "http://", "ws://", 1), "tok")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := db.Init(ctx); err != nil {
		t.Fatal(err)
	}
	exp := time.Now().Add(time.Minute)
	// bodies of several lengths, so the encoded blobs need 0, 1 and 2 padding characters
	for i, body := range [][]byte{[]byte(`{"q":"UAL1275","results":[]}`), []byte(`{"q":"A"}`), []byte(`{"q":"AB","count":12}`), []byte(strings.Repeat(`{"x":1}`, 500))} {
		key := "/k" + strconv.Itoa(i)
		if err := db.Put(ctx, key, 200, body, exp); err != nil {
			t.Fatal(err)
		}
		status, got, expires, ok, err := db.Get(ctx, key)
		if err != nil || !ok || status != 200 || string(got) != string(body) || expires.UnixMilli() != exp.UnixMilli() {
			t.Fatalf("get %s = %d %.40q %v %v %v", key, status, got, expires, ok, err)
		}
	}
	body := []byte(`{}`)
	if err := db.Put(ctx, "/old", 200, body, time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, _, _, ok, _ := db.Get(ctx, "/old"); ok {
		t.Fatal("expired answer returned")
	}
}

func TestOpenURL(t *testing.T) {
	db, err := Open("libsql://abc-wv-seo-cache.lite.bunnydb.net/", "t")
	if err != nil || db.endpoint != "https://abc-wv-seo-cache.lite.bunnydb.net/v2/pipeline" {
		t.Fatalf("endpoint = %q, %v", db.endpoint, err)
	}
	if _, err := Open("postgres://x", "t"); err == nil {
		t.Fatal("non-libsql URL accepted")
	}
}
