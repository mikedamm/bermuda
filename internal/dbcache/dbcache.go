// Package dbcache keeps API answers in a Bunny Database (libSQL), shared by
// every instance and kept across restarts. It speaks libSQL's HTTP pipeline
// API (Hrana over HTTP, v2) with the standard library; no driver needed.
package dbcache

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type DB struct {
	endpoint string // https://host/v2/pipeline
	token    string
	http     *http.Client
}

// Open turns a libsql:// (or https://) database URL into a pipeline client.
func Open(rawURL, token string) (*DB, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("bad database URL %q", rawURL)
	}
	switch u.Scheme {
	case "libsql", "wss", "https":
		u.Scheme = "https"
	case "ws", "http":
		u.Scheme = "http"
	default:
		return nil, fmt.Errorf("unsupported database URL scheme %q", u.Scheme)
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/v2/pipeline"
	return &DB{endpoint: u.String(), token: token, http: &http.Client{Timeout: 10 * time.Second}}, nil
}

// ---------- the pipeline protocol ----------

type value struct {
	Type   string `json:"type"`
	Value  string `json:"value,omitempty"`
	Base64 string `json:"base64,omitempty"`
}

type stmt struct {
	SQL  string  `json:"sql"`
	Args []value `json:"args,omitempty"`
}

type request struct {
	Type string `json:"type"`
	Stmt *stmt  `json:"stmt,omitempty"`
}

type pipelineResponse struct {
	Results []struct {
		Type  string `json:"type"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
		Response *struct {
			Result *struct {
				Rows [][]value `json:"rows"`
			} `json:"result"`
		} `json:"response"`
	} `json:"results"`
}

func text(s string) value   { return value{Type: "text", Value: s} }
func integer(n int64) value { return value{Type: "integer", Value: strconv.FormatInt(n, 10)} }
func blob(b []byte) value   { return value{Type: "blob", Base64: base64.StdEncoding.EncodeToString(b)} }
func (v value) int() int64  { n, _ := strconv.ParseInt(v.Value, 10, 64); return n }

// bytes decodes a blob. libSQL returns base64 without padding, so accept
// both forms; a bad blob is an error, never a silently truncated value.
func (v value) bytes() ([]byte, error) {
	return base64.RawStdEncoding.DecodeString(strings.TrimRight(v.Base64, "="))
}

// exec runs statements in one round trip and returns each one's rows.
func (d *DB) exec(ctx context.Context, stmts ...stmt) ([][][]value, error) {
	reqs := make([]request, 0, len(stmts)+1)
	for i := range stmts {
		reqs = append(reqs, request{Type: "execute", Stmt: &stmts[i]})
	}
	reqs = append(reqs, request{Type: "close"})
	payload, err := json.Marshal(map[string]any{"requests": reqs})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+d.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("database: status %d: %.200s", resp.StatusCode, body)
	}
	var pr pipelineResponse
	if err := json.Unmarshal(body, &pr); err != nil {
		return nil, fmt.Errorf("database: %w", err)
	}
	out := make([][][]value, len(stmts))
	for i := range stmts {
		if i >= len(pr.Results) {
			return nil, errors.New("database: short pipeline response")
		}
		r := pr.Results[i]
		if r.Type != "ok" {
			msg := "unknown error"
			if r.Error != nil {
				msg = r.Error.Message
			}
			return nil, fmt.Errorf("database: %s", msg)
		}
		if r.Response != nil && r.Response.Result != nil {
			out[i] = r.Response.Result.Rows
		}
	}
	return out, nil
}

// ---------- the cache ----------

// Init creates the table once; safe to run on every start.
func (d *DB) Init(ctx context.Context) error {
	_, err := d.exec(ctx,
		stmt{SQL: `CREATE TABLE IF NOT EXISTS api_cache (
			key TEXT PRIMARY KEY,
			status INTEGER NOT NULL,
			body BLOB NOT NULL,
			expires INTEGER NOT NULL
		)`},
		stmt{SQL: `CREATE INDEX IF NOT EXISTS api_cache_expires ON api_cache (expires)`},
	)
	return err
}

// Get returns a stored answer that has not expired.
func (d *DB) Get(ctx context.Context, key string) (status int, body []byte, expires time.Time, ok bool, err error) {
	now := time.Now()
	rows, err := d.exec(ctx, stmt{
		SQL:  `SELECT status, body, expires FROM api_cache WHERE key = ? AND expires > ?`,
		Args: []value{text(key), integer(now.UnixMilli())},
	})
	if err != nil || len(rows[0]) == 0 || len(rows[0][0]) < 3 {
		return 0, nil, time.Time{}, false, err
	}
	row := rows[0][0]
	raw, err := row[1].bytes()
	if err != nil {
		return 0, nil, time.Time{}, false, err
	}
	zr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return 0, nil, time.Time{}, false, err
	}
	body, err = io.ReadAll(zr)
	if err != nil {
		return 0, nil, time.Time{}, false, err
	}
	return int(row[0].int()), body, time.UnixMilli(row[2].int()), true, nil
}

// Put stores an answer until expires, gzip-compressed.
func (d *DB) Put(ctx context.Context, key string, status int, body []byte, expires time.Time) error {
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestSpeed)
	zw.Write(body)
	zw.Close()
	_, err := d.exec(ctx, stmt{
		SQL: `INSERT INTO api_cache (key, status, body, expires) VALUES (?, ?, ?, ?)
			ON CONFLICT (key) DO UPDATE SET status = excluded.status, body = excluded.body, expires = excluded.expires`,
		Args: []value{text(key), integer(int64(status)), blob(buf.Bytes()), integer(expires.UnixMilli())},
	})
	return err
}

// Prune deletes up to n expired answers and reports how many went.
func (d *DB) Prune(ctx context.Context, n int) (int, error) {
	rows, err := d.exec(ctx,
		stmt{SQL: `DELETE FROM api_cache WHERE key IN (SELECT key FROM api_cache WHERE expires < ? LIMIT ?)`,
			Args: []value{integer(time.Now().UnixMilli()), integer(int64(n))}},
		stmt{SQL: `SELECT changes()`},
	)
	if err != nil || len(rows[1]) == 0 || len(rows[1][0]) == 0 {
		return 0, err
	}
	return int(rows[1][0][0].int()), nil
}
