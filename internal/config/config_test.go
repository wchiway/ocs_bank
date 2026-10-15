package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		ok         bool
	}{
		{"valid", `{"model":"test","base_url":"https://example.com/v1"}`, true},
		{"messages", `{"protocol":"message","model":"test","base_url":"https://example.com/v1"}`, true},
		{"response reasoning", `{"protocol":"response","reasoning_effort":"low","model":"test","base_url":"https://example.com/v1"}`, true},
		{"invalid reasoning", `{"protocol":"response","reasoning_effort":"typo","model":"test","base_url":"https://example.com/v1"}`, false},
		{"chat reasoning rejected", `{"protocol":"chat","reasoning_effort":"low","model":"test","base_url":"https://example.com/v1"}`, false},
		{"messages reasoning rejected", `{"protocol":"messages","reasoning_effort":"low","model":"test","base_url":"https://example.com/v1"}`, false},
		{"removed key setting", `{"model":"test","base_url":"https://example.com/v1","api_key_env":"TEST_KEY"}`, false},
		{"inline key rejected", `{"model":"test","base_url":"https://example.com/v1","api_key":"secret"}`, false},
		{"public", `{"listen":"0.0.0.0:5000","model":"test","base_url":"https://example.com/v1"}`, false},
		{"unknown", `{"typo":true}`, false}, {"trailing", `{} {}`, false},
		{"plaintext", `{"model":"test","base_url":"http://example.com/v1"}`, false},
		{"bad endpoint", `{"endpoint_path":"//evil/path","model":"test","base_url":"https://example.com/v1"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			p := filepath.Join(dir, "config.json")
			if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("AI_API_KEY=dummy\n"), 0600); err != nil {
				t.Fatal(err)
			}
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

func TestDotEnvSecrets(t *testing.T) {
	t.Setenv("AI_API_KEY", "process-key-must-not-be-used")
	t.Setenv("TEST_ACCESS", "process-access")
	for _, tc := range []struct {
		name                string
		contents            *string
		wantKey, wantAccess string
		wantError           bool
	}{
		{"missing file", nil, "", "", true},
		{"missing key", str("OTHER_KEY=x\n"), "", "", true},
		{"empty key", str("AI_API_KEY=\n"), "", "", true},
		{"blank key", str("AI_API_KEY='  '\n"), "", "", true},
		{"quoted and comment", str("# credentials\nexport AI_API_KEY='file#key' # comment\nTEST_ACCESS=file-access\n"), "file#key", "file-access", false},
		{"process token fallback", str("AI_API_KEY=file-key\n"), "file-key", "process-access", false},
		{"explicit empty token", str("AI_API_KEY=file-key\nTEST_ACCESS=\n"), "", "", true},
		{"malformed secret", str("AI_API_KEY=\"do-not-leak-this-value\n"), "", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "settings.json")
			if err := os.WriteFile(path, []byte(`{"model":"test","base_url":"https://example.com/v1","access_token_env":"TEST_ACCESS"}`), 0600); err != nil {
				t.Fatal(err)
			}
			if tc.contents != nil {
				if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(*tc.contents), 0600); err != nil {
					t.Fatal(err)
				}
			}
			c, err := Load(path)
			if tc.wantError {
				if err == nil {
					t.Fatal("expected configuration error")
				}
				if strings.Contains(err.Error(), "do-not-leak-this-value") {
					t.Fatal("parser leaked secret")
				}
			} else if err != nil || c.APIKey != tc.wantKey || c.AccessToken != tc.wantAccess {
				t.Fatal("incorrect dotenv credentials or error", err)
			}
			if os.Getenv("AI_API_KEY") != "process-key-must-not-be-used" || os.Getenv("TEST_ACCESS") != "process-access" {
				t.Fatal("loading dotenv modified process environment")
			}
		})
	}
}
func str(s string) *string { return &s }
