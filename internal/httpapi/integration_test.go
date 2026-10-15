package httpapi_test

import (
	"context"
	"encoding/json"
	"github.com/wchiway/ocs_bank/internal/config"
	"github.com/wchiway/ocs_bank/internal/httpapi"
	"github.com/wchiway/ocs_bank/internal/provider"
	"github.com/wchiway/ocs_bank/internal/question"
	"github.com/wchiway/ocs_bank/internal/store"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestResponsesOutputLimitDoesNotCacheAndCanRecover(t *testing.T) {
	var calls atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			MaxTokens int `json:"max_output_tokens"`
			Reasoning struct {
				Effort string `json:"effort"`
			} `json:"reasoning"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
			return
		}
		calls.Add(1)
		if payload.MaxTokens == 1024 && payload.Reasoning.Effort == "" {
			_, _ = w.Write([]byte(`{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output":[{"type":"reasoning","summary":[]}],"usage":{"input_tokens":12,"output_tokens":1024}}`))
			return
		}
		if payload.MaxTokens != 4096 || payload.Reasoning.Effort != "low" {
			t.Errorf("unexpected generation settings: %+v", payload)
		}
		_, _ = w.Write([]byte(`{"status":"completed","output":[{"type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"{\"answer\":\"apple\"}"}]}]}`))
	}))
	defer up.Close()
	cfg := config.Config{Listen: "127.0.0.1:5000", Database: filepath.Join(t.TempDir(), "bank.db"), Protocol: "response", BaseURL: up.URL, Model: "synthetic-reasoning-model", APIKey: "dummy", TimeoutSeconds: 2, MaxConcurrent: 1, MaxPending: 4, MaxTokens: 1024, RequestsPerMinute: 120, CacheTTLHours: 1}
	db, err := store.Open(context.Background(), cfg.Database, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	newHandler := func(c config.Config) http.Handler {
		t.Helper()
		ai, err := provider.New(c)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(ai.Close)
		svc, err := question.NewService(context.Background(), db, ai, question.ServiceOptions{Namespace: ai.Namespace(), Timeout: 2 * time.Second, MaxConcurrent: 1, MaxPending: 4})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(svc.Close)
		return httpapi.New(svc, httpapi.Options{})
	}
	path := "/ocs/query?" + url.Values{"title": {"pick"}, "type": {"single"}, "options": {"A. apple\nB. pear"}}.Encode()
	request := func(h http.Handler) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		return w
	}
	h := newHandler(cfg)
	for range 2 {
		w := request(h)
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"error_code":"output_limit"`) || strings.Contains(w.Body.String(), `"data":`) {
			t.Fatal(w.Code, w.Body)
		}
	}
	if calls.Load() != 2 {
		t.Fatal("incomplete answer was cached", calls.Load())
	}
	cfg.ReasoningEffort, cfg.MaxTokens = "low", 4096
	h = newHandler(cfg)
	for range 2 {
		w := request(h)
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"data":"apple"`) {
			t.Fatal(w.Code, w.Body)
		}
	}
	if calls.Load() != 3 {
		t.Fatal("complete answer was not cached", calls.Load())
	}
}

func TestQueryWithRealSQLiteAndProvider(t *testing.T) {
	for _, tc := range []struct{ protocol, body string }{
		{"chat", `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"{\"answer\":\"yes\"}"}}]}`},
		{"message", `{"type":"message","role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":"{\"answer\":\"yes\"}"}]}`},
		{"response", `{"status":"completed","output":[{"type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"{\"answer\":\"yes\"}"}]}]}`},
	} {
		t.Run(tc.protocol, func(t *testing.T) {
			var calls atomic.Int32
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); _, _ = w.Write([]byte(tc.body)) }))
			defer up.Close()
			c := config.Config{Listen: "127.0.0.1:5000", Database: filepath.Join(t.TempDir(), "bank.db"), Protocol: tc.protocol, BaseURL: up.URL + "/v1", Model: "test", APIKey: "fake", TimeoutSeconds: 2, MaxConcurrent: 2, MaxPending: 10, MaxTokens: 100, RequestsPerMinute: 120, CacheTTLHours: 1}
			ai, err := provider.New(c)
			if err != nil {
				t.Fatal(err)
			}
			defer ai.Close()
			db, err := store.Open(context.Background(), c.Database, time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			svc, err := question.NewService(context.Background(), db, ai, question.ServiceOptions{Namespace: ai.Namespace(), Timeout: 2 * time.Second, MaxConcurrent: 2, MaxPending: 10})
			if err != nil {
				t.Fatal(err)
			}
			defer svc.Close()
			h := httpapi.New(svc, httpapi.Options{})
			for i, path := range []string{"/api/query?title=q&options=a+b", "/api/query?title=q&options=a+b", "/api/query?title=q&options=c+d"} {
				w := httptest.NewRecorder()
				h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
				var got struct {
					Success bool `json:"success"`
					Data    struct {
						Code   int    `json:"code"`
						Data   string `json:"data"`
						Source string `json:"source"`
					} `json:"data"`
				}
				if err = json.Unmarshal(w.Body.Bytes(), &got); err != nil {
					t.Fatal(err)
				}
				if w.Code != 200 || !got.Success || got.Data.Code != 1 || got.Data.Data != "yes" {
					t.Fatal(w.Code, w.Body)
				}
				if i == 1 && got.Data.Source != "public" {
					t.Fatal("not cached")
				}
			}
			if calls.Load() != 2 {
				t.Fatal("cache identity", calls.Load())
			}
		})
	}
}
