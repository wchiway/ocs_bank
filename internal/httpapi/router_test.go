package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/wchiway/ocs_bank/internal/question"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"
)

type fake struct {
	result question.Result
	err    error
	calls  int
}

func (f *fake) Query(context.Context, question.Question) (question.Result, error) {
	f.calls++
	return f.result, f.err
}
func TestContract(t *testing.T) {
	for _, tc := range []struct {
		name, path   string
		result       question.Result
		err          error
		status, code int
	}{
		{"answer", "/api/query?title=hello", question.Result{Answer: "yes"}, nil, 200, 1},
		{"cache", "/api/query?title=hello", question.Result{Answer: "yes", Cached: true}, nil, 200, 1},
		{"missing", "/api/query", question.Result{}, nil, 400, 0},
		{"duplicate", "/api/query?title=a&title=b", question.Result{}, nil, 400, 0},
		{"bad encoding", "/api/query?title=%zz", question.Result{}, nil, 400, 0},
		{"not found", "/api/query?title=x", question.Result{}, question.ErrNotFound, 200, 0},
		{"timeout", "/api/query?title=x", question.Result{}, context.DeadlineExceeded, 504, 0},
		{"upstream", "/api/query?title=x", question.Result{}, errors.New("secret must not leak"), 502, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fake{result: tc.result, err: tc.err}
			h := New(f, Options{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("GET", tc.path, nil))
			if w.Code != tc.status {
				t.Fatalf("%d %s", w.Code, w.Body)
			}
			var got response
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if tc.status == 200 {
				if !got.Success || got.Data == nil || got.Data.Code != tc.code {
					t.Fatalf("%+v", got)
				}
				if tc.code == 0 && got.Data.Data != nil {
					t.Fatal("missing answer must be null")
				}
				if tc.result.Cached && got.Data.Source != "public" {
					t.Fatal("cache source")
				}
			}
			if tc.status == 400 && f.calls != 0 {
				t.Fatal("invalid request reached service")
			}
			if got.Error == "secret must not leak" {
				t.Fatal("leaked internal error")
			}
		})
	}
}
func TestAuthRateAndHealth(t *testing.T) {
	f := &fake{result: question.Result{Answer: "x"}}
	h := New(f, Options{Token: "test-token", RequestsPerMinute: 1})
	for i, status := range []int{401, 200, 429} {
		r := httptest.NewRequest("GET", "/api/query?title=x", nil)
		if i > 0 {
			r.Header.Set("Authorization", "Bearer test-token")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("got %d want %d", w.Code, status)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/healthz", nil))
	if w.Code != 200 || f.calls != 1 {
		t.Fatal("health must not call provider")
	}
}
