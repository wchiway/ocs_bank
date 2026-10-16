package desktop

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/joho/godotenv"
	"github.com/wchiway/ocs_bank/internal/config"
)

func controller(t *testing.T) *Controller {
	t.Helper()
	c, err := New(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func saveRequest(t *testing.T, c *Controller) SaveRequest {
	t.Helper()
	s, err := c.Settings()
	if err != nil {
		t.Fatal(err)
	}
	s.Port, s.Model = freePort(t), "synthetic-model"
	return SaveRequest{Settings: s, APIKey: "synthetic-desktop-secret"}
}

func TestSettingsKeepSecretsPrivateAndPreserveKey(t *testing.T) {
	c := controller(t)
	s, err := c.Settings()
	if err != nil || s.Configured || s.HasAPIKey || s.Port != 5000 || s.MaxConcurrent != 4 {
		t.Fatal(s, err)
	}
	request := saveRequest(t, c)
	s, err = c.SaveSettings(request)
	if err != nil || !s.Configured || !s.HasAPIKey {
		t.Fatal(s, err)
	}
	assertRedacted(t, s, request.APIKey)
	assertRedacted(t, c.Status(), request.APIKey)
	body, err := os.ReadFile(filepath.Join(c.root, "config.json"))
	if err != nil || strings.Contains(string(body), request.APIKey) || strings.Contains(string(body), "api_key") {
		t.Fatal("configuration contains a key", err)
	}
	loaded, err := config.Load(filepath.Join(c.root, "config.json"))
	if err != nil || loaded.APIKey != request.APIKey || loaded.Database != filepath.Join(c.root, "question_bank.db") {
		t.Fatal("saved configuration did not round trip", err)
	}
	request.APIKey, request.Model = "", "another-synthetic-model"
	if _, err = c.SaveSettings(request); err != nil {
		t.Fatal(err)
	}
	loaded, err = config.Load(filepath.Join(c.root, "config.json"))
	if err != nil || loaded.APIKey != "synthetic-desktop-secret" || loaded.Model != request.Model {
		t.Fatal("blank key did not preserve existing key", err)
	}
	fresh, err := New(c.root, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	s, err = fresh.Settings()
	if err != nil || !s.Configured || !s.HasAPIKey {
		t.Fatal(s, err)
	}
	assertRedacted(t, s, "synthetic-desktop-secret")
}

func TestSecretSpecialCharactersRoundTrip(t *testing.T) {
	c := controller(t)
	req := saveRequest(t, c)
	req.APIKey = "synthetic-$VALUE-\\\"-\\\\-#-`"
	if _, err := c.SaveSettings(req); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(filepath.Join(c.root, "config.json"))
	if err != nil || loaded.APIKey != req.APIKey {
		t.Fatal("key with special characters did not round trip", err)
	}
}

func TestInvalidSettingsNeverReplaceSavedConfiguration(t *testing.T) {
	c := controller(t)
	req := saveRequest(t, c)
	if _, err := c.SaveSettings(SaveRequest{Settings: req.Settings}); err == nil {
		t.Fatal("accepted missing initial key")
	}
	if _, err := c.SaveSettings(req); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(c.root, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*SaveRequest){
		func(r *SaveRequest) { r.BaseURL = "http://insecure.example/v1" },
		func(r *SaveRequest) { r.Port = 0 },
		func(r *SaveRequest) { r.Port = 65536 },
		func(r *SaveRequest) { r.MaxPending = r.MaxConcurrent - 1 },
		func(r *SaveRequest) { r.ReasoningEffort = "low" },
		func(r *SaveRequest) { r.Model = "" },
	} {
		bad := req
		mutate(&bad)
		if _, err := c.SaveSettings(bad); err == nil {
			t.Fatal("accepted invalid configuration")
		}
		after, err := os.ReadFile(filepath.Join(c.root, "config.json"))
		if err != nil || string(after) != string(before) {
			t.Fatal("invalid save changed configuration", err)
		}
	}
}

func TestSettingsErrorsNeverReturnSecretParserText(t *testing.T) {
	c := controller(t)
	if err := os.WriteFile(filepath.Join(c.root, ".env"), []byte("AI_API_KEY=\"sentinel-secret-parser-text\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := c.Settings()
	if err == nil || strings.Contains(err.Error(), "sentinel-secret-parser-text") {
		t.Fatal("unsafe or missing dotenv error", err)
	}
	if err = os.WriteFile(filepath.Join(c.root, "config.json"), []byte(`{"api_key":"sentinel-secret-json-text"}`), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = c.Settings()
	if err == nil || strings.Contains(err.Error(), "sentinel-secret-json-text") {
		t.Fatal("unsafe or missing JSON error", err)
	}
}

func TestInterruptedSaveRequiresResave(t *testing.T) {
	c := controller(t)
	req := saveRequest(t, c)
	if _, err := c.SaveSettings(req); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(c.root, ".settings-pending"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if s, err := c.Settings(); err != nil || s.Configured || !s.HasAPIKey {
		t.Fatal(s, err)
	}
	if _, err := c.Start(); err == nil {
		t.Fatal("started with interrupted settings")
	}
	req.APIKey = ""
	if _, err := c.SaveSettings(req); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Start(); err != nil {
		t.Fatal(err)
	}
}

func TestFailedSaveRestoresOldKey(t *testing.T) {
	c := controller(t)
	req := saveRequest(t, c)
	if _, err := c.SaveSettings(req); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(c.root, "config.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(c.root, "config.json"), 0700); err != nil {
		t.Fatal(err)
	}
	req.APIKey = "replacement-synthetic-secret"
	if _, err := c.SaveSettings(req); err == nil {
		t.Fatal("save unexpectedly succeeded")
	}
	secrets, err := godotenv.Read(filepath.Join(c.root, ".env"))
	if err != nil || secrets["AI_API_KEY"] != "synthetic-desktop-secret" {
		t.Fatal("previous key not restored", err)
	}
}

func TestLifecycleAndCancellation(t *testing.T) {
	c := controller(t)
	started := make(chan struct{})
	canceled := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		close(started)
		<-r.Context().Done()
		close(canceled)
	}))
	defer upstream.Close()
	req := saveRequest(t, c)
	req.BaseURL, req.MaxRetries = upstream.URL, 0
	if _, err := c.SaveSettings(req); err != nil {
		t.Fatal(err)
	}
	s, err := c.Start()
	if err != nil || s.State != "running" || s.StartedAt == "" || s.URL != queryURL(req.Port) {
		t.Fatal(s, err)
	}
	firstStartedAt := s.StartedAt
	client := &http.Client{Timeout: 4 * time.Second}
	base := "http://127.0.0.1:" + strconv.Itoa(req.Port)
	resp, err := client.Get(base + "/readyz")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatal(resp.StatusCode)
	}
	if _, err = c.SaveSettings(req); err == nil {
		t.Fatal("saved while running")
	}
	queryDone := make(chan struct{})
	go func() {
		defer close(queryDone)
		resp, err := client.Get(base + "/api/query?title=synthetic-question&type=completion")
		if err == nil {
			resp.Body.Close()
		}
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("upstream did not start")
	}
	s, err = c.Stop()
	if err != nil || s.State != "stopped" || s.Counters["queries_total"] != 1 || s.StartedAt != firstStartedAt {
		t.Fatal(s, err)
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("upstream kept running after stop")
	}
	<-queryDone
	s, err = c.Start()
	if err != nil || s.Counters["queries_total"] != 0 || s.StartedAt == "" {
		t.Fatal("restart did not reset statistics", s, err)
	}
	if err = c.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = c.Start(); err == nil {
		t.Fatal("closed controller started")
	}
}

func TestConcurrentStartsAndStops(t *testing.T) {
	c := controller(t)
	if _, err := c.SaveSettings(saveRequest(t, c)); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.Start(); err != nil {
				t.Error(err)
			}
			_ = c.Status()
		}()
	}
	wg.Wait()
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.Stop(); err != nil {
				t.Error(err)
			}
			_ = c.Status()
		}()
	}
	wg.Wait()
	if c.Status().State != "stopped" {
		t.Fatal(c.Status())
	}
}

