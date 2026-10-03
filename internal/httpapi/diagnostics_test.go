package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wchiway/ocs_bank/internal/question"
	"github.com/wchiway/ocs_bank/internal/telemetry"
)

func TestDiagnosticsAreAuthenticatedAndRedacted(t *testing.T) {
	var logs bytes.Buffer
	m := &telemetry.Metrics{}
	f := &fake{result: question.Result{Answer: "secret-answer", Cached: true, Reviewed: true}}
	h := New(f, Options{Token: "secret-token", Metrics: m, Logger: slog.New(slog.NewJSONHandler(&logs, nil))})
	request := func(path string, auth bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", path, nil)
		if auth {
			r.Header.Set("Authorization", "Bearer secret-token")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := request("/api/stats", false); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w := request("/api/query?title=secret-question", true); w.Code != 200 {
		t.Fatal(w.Code)
	}
	f.result = question.Result{}
	f.err = errors.Join(question.ErrStorage, errors.New("secret-database-error"))
	if w := request("/api/query?title=secret-question", true); w.Code != 503 || strings.Contains(w.Body.String(), "secret") {
		t.Fatal(w.Code, w.Body)
	}
	if w := request("/api/query", true); w.Code != 400 {
		t.Fatal(w.Code)
	}
	if w := request("/api/query?title=q", false); w.Code != 401 {
		t.Fatal(w.Code)
	}
	w := request("/api/stats", true)
	var stats struct {
		Scope    string           `json:"scope"`
		Counters map[string]int64 `json:"counters"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &stats); err != nil || stats.Scope != "process" {
		t.Fatal(stats, err)
	}
	for key, want := range map[string]int64{"queries_total": 4, "query_layer_reviewed": 1, "query_result_storage": 1, "query_result_invalid": 1, "query_result_unauthorized": 1} {
		if stats.Counters[key] != want {
			t.Fatal(key, stats.Counters)
		}
	}
	if strings.Contains(logs.String(), "secret") || strings.Contains(w.Body.String(), "secret") {
		t.Fatal("sensitive diagnostics")
	}
	if !strings.Contains(logs.String(), `"result":"storage"`) || !strings.Contains(logs.String(), `"layer":"reviewed"`) {
		t.Fatal(logs.String())
	}
}

func TestReadinessIsBoundedAndDoesNotCallProvider(t *testing.T) {
	for _, tc := range []struct {
		name   string
		check  func(context.Context) error
		status int
	}{
		{"healthy", func(context.Context) error { return nil }, 200},
		{"closed", func(context.Context) error { return context.Canceled }, 503},
		{"deadline", func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }, 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fake{}
			h := New(f, Options{Ready: tc.check})
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("GET", "/readyz", nil))
			if w.Code != tc.status || f.calls != 0 {
				t.Fatal(w.Code, f.calls)
			}
			w = httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("GET", "/healthz", nil))
			if w.Code != 200 {
				t.Fatal(w.Code)
			}
		})
	}
}
