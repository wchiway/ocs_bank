// Package desktop manages the local server for the desktop application.
package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wchiway/ocs_bank/internal/config"
	"github.com/wchiway/ocs_bank/internal/httpapi"
	"github.com/wchiway/ocs_bank/internal/provider"
	"github.com/wchiway/ocs_bank/internal/question"
	"github.com/wchiway/ocs_bank/internal/store"
	"github.com/wchiway/ocs_bank/internal/telemetry"
)

type Settings struct {
	Port               int    `json:"port"`
	Protocol           string `json:"protocol"`
	BaseURL            string `json:"base_url"`
	Model              string `json:"model"`
	EndpointPath       string `json:"endpoint_path"`
	ReasoningEffort    string `json:"reasoning_effort"`
	ChatTokenParameter string `json:"chat_token_parameter"`
	TimeoutSeconds     int    `json:"timeout_seconds"`
	MaxConcurrent      int    `json:"max_concurrent"`
	MaxPending         int    `json:"max_pending"`
	MaxRetries         int    `json:"max_retries"`
	MaxTokens          int    `json:"max_tokens"`
	RequestsPerMinute  int    `json:"requests_per_minute"`
	CacheTTLHours      int    `json:"cache_ttl_hours"`
	HasAPIKey          bool   `json:"has_api_key"`
	Configured         bool   `json:"configured"`
	DataDir            string `json:"data_dir"`
}

type SaveRequest struct {
	Settings
	APIKey string `json:"api_key"`
}

type Status struct {
	State      string           `json:"state"`
	URL        string           `json:"url"`
	StartedAt  string           `json:"started_at"`
	Counters   map[string]int64 `json:"counters"`
	LastError  string           `json:"last_error"`
	Configured bool             `json:"configured"`
	DataDir    string           `json:"data_dir"`
}

type Controller struct {
	op         sync.Mutex
	mu         sync.Mutex
	root       string
	logger     *slog.Logger
	state      string
	port       int
	configured bool
	closed     bool
	lastError  string
	metrics    *telemetry.Metrics
	started    time.Time
	running    *running
}

type running struct {
	server  *http.Server
	cancel  context.CancelFunc
	service *question.Service
	ai      *provider.Client
	db      *store.SQLite
	metrics *telemetry.Metrics
	started time.Time
	once    sync.Once
	stopped chan struct{}
}

func New(root string, logger *slog.Logger) (*Controller, error) {
	if root == "" {
		return nil, errors.New("请指定桌面应用的数据目录")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, errors.New("无法确定桌面应用的数据目录")
	}
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Controller{root: abs, logger: logger, state: "stopped", port: 5000}, nil
}

func (c *Controller) Settings() (Settings, error) {
	c.op.Lock()
	defer c.op.Unlock()
	cfg, exists, err := c.readDocument()
	if err != nil {
		return Settings{}, err
	}
	secrets, err := c.readSecrets()
	if err != nil {
		return Settings{}, err
	}
	s := c.settings(cfg)
	s.HasAPIKey = strings.TrimSpace(secrets["AI_API_KEY"]) != ""
	cfg.APIKey = secrets["AI_API_KEY"]
	pending, err := c.savePending()
	if err != nil {
		return Settings{}, err
	}
	s.Configured = exists && !pending && c.validate(&cfg) == nil
	c.mu.Lock()
	c.configured, c.port = s.Configured, s.Port
	c.mu.Unlock()
	return s, nil
}

func (c *Controller) Start() (Status, error) {
	c.op.Lock()
	defer c.op.Unlock()
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return c.Status(), errors.New("桌面应用正在退出")
	}
	if c.state == "running" {
		c.mu.Unlock()
		return c.Status(), nil
	}
	if c.state != "stopped" {
		c.mu.Unlock()
		return c.Status(), errors.New("请等待服务停止后再启动")
	}
	c.mu.Unlock()
	cfg, exists, err := c.readDocument()
	if err != nil {
		return c.startFailure(err)
	}
	if !exists {
		return c.startFailure(errors.New("请先填写模型信息和 API Key，并保存设置"))
	}
	pending, err := c.savePending()
	if err != nil {
		return c.startFailure(err)
	}
	if pending {
		return c.startFailure(errors.New("上次设置保存未完成，请重新保存设置后启动"))
	}
	secrets, err := c.readSecrets()
	if err != nil {
		return c.startFailure(err)
	}
	cfg.APIKey = secrets["AI_API_KEY"]
	if err = c.validate(&cfg); err != nil {
		return c.startFailure(err)
	}
	c.mu.Lock()
	c.state, c.port, c.configured, c.lastError = "starting", portOf(cfg.Listen), true, ""
	c.mu.Unlock()
	listener, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return c.startFailure(errors.New("无法监听本机端口，请关闭占用该端口的程序或修改端口"))
	}
	ctx, cancel := context.WithCancel(context.Background())
	initCtx, initCancel := context.WithTimeout(ctx, 10*time.Second)
	db, err := store.Open(initCtx, cfg.Database, time.Duration(cfg.CacheTTLHours)*time.Hour)
	initCancel()
	if err != nil {
		listener.Close()
		cancel()
		return c.startFailure(errors.New("无法打开题库数据库，请检查数据目录是否可写"))
	}
	metrics := &telemetry.Metrics{}
	ai, err := provider.NewObserved(cfg, telemetry.CountingRecorder{Next: db, Metrics: metrics}, c.logger)
	if err != nil {
		listener.Close()
		cancel()
		db.Close()
		return c.startFailure(errors.New("无法初始化模型连接，请检查模型设置"))
	}
	service, err := question.NewService(ctx, db, ai, question.ServiceOptions{Namespace: ai.Namespace(), Timeout: cfg.Timeout(), MaxConcurrent: cfg.MaxConcurrent, MaxPending: cfg.MaxPending, Logger: c.logger, Metrics: metrics})
	if err != nil {
		listener.Close()
		cancel()
		ai.Close()
		db.Close()
		return c.startFailure(errors.New("无法初始化查询服务，请检查并发和超时设置"))
	}
	handler := httpapi.New(service, httpapi.Options{Timeout: cfg.Timeout(), RequestsPerMinute: cfg.RequestsPerMinute, Logger: c.logger, Metrics: metrics, Ready: db.Ping})
	r := &running{
		server: &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: cfg.Timeout() + 5*time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 128 << 10},
		cancel: cancel, service: service, ai: ai, db: db, metrics: metrics, started: time.Now(), stopped: make(chan struct{}),
	}
	c.mu.Lock()
	c.running, c.state = r, "running"
	c.metrics, c.started = r.metrics, r.started
	c.mu.Unlock()
	go func() {
		if err := r.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			c.mu.Lock()
			if c.running == r {
				c.lastError = "服务意外停止，请重新启动"
				c.state = "stopping"
			}
			c.mu.Unlock()
			c.stopRuntime(r)
		}
	}()
	return c.Status(), nil
}

