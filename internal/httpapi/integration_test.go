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
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

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
