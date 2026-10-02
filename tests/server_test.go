package tests

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestServerBinary(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "server")
	build := exec.Command("go", "build", "-o", binary, "../cmd/server")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	var calls atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"{\"answer\":\"yes\"}"}}]}`)
	}))
	defer up.Close()
	cfg := map[string]any{"listen": "", "database": filepath.Join(dir, "bank.db"), "protocol": "chat", "base_url": up.URL + "/v1", "model": "test", "access_token_env": "OCS_BINARY_TEST_ACCESS", "timeout_seconds": 2}
	client := &http.Client{Timeout: time.Second}
	start := func() (string, func()) {
		t.Helper()
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		address := listener.Addr().String()
		listener.Close()
		cfg["listen"] = address
		path := filepath.Join(dir, "config.json")
		body, _ := json.Marshal(cfg)
		if err = os.WriteFile(path, body, 0600); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(binary, "-config", path)
		if err = os.WriteFile(filepath.Join(dir, ".env"), []byte("AI_API_KEY=sentinel-api-key\nOCS_BINARY_TEST_ACCESS=sentinel-access-token\n"), 0600); err != nil {
			t.Fatal(err)
		}
		cmd.Env = os.Environ()
		var logs bytes.Buffer
		cmd.Stdout = &logs
		cmd.Stderr = &logs
		if err = cmd.Start(); err != nil {
			t.Fatal(err)
		}
		done := make(chan struct{})
		var waitErr error
		go func() { waitErr = cmd.Wait(); close(done) }()
		var once sync.Once
		stop := func() {
			once.Do(func() {
				_ = cmd.Process.Signal(syscall.SIGTERM)
				select {
				case <-done:
				case <-time.After(10 * time.Second):
					_ = cmd.Process.Kill()
					<-done
					t.Error("server failed to shut down")
				}
				if waitErr != nil {
					t.Errorf("server exit: %v %s", waitErr, logs.String())
				}
				if strings.Contains(logs.String(), "sentinel-api-key") || strings.Contains(logs.String(), "sentinel-access-token") || strings.Contains(logs.String(), "private-question") {
					t.Error("sensitive log output")
				}
			})
		}
		t.Cleanup(stop)
		base := "http://" + address
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			select {
			case <-done:
				t.Fatalf("startup failed: %v %s", waitErr, logs.String())
			default:
			}
			r, err := client.Get(base + "/healthz")
			if err == nil {
				r.Body.Close()
				if r.StatusCode == 200 {
					return base, stop
				}
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatal("startup timed out")
		return "", stop
	}
	query := func(base string, authorized bool) (int, string) {
		t.Helper()
		req, _ := http.NewRequest("GET", base+"/api/query?title=private-question&type=completion", nil)
		if authorized {
			req.Header.Set("Authorization", "Bearer sentinel-access-token")
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(body)
	}
	base, stop := start()
	if status, _ := query(base, false); status != 401 {
		t.Fatal(status)
	}
	if status, body := query(base, true); status != 200 || !strings.Contains(body, `"data":"yes"`) {
		t.Fatal(status, body)
	}
	stop()
	base, stop = start()
	if status, body := query(base, true); status != 200 || !strings.Contains(body, `"source":"public"`) {
		t.Fatal(status, body)
	}
	stop()
	if calls.Load() != 1 {
		t.Fatal("cache did not survive process restart", calls.Load())
	}
}