func (c *Controller) Stop() (Status, error) {
	c.op.Lock()
	defer c.op.Unlock()
	return c.stop()
}

func (c *Controller) stop() (Status, error) {
	c.mu.Lock()
	r := c.running
	if r == nil {
		c.mu.Unlock()
		return c.Status(), nil
	}
	c.state = "stopping"
	c.mu.Unlock()
	c.stopRuntime(r)
	select {
	case <-r.stopped:
		return c.Status(), nil
	case <-time.After(10 * time.Second):
		return c.Status(), errors.New("服务仍在完成退出，请稍后查看状态")
	}
}

func (c *Controller) stopRuntime(r *running) {
	r.once.Do(func() {
		go func() {
			r.cancel()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			if r.server.Shutdown(ctx) != nil {
				_ = r.server.Close()
			}
			cancel()
			r.service.Close()
			r.ai.Close()
			_ = r.db.Close()
			c.mu.Lock()
			if c.running == r {
				c.running, c.state = nil, "stopped"
			}
			c.mu.Unlock()
			close(r.stopped)
		}()
	})
}

func (c *Controller) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := Status{State: c.state, URL: queryURL(c.port), LastError: c.lastError, Configured: c.configured, DataDir: c.root, Counters: map[string]int64{}}
	if c.metrics != nil {
		s.StartedAt = c.started.Format(time.RFC3339)
		s.Counters = c.metrics.Snapshot()
	}
	return s
}

func (c *Controller) Close() error {
	c.op.Lock()
	defer c.op.Unlock()
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	_, err := c.stop()
	return err
}

func (c *Controller) startFailure(err error) (Status, error) {
	c.mu.Lock()
	c.state, c.lastError = "stopped", err.Error()
	c.mu.Unlock()
	return c.Status(), err
}

func (c *Controller) readDocument() (config.Config, bool, error) {
	cfg := config.Defaults()
	cfg.Database = filepath.Join(c.root, "question_bank.db")
	cfg.BaseURL = "https://api.siliconflow.cn/v1"
	f, err := os.Open(filepath.Join(c.root, "config.json"))
	if errors.Is(err, os.ErrNotExist) {
		return cfg, false, nil
	}
	if err != nil {
		return cfg, false, errors.New("无法读取设置，请检查数据目录权限")
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 65537))
	d.DisallowUnknownFields()
	if err = d.Decode(&cfg); err != nil {
		return cfg, true, errors.New("设置文件格式错误，请检查 config.json")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return cfg, true, errors.New("设置文件只能包含一个 JSON 对象")
	}
	return cfg, true, nil
}

func (c *Controller) validate(cfg *config.Config) error {
	host, port, err := net.SplitHostPort(cfg.Listen)
	if err != nil || (host != "localhost" && !net.ParseIP(host).IsLoopback()) {
		return errors.New("桌面端仅允许监听本机地址")
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return errors.New("端口必须在 1 到 65535 之间")
	}
	if cfg.AccessTokenEnv != "" {
		return errors.New("桌面端使用本机连接，请移除 access_token_env 设置")
	}
	cfg.Listen = net.JoinHostPort("127.0.0.1", port)
	cfg.Database = filepath.Join(c.root, "question_bank.db")
	if err := cfg.Validate(); err != nil {
		return errors.New("模型设置无效，请检查 API 地址、模型、API Key、协议及高级参数")
	}
	return nil
}

func (c *Controller) settings(cfg config.Config) Settings {
	if cfg.ChatTokenParameter == "" {
		cfg.ChatTokenParameter = "max_tokens"
	}
	return Settings{
		Port: portOf(cfg.Listen), Protocol: cfg.Protocol, BaseURL: cfg.BaseURL, Model: cfg.Model, EndpointPath: cfg.EndpointPath,
		ReasoningEffort: cfg.ReasoningEffort, ChatTokenParameter: cfg.ChatTokenParameter, TimeoutSeconds: cfg.TimeoutSeconds,
		MaxConcurrent: cfg.MaxConcurrent, MaxPending: cfg.MaxPending, MaxRetries: cfg.MaxRetries, MaxTokens: cfg.MaxTokens,
		RequestsPerMinute: cfg.RequestsPerMinute, CacheTTLHours: cfg.CacheTTLHours, DataDir: c.root,
	}
}

func portOf(address string) int {
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return 5000
	}
	n, err := strconv.Atoi(port)
	if err != nil {
		return 5000
	}
	return n
}

func queryURL(port int) string {
	return "http://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(port)) + "/ocs/query"
}