func TestPortConflictAndLocalBinding(t *testing.T) {
	c := controller(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	req := saveRequest(t, c)
	req.Port = listener.Addr().(*net.TCPAddr).Port
	if _, err = c.SaveSettings(req); err != nil {
		t.Fatal(err)
	}
	s, err := c.Start()
	if err == nil || s.State != "stopped" || s.LastError == "" {
		t.Fatal(s, err)
	}
	for _, host := range []string{"0.0.0.0", "::", "192.0.2.1"} {
		cfg := config.Defaults()
		cfg.Listen, cfg.APIKey, cfg.Model, cfg.BaseURL = net.JoinHostPort(host, "5000"), "synthetic", "test", "https://example.com"
		if err = c.validate(&cfg); err == nil {
			t.Fatal("accepted public listen", host)
		}
	}
}

func TestExportPreservesOCSHandlerAndNeverIncludesKey(t *testing.T) {
	c := controller(t)
	req := saveRequest(t, c)
	if _, err := c.SaveSettings(req); err != nil {
		t.Fatal(err)
	}
	exported, err := c.ExportOCS()
	if err != nil || exported.URL != queryURL(req.Port) {
		t.Fatal(exported.URL, err)
	}
	assertRedacted(t, exported, req.APIKey)
	var got, expected []map[string]any
	if err = json.Unmarshal([]byte(exported.JSON), &got); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile("../../examples/ocs.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(body, &expected); err != nil {
		t.Fatal(err)
	}
	expected[0]["url"] = exported.URL
	if !reflect.DeepEqual(got, expected) {
		t.Fatal("desktop export differs from OCS example")
	}
}

func assertRedacted(t *testing.T, value any, key string) {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil || strings.Contains(string(body), key) {
		t.Fatal("secret leaked in response", err)
	}
}
