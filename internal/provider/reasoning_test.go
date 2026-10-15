package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wchiway/ocs_bank/internal/question"
)

func TestResponsesReasoningEffort(t *testing.T) {
	for _, effort := range []string{"", "none", "minimal", "low", "medium", "high", "xhigh", "max"} {
		t.Run("effort="+effort, func(t *testing.T) {
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					return
				}
				if effort == "" {
					if _, exists := body["reasoning"]; exists {
						t.Error("unset reasoning must preserve the provider default")
					}
				} else if string(body["reasoning"]) != `{"effort":"`+effort+`"}` {
					t.Errorf("reasoning = %s", body["reasoning"])
				}
				if string(body["max_output_tokens"]) != "4096" || string(body["store"]) != "false" {
					t.Error("response output budget or storage setting lost")
				}
				_, _ = w.Write([]byte(responsesOK))
			}))
			defer up.Close()
			cfg := testConfig(up.URL, "response")
			cfg.ReasoningEffort, cfg.MaxTokens = effort, 4096
			c, err := New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if res, err := c.Generate(context.Background(), question.Question{Title: "q"}); err != nil || res.Answer != "yes" {
				t.Fatal(res, err)
			}
			base := "responses|" + c.endpoint + "|test"
			if effort != "" {
				base += "|reasoning_effort=" + effort
			}
			if c.Namespace() != base {
				t.Fatalf("cache namespace = %q; want %q", c.Namespace(), base)
			}
		})
	}
}
