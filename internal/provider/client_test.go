package provider

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/wchiway/ocs_bank/internal/config"
	"github.com/wchiway/ocs_bank/internal/question"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const chatOK = `{"model":"test","choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"{\"answer\":\"yes\"}"}}],"usage":{"prompt_tokens":3,"completion_tokens":2}}`
const messagesOK = `{"type":"message","role":"assistant","model":"test","stop_reason":"end_turn","content":[{"type":"thinking","thinking":"hidden"},{"type":"text","text":"{\"answer\":"},{"type":"text","text":"\"yes\"}"}],"usage":{"input_tokens":3,"output_tokens":2}}`
const responsesOK = `{"status":"completed","model":"test","output":[{"type":"reasoning"},{"type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"{\"answer\":\"yes\"}"}]}],"usage":{"input_tokens":3,"output_tokens":2}}`

func testConfig(base, protocol string) config.Config {
	return config.Config{Listen: "127.0.0.1:5000", Database: "unused", Protocol: protocol, BaseURL: base, Model: "test", APIKey: "dummy-secret", TimeoutSeconds: 2, MaxConcurrent: 2, MaxPending: 4, MaxTokens: 128, RequestsPerMinute: 120, CacheTTLHours: 1}
}
func TestProtocols(t *testing.T) {
	for _, tc := range []struct{ protocol, path, body string }{{"chat", "/gateway/v1/chat/completions", chatOK}, {"message", "/gateway/v1/messages", messagesOK}, {"response", "/gateway/v1/responses", responsesOK}} {
		t.Run(tc.protocol, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.URL.Path != tc.path {
					t.Errorf("request %s %s", r.Method, r.URL.Path)
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if body["model"] != "test" || body["stream"] != false {
					t.Error("model/stream")
				}
				switch tc.protocol {
				case "message":
					if r.Header.Get("x-api-key") != "dummy-secret" || r.Header.Get("anthropic-version") == "" || body["system"] == nil || body["max_tokens"] != float64(128) {
						t.Error("message schema")
					}
				case "response":
					if body["input"] == nil || body["instructions"] == nil || body["max_output_tokens"] != float64(128) || body["store"] != false {
						t.Error("response schema")
					}
				case "chat":
					if body["messages"] == nil || body["max_tokens"] != float64(128) {
						t.Error("chat schema")
					}
				}
				if tc.protocol != "message" && r.Header.Get("Authorization") != "Bearer dummy-secret" {
					t.Error("bearer")
				}
				_, _ = w.Write([]byte(tc.body))
			}))
			defer s.Close()
			c, err := New(testConfig(s.URL+"/gateway/v1/", tc.protocol))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			r, err := c.Generate(context.Background(), question.Question{Title: "x"})
			if err != nil {
				t.Fatal(err)
			}
			if r.Answer != "yes" || r.InputTokens != 3 || r.OutputTokens != 2 || r.Model != "test" {
				t.Fatalf("%+v", r)
			}
		})
	}
}
func TestMalformedAndIncomplete(t *testing.T) {
	for _, tc := range []struct {
		name, protocol, body string
		want                 error
	}{
		{"bad json", "chat", "{", question.ErrProtocol}, {"no choices", "chat", `{"choices":[]}`, question.ErrProtocol},
		{"truncated", "chat", strings.Replace(chatOK, `"stop"`, `"length"`, 1), question.ErrProtocol},
		{"refusal", "chat", strings.Replace(chatOK, `"content":`, `"refusal":"no","content":`, 1), question.ErrRefused},
		{"bad answer", "chat", strings.Replace(chatOK, `{\"answer\":\"yes\"}`, `not json`, 1), question.ErrProtocol},
		{"tool only", "message", strings.Replace(messagesOK, `"end_turn"`, `"tool_use"`, 1), question.ErrProtocol},
		{"message refusal", "message", strings.Replace(messagesOK, `"end_turn"`, `"refusal"`, 1), question.ErrRefused},
		{"incomplete", "response", strings.Replace(responsesOK, `"status":"completed"`, `"status":"incomplete"`, 1), question.ErrProtocol},
		{"response refusal", "response", strings.Replace(responsesOK, `"type":"output_text"`, `"type":"refusal"`, 1), question.ErrRefused},
		{"no output", "response", `{"status":"completed","output":[]}`, question.ErrProtocol},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(tc.body)) }))
			defer s.Close()
			c, err := New(testConfig(s.URL, tc.protocol))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			res, err := c.Generate(context.Background(), question.Question{Title: "x"})
			if !errors.Is(err, tc.want) || res.Answer != "" {
				t.Fatalf("%+v %v", res, err)
			}
		})
	}
}
func TestHTTPFailuresAndRetries(t *testing.T) {
	for _, status := range []int{401, 403, 429, 500, 400} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls atomic.Int32
			s := httptest.NewServer(http.HandlerFunc(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(status)
				_, _ = w.Write([]byte("sensitive error"))
			})))
			defer s.Close()
			cfg := testConfig(s.URL, "chat")
			cfg.MaxRetries = 1
			c, _ := New(cfg)
			defer c.Close()
			_, err := c.Generate(context.Background(), question.Question{Title: "x"})
			want := question.ErrProtocol
			n := int32(1)
			switch status {
			case 401, 403:
				want = question.ErrAuth
			case 429:
				want = question.ErrRateLimit
				n = 2
			case 500:
				want = question.ErrUnavailable
				n = 2
			}
			if !errors.Is(err, want) || calls.Load() != n {
				t.Fatalf("calls=%d err=%v", calls.Load(), err)
			}
		})
	}
}
func TestRetryCancellationAndRedirect(t *testing.T) {
	t.Run("retry budget", func(t *testing.T) {
		var calls atomic.Int32
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			w.Header().Set("Retry-After", "3600")
			w.WriteHeader(429)
		}))
		defer s.Close()
		cfg := testConfig(s.URL, "response")
		cfg.MaxRetries = 2
		c, _ := New(cfg)
		defer c.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel()
		_, err := c.Generate(ctx, question.Question{Title: "x"})
		if !errors.Is(err, context.DeadlineExceeded) || calls.Load() != 1 {
			t.Fatal(err, calls.Load())
		}
	})
	t.Run("redirect", func(t *testing.T) {
		var hits atomic.Int32
		target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
		defer target.Close()
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) }))
		defer s.Close()
		c, _ := New(testConfig(s.URL, "chat"))
		defer c.Close()
		_, err := c.Generate(context.Background(), question.Question{Title: "x"})
		if err == nil || hits.Load() != 0 {
			t.Fatal("redirect followed")
		}
	})
	t.Run("timeout", func(t *testing.T) {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.Copy(io.Discard, r.Body); <-r.Context().Done() }))
		defer s.Close()
		c, _ := New(testConfig(s.URL, "message"))
		defer c.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel()
		_, err := c.Generate(ctx, question.Question{Title: "x"})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
	})
}
func TestEndpointAndTokenOverride(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/custom/chat" {
			t.Error(r.URL.Path)
		}
		var b map[string]any
		_ = json.NewDecoder(r.Body).Decode(&b)
		if b["max_completion_tokens"] != float64(128) || b["max_tokens"] != nil {
			t.Error(b)
		}
		_, _ = w.Write([]byte(chatOK))
	}))
	defer s.Close()
	cfg := testConfig(s.URL+"/v1", "chat")
	cfg.EndpointPath = "/custom/chat"
	cfg.ChatTokenParameter = "max_completion_tokens"
	c, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err = c.Generate(context.Background(), question.Question{Title: "x"}); err != nil {
		t.Fatal(err)
	}
}
func TestOversizedBody(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", maxResponseBytes+1)))
	}))
	defer s.Close()
	c, _ := New(testConfig(s.URL, "chat"))
	defer c.Close()
	_, err := c.Generate(context.Background(), question.Question{Title: "x"})
	if !errors.Is(err, question.ErrProtocol) {
		t.Fatal(err)
	}
}
