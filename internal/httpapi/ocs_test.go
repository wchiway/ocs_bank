package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wchiway/ocs_bank/internal/question"
)

func TestOCSBusinessErrors(t *testing.T) {
	for _, tc := range []struct {
		err    error
		code   string
		status int
	}{
		{question.ErrOutputLimit, "output_limit", 502},
		{question.ErrAnswerFormat, "answer_format", 502},
		{question.ErrAnswerInvalid, "answer_invalid", 502},
		{question.ErrProtocol, "protocol", 502},
		{question.ErrAuth, "auth", 502},
		{question.ErrStorage, "storage", 503},
		{question.ErrRateLimit, "rate_limit", 503},
		{question.ErrBusy, "busy", 503},
		{context.DeadlineExceeded, "timeout", 504},
		{context.Canceled, "canceled", 408},
		{question.ErrInvalid, "invalid", 400},
		{question.ErrUnavailable, "unavailable", 502},
	} {
		t.Run(tc.code, func(t *testing.T) {
			f := &fake{err: errors.Join(tc.err, errors.New("private upstream answer and key"))}
			h := New(f, Options{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
			for _, path := range []string{"/api/query", "/ocs/query"} {
				w := httptest.NewRecorder()
				h.ServeHTTP(w, httptest.NewRequest("GET", path+"?title=q", nil))
				wantStatus := tc.status
				if path == "/ocs/query" {
					wantStatus = http.StatusOK
				}
				var got response
				if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
					t.Fatal(err)
				}
				if w.Code != wantStatus || got.Success || got.ErrorCode != tc.code || got.Error == "" || got.Data != nil {
					t.Fatalf("%s: %d %s", path, w.Code, w.Body)
				}
				if strings.Contains(w.Body.String(), "private") || w.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("unsafe error response")
				}
			}
		})
	}
}

func TestOCSProbeAndAccessControl(t *testing.T) {
	f := &fake{result: question.Result{Answer: "yes"}}
	h := New(f, Options{Token: "test-token", RequestsPerMinute: 1})
	request := func(method, query string, auth bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/ocs/query"+query, nil)
		if auth {
			r.Header.Set("Authorization", "Bearer test-token")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := request("HEAD", "?t=1", false); w.Code != 200 || w.Body.Len() != 0 || f.calls != 0 {
		t.Fatal("probe must be public and must not query", w.Code, w.Body, f.calls)
	}
	w := request("GET", "?title=q", false)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"error_code":"unauthorized"`) || f.calls != 0 {
		t.Fatal("unauthorized caller reached service", w.Code, w.Body)
	}
	w = request("GET", "?title=q", true)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"code":1`) || f.calls != 1 {
		t.Fatal("probe or unauthorized caller consumed rate limit", w.Code, w.Body)
	}
	r := httptest.NewRequest("GET", "/api/query?title=q", nil)
	r.Header.Set("Authorization", "Bearer test-token")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 429 || f.calls != 1 {
		t.Fatal("API and OCS must share the limiter", w.Code, w.Body)
	}
	w = request("GET", "?title=q", true)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"error_code":"rate_limit"`) || w.Header().Get("Retry-After") == "" || f.calls != 1 {
		t.Fatal("OCS rate limit response", w.Code, w.Body)
	}
}

func TestOCSAnswersAndMissingResults(t *testing.T) {
	for _, tc := range []struct {
		name string
		fake fake
		code int
	}{
		{"generated", fake{result: question.Result{Answer: "yes"}}, 1},
		{"cached", fake{result: question.Result{Answer: "yes", Cached: true}}, 1},
		{"missing", fake{err: question.ErrNotFound}, 0},
		{"refused", fake{err: question.ErrRefused}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := New(&tc.fake, Options{})
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("GET", "/ocs/query?title=q", nil))
			var got response
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if w.Code != 200 || !got.Success || got.Data == nil || got.Data.Code != tc.code {
				t.Fatal(w.Code, w.Body)
			}
			if tc.code == 0 && got.Data.Data != nil {
				t.Fatal("error text returned as an answer")
			}
		})
	}
	f := &fake{}
	w := httptest.NewRecorder()
	New(f, Options{}).ServeHTTP(w, httptest.NewRequest("GET", "/ocs/query?title=%zz", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"error_code":"invalid"`) || f.calls != 0 {
		t.Fatal("invalid OCS query reached service", w.Code, w.Body)
	}
}
