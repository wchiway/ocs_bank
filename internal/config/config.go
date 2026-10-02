package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
	"strings"
	"time"
)

type Config struct {
	ChatTokenParameter string `json:"chat_token_parameter"`
	Listen             string `json:"listen"`
	Database           string `json:"database"`
	Protocol           string `json:"protocol"`
	BaseURL            string `json:"base_url"`
	EndpointPath       string `json:"endpoint_path"`
	Model              string `json:"model"`
	AccessTokenEnv     string `json:"access_token_env"`
	TimeoutSeconds     int    `json:"timeout_seconds"`
	MaxConcurrent      int    `json:"max_concurrent"`
	MaxPending         int    `json:"max_pending"`
	MaxRetries         int    `json:"max_retries"`
	MaxTokens          int    `json:"max_tokens"`
	RequestsPerMinute  int    `json:"requests_per_minute"`
	CacheTTLHours      int    `json:"cache_ttl_hours"`
	APIKey             string `json:"-"`
	AccessToken        string `json:"-"`
}

func Load(path string) (Config, error) {
	c := Config{Listen: "127.0.0.1:5000", Database: "question_bank.db", Protocol: "chat", TimeoutSeconds: 60, MaxConcurrent: 4, MaxPending: 64, MaxRetries: 2, MaxTokens: 1024, RequestsPerMinute: 120, CacheTTLHours: 720}
	f, err := os.Open(path)
	if err != nil {
		return c, err
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 65537))
	d.DisallowUnknownFields()
	if err = d.Decode(&c); err != nil {
		return c, fmt.Errorf("invalid configuration: %w", err)
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return c, errors.New("configuration must contain one JSON object")
	}
	secrets, err := godotenv.Read(filepath.Join(filepath.Dir(path), ".env"))
	if err != nil {
		// Parser errors can contain secret values. Never include their raw text.
		if errors.Is(err, os.ErrNotExist) {
			return c, errors.New(".env file is required beside the configuration file")
		}
		return c, errors.New("cannot read or parse .env file beside the configuration file")
	}
	c.APIKey = secrets["AI_API_KEY"]
	if strings.TrimSpace(c.APIKey) == "" {
		return c, errors.New("AI_API_KEY must be set in .env")
	}
	c.AccessToken, _ = secrets[c.AccessTokenEnv]
	if _, exists := secrets[c.AccessTokenEnv]; !exists {
		c.AccessToken = os.Getenv(c.AccessTokenEnv)
	}
	if err = c.Validate(); err != nil {
		return c, err
	}
	return c, nil
}
func (c *Config) Validate() error {
	if c.ChatTokenParameter == "" {
		c.ChatTokenParameter = "max_tokens"
	}
	if c.ChatTokenParameter != "max_tokens" && c.ChatTokenParameter != "max_completion_tokens" {
		return errors.New("invalid chat_token_parameter")
	}
	switch c.Protocol {
	case "chat", "chat_completions":
		c.Protocol = "chat_completions"
	case "message", "messages":
		c.Protocol = "messages"
	case "response", "responses":
		c.Protocol = "responses"
	default:
		return errors.New("invalid protocol")
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("base_url must be an HTTP(S) API root without credentials, query or fragment")
	}
	if u.Scheme == "http" && !loopback(u.Hostname()) {
		return errors.New("non-loopback upstream requires HTTPS")
	}
	if c.EndpointPath != "" {
		p, err := url.Parse(c.EndpointPath)
		if err != nil || !strings.HasPrefix(c.EndpointPath, "/") || p.IsAbs() || p.Host != "" || p.RawQuery != "" || p.Fragment != "" || strings.Contains(p.Path, "..") {
			return errors.New("endpoint_path must be an absolute URL path")
		}
	}
	host, _, err := net.SplitHostPort(c.Listen)
	if err != nil {
		return errors.New("listen must be host:port")
	}
	if !loopback(host) && c.AccessToken == "" {
		return errors.New("non-loopback listen requires access_token_env with a nonempty secret")
	}
	if strings.TrimSpace(c.Model) == "" || strings.TrimSpace(c.APIKey) == "" || c.Database == "" {
		return errors.New("model, database and API key are required")
	}
	if c.AccessTokenEnv != "" && c.AccessToken == "" {
		return errors.New("access token environment variable is empty")
	}
	if c.TimeoutSeconds < 1 || c.TimeoutSeconds > 600 || c.MaxConcurrent < 1 || c.MaxConcurrent > 128 || c.MaxPending < c.MaxConcurrent || c.MaxPending > 10000 || c.MaxRetries < 0 || c.MaxRetries > 5 || c.MaxTokens < 1 || c.MaxTokens > 65536 || c.RequestsPerMinute < 1 || c.CacheTTLHours < 1 || c.CacheTTLHours > 87600 {
		return errors.New("configuration limits out of range")
	}
	return nil
}
func loopback(host string) bool {
	ip := net.ParseIP(host)
	return host == "localhost" || (ip != nil && ip.IsLoopback())
}
func (c Config) Timeout() time.Duration { return time.Duration(c.TimeoutSeconds) * time.Second }
