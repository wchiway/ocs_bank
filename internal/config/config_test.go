package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	t.Setenv("TEST_KEY", "dummy")
	for _, tc := range []struct {
		name, body string
		ok         bool
	}{
		{"valid", `{"model":"test","base_url":"https://example.com/v1","api_key_env":"TEST_KEY"}`, true},
		{"messages", `{"protocol":"message","model":"test","base_url":"https://example.com/v1","api_key_env":"TEST_KEY"}`, true},
		{"missing key", `{"model":"test","base_url":"https://example.com/v1","api_key_env":"MISSING_OCS_TEST_KEY"}`, false},
		{"public", `{"listen":"0.0.0.0:5000","model":"test","base_url":"https://example.com/v1","api_key_env":"TEST_KEY"}`, false},
		{"unknown", `{"typo":true}`, false}, {"trailing", `{} {}`, false},
		{"plaintext", `{"model":"test","base_url":"http://example.com/v1","api_key_env":"TEST_KEY"}`, false},
		{"bad endpoint", `{"endpoint_path":"//evil/path","model":"test","base_url":"https://example.com/v1","api_key_env":"TEST_KEY"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(p, []byte(tc.body), 0600); err != nil {
				t.Fatal(err)
			}
			_, err := Load(p)
			if (err == nil) != tc.ok {
				t.Fatalf("ok=%v err=%v", tc.ok, err)
			}
		})
	}
}
