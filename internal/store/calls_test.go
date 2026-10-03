package store

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/wchiway/ocs_bank/internal/config"
	"github.com/wchiway/ocs_bank/internal/provider"
	"github.com/wchiway/ocs_bank/internal/question"
)

func TestCallsSurviveCacheOverwrite(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"{\"answer\":\"yes\"}"}}],"usage":{"prompt_tokens":3,"completion_tokens":2}}`)
	}))
	defer up.Close()
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "bank.db"), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c, err := provider.NewObserved(config.Config{Listen: "127.0.0.1:5000", Database: "unused", Protocol: "chat", BaseURL: up.URL, Model: "test", APIKey: "synthetic", TimeoutSeconds: 2, MaxConcurrent: 1, MaxPending: 2, MaxTokens: 128, RequestsPerMinute: 120, CacheTTLHours: 1}, s, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	svc, err := question.NewService(ctx, s, c, question.ServiceOptions{Namespace: c.Namespace(), Timeout: 2 * time.Second, MaxConcurrent: 1, MaxPending: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	q := question.Question{Title: "q"}
	for range 2 {
		if _, err = svc.Query(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.Invalidate(ctx, q.Key(c.Namespace())); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Query(ctx, q); err != nil {
		t.Fatal(err)
	}
	var count, input, answers int
	if err = s.db.QueryRow(`SELECT count(*),sum(input_tokens) FROM upstream_calls`).Scan(&count, &input); err != nil || count != 2 || input != 6 {
		t.Fatal(count, input, err)
	}
	if err = s.db.QueryRow(`SELECT count(*) FROM answers`).Scan(&answers); err != nil || answers != 1 {
		t.Fatal(answers, err)
	}
}
