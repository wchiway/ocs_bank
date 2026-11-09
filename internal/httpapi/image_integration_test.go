package httpapi_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wchiway/ocs_bank/internal/config"
	"github.com/wchiway/ocs_bank/internal/httpapi"
	"github.com/wchiway/ocs_bank/internal/legacy"
	"github.com/wchiway/ocs_bank/internal/provider"
	"github.com/wchiway/ocs_bank/internal/question"
	"github.com/wchiway/ocs_bank/internal/store"
)

func integrationImage(t *testing.T, seed uint8) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.SetRGBA(0, 0, color.RGBA{R: seed, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

func TestImageQueryWithSQLiteAndAllProviders(t *testing.T) {
	t.Parallel()
	for _, protocol := range []string{"chat", "message", "response"} {
		t.Run(protocol, func(t *testing.T) {
			var calls atomic.Int32
			var fail atomic.Bool
			fail.Store(true)
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := calls.Add(1)
				_, _ = io.Copy(io.Discard, r.Body)
				if fail.Load() {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte("private unsupported image response"))
					return
				}
				answer, _ := json.Marshal(map[string]string{"answer": fmt.Sprint(n)})
				switch protocol {
				case "chat":
					_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": string(answer)}}}})
				case "message":
					_ = json.NewEncoder(w).Encode(map[string]any{"type": "message", "role": "assistant", "stop_reason": "end_turn", "content": []any{map[string]any{"type": "text", "text": string(answer)}}})
				case "response":
					_ = json.NewEncoder(w).Encode(map[string]any{"status": "completed", "output": []any{map[string]any{"type": "message", "status": "completed", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": string(answer)}}}}})
				}
			}))
			defer up.Close()
			cfg := config.Config{Listen: "127.0.0.1:5000", Database: filepath.Join(t.TempDir(), "images.db"), Protocol: protocol, BaseURL: up.URL, Model: "synthetic-vision-model", APIKey: "dummy", TimeoutSeconds: 2, MaxConcurrent: 2, MaxPending: 4, MaxTokens: 128, RequestsPerMinute: 120, CacheTTLHours: 1}
			client, err := provider.New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			db, err := store.Open(context.Background(), cfg.Database, time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			text := question.Question{Title: "same image URL", Type: "completion"}
			if _, err := db.Import(context.Background(), []legacy.Candidate{{Question: text, Answer: "reviewed text"}}); err != nil {
				t.Fatal(err)
			}
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			svc, err := question.NewService(context.Background(), db, client, question.ServiceOptions{Namespace: client.Namespace(), Timeout: 2 * time.Second, MaxConcurrent: 2, MaxPending: 4, Logger: logger})
			if err != nil {
				t.Fatal(err)
			}
			defer svc.Close()
			h := httpapi.New(svc, httpapi.Options{Logger: logger})
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("GET", "/api/query?"+url.Values{"title": {text.Title}, "type": {text.Type}}.Encode(), nil))
			if !strings.Contains(w.Body.String(), "reviewed text") || calls.Load() != 0 {
				t.Fatal("legacy reviewed text no longer works", w.Body)
			}
			query := func(imageURL string) *httptest.ResponseRecorder {
				t.Helper()
				body, _ := json.Marshal(map[string]any{"title": text.Title, "type": text.Type, "images": []string{imageURL}, "suggestion_title": "look at [图片1]"})
				r := httptest.NewRequest("POST", "/ocs/query", bytes.NewReader(body))
				r.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				return w
			}
			first, second := integrationImage(t, 1), integrationImage(t, 2)
			w = query(first)
			if w.Code != 200 || !strings.Contains(w.Body.String(), `"error_code":"protocol"`) || strings.Contains(w.Body.String(), "private") || calls.Load() != 1 {
				t.Fatal("image failure not reported safely", w.Body)
			}
			fail.Store(false)
			for _, tc := range []struct {
				image, answer string
				calls         int32
			}{{first, "2", 2}, {first, "2", 2}, {second, "3", 3}, {second, "3", 3}} {
				w = query(tc.image)
				if w.Code != 200 || !strings.Contains(w.Body.String(), `"data":"`+tc.answer+`"`) || calls.Load() != tc.calls {
					t.Fatal("image identity, recovery or cache failed", w.Body, calls.Load())
				}
			}
		})
	}
}
