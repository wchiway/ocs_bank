package provider

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wchiway/ocs_bank/internal/question"
	"github.com/wchiway/ocs_bank/internal/telemetry"
)

type records struct {
	calls []telemetry.Call
	fail  bool
}

func (r *records) RecordCall(ctx context.Context, c telemetry.Call) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	r.calls = append(r.calls, c)
	if r.fail {
		return errors.New("synthetic write failure")
	}
	return nil
}

func TestAttemptRecords(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		category   string
		known      bool
	}{
		{"success", chatOK, 200, "ok", true},
		{"auth", "private upstream body", 401, "auth", false},
		{"malformed", "{", 200, "protocol", false},
		{"typed invalid", `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"{\"answer\":\"A\"}"}}]}`, 200, "answer_invalid", false},
		{"truncated", strings.Replace(chatOK, `"stop"`, `"length"`, 1), 200, "output_limit", true},
		{"answer format", strings.Replace(chatOK, `{\"answer\":\"yes\"}`, `not JSON`, 1), 200, "answer_format", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); io.WriteString(w, tc.body) }))
			defer up.Close()
			r := &records{fail: true}
			c, err := NewObserved(testConfig(up.URL, "chat"), r, slog.New(slog.NewTextHandler(io.Discard, nil)))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			q := question.Question{Title: "q"}
			if tc.name == "typed invalid" {
				q.Type = "single"
				q.Options = "A. apple\nB. pear"
			}
			result, err := c.Generate(context.Background(), q)
			if question.ErrorCategory(err) != tc.category {
				t.Fatal(err)
			}
			if tc.category == "ok" && result.Answer != "yes" {
				t.Fatal("record failure lost answer")
			}
			if len(r.calls) != 1 {
				t.Fatal(r.calls)
			}
			call := r.calls[0]
			if call.HTTPStatus != tc.status || call.Result != tc.category || call.UsageKnown != tc.known || call.Attempt != 1 {
				t.Fatal(call)
			}
		})
	}
}

func TestRetryAndCancellationRecords(t *testing.T) {
	count := 0
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		if count == 1 {
			w.WriteHeader(503)
		} else {
			io.WriteString(w, chatOK)
		}
	}))
	defer up.Close()
	r := &records{}
	cfg := testConfig(up.URL, "chat")
	cfg.MaxRetries = 1
	c, err := NewObserved(cfg, r, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err = c.Generate(context.Background(), question.Question{Title: "q"}); err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 2 || r.calls[0].Result != "unavailable" || r.calls[1].Attempt != 2 || r.calls[1].InputTokens != 3 {
		t.Fatal(r.calls)
	}
	blocked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.Copy(io.Discard, r.Body); <-r.Context().Done() }))
	defer blocked.Close()
	r = &records{}
	c2, err := NewObserved(testConfig(blocked.URL, "chat"), r, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err = c2.Generate(ctx, question.Question{Title: "q"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if len(r.calls) != 1 || r.calls[0].Result != "timeout" {
		t.Fatal("canceled attempt was not persisted", r.calls)
	}
}

func TestUsagePresence(t *testing.T) {
	for _, tc := range []struct {
		raw, protocol string
		known         bool
	}{
		{`{}`, "messages", false}, {`{"usage":{"input_tokens":0,"output_tokens":0}}`, "messages", true},
		{`{"usage":{"input_tokens":1}}`, "responses", false}, {`{"usage":{"input_tokens":-1,"output_tokens":2}}`, "responses", false},
		{`{"usage":{"input_tokens":1.5,"output_tokens":2}}`, "responses", false},
		{`{"usage":{"prompt_tokens":3,"completion_tokens":2}}`, "chat_completions", true},
	} {
		_, _, known := usage([]byte(tc.raw), tc.protocol)
		if known != tc.known {
			t.Fatal(tc)
		}
	}
}
