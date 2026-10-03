package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/wchiway/ocs_bank/internal/question"
	"github.com/wchiway/ocs_bank/internal/telemetry"
)

type Options struct {
	Token             string
	RequestsPerMinute int
	Timeout           time.Duration
	Logger            *slog.Logger
	Metrics           *telemetry.Metrics
	Ready             func(context.Context) error
}
type response struct {
	Success bool   `json:"success"`
	Data    *data  `json:"data,omitempty"`
	Error   string `json:"error,omitempty"`
}
type data struct {
	Code   int     `json:"code"`
	Data   *string `json:"data"`
	Msg    string  `json:"msg"`
	Source string  `json:"source,omitempty"`
}

func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func New(q question.Querier, o Options) http.Handler {
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.Timeout <= 0 {
		o.Timeout = time.Minute
	}
	if o.RequestsPerMinute < 1 {
		o.RequestsPerMinute = 120
	}
	if o.Metrics == nil {
		o.Metrics = &telemetry.Metrics{}
	}
	authorized := func(r *http.Request) bool {
		return o.Token == "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+o.Token)) == 1
	}
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Request-ID", middleware.GetReqID(r.Context()))
			defer func() {
				if recover() != nil {
					o.Logger.Error("request panic", "request_id", middleware.GetReqID(r.Context()))
					write(w, 500, response{Error: "internal error"})
				}
			}()
			next.ServeHTTP(w, r)
		})
	})
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) { write(w, 200, map[string]string{"status": "ok"}) })
	r.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		if o.Ready != nil && o.Ready(ctx) != nil {
			write(w, 503, map[string]string{"status": "unavailable"})
			return
		}
		write(w, 200, map[string]string{"status": "ready"})
	})
	r.Get("/api/stats", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(r) {
			write(w, 401, response{Error: "unauthorized"})
			return
		}
		write(w, 200, map[string]any{"scope": "process", "counters": o.Metrics.Snapshot()})
	})
	var mu sync.Mutex
	window := time.Now()
	count := 0
	r.Get("/api/query", func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		category := "panic"
		var res question.Result
		defer func() {
			o.Metrics.Add("queries_total", 1)
			o.Metrics.Add("query_result_"+category, 1)
			o.Metrics.Add("query_layer_"+res.Layer(), 1)
			o.Metrics.ObserveDuration("query", time.Since(start))
			o.Logger.Info("query", "request_id", middleware.GetReqID(r.Context()), "duration_ms", time.Since(start).Milliseconds(), "cached", res.Cached, "layer", res.Layer(), "result", category)
		}()
		if !authorized(r) {
			category = "unauthorized"
			write(w, 401, response{Error: "unauthorized"})
			return
		}
		mu.Lock()
		if time.Since(window) >= time.Minute {
			window = time.Now()
			count = 0
		}
		allowed := count < o.RequestsPerMinute
		if allowed {
			count++
		}
		retrySeconds := max(1, int(time.Until(window.Add(time.Minute)).Seconds())+1)
		mu.Unlock()
		if !allowed {
			category = "rate_limit"
			w.Header().Set("Retry-After", strconv.Itoa(retrySeconds))
			write(w, 429, response{Error: "rate limited"})
			return
		}
		values, err := parseQuery(r)
		if err != nil {
			category = "invalid"
			write(w, 400, response{Error: "invalid query parameters"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), o.Timeout)
		defer cancel()
		res, err = q.Query(ctx, values)
		category = question.ErrorCategory(err)
		if err != nil {
			switch {
			case errors.Is(err, question.ErrNotFound), errors.Is(err, question.ErrRefused):
				write(w, 200, response{Success: true, Data: &data{Code: 0, Msg: "未找到答案"}})
			case errors.Is(err, question.ErrInvalid):
				write(w, 400, response{Error: "invalid question"})
			case errors.Is(err, question.ErrStorage):
				write(w, 503, response{Error: "storage unavailable"})
			case errors.Is(err, context.DeadlineExceeded):
				write(w, 504, response{Error: "upstream timeout"})
			case errors.Is(err, context.Canceled):
				write(w, 408, response{Error: "request canceled"})
			case errors.Is(err, question.ErrBusy), errors.Is(err, question.ErrRateLimit):
				write(w, 503, response{Error: "service busy"})
			default:
				write(w, 502, response{Error: "answer service unavailable"})
			}
			return
		}
		msg := "AI回答"
		source := "ai"
		if res.Cached {
			msg = "来于本地数据库题库"
			source = "public"
		}
		write(w, 200, response{Success: true, Data: &data{Code: 1, Data: &res.Answer, Msg: msg, Source: source}})
	})
	r.NotFound(func(w http.ResponseWriter, r *http.Request) { write(w, 404, response{Error: "not found"}) })
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) { write(w, 405, response{Error: "method not allowed"}) })
	return r
}
func parseQuery(r *http.Request) (question.Question, error) {
	if len(r.URL.RawQuery) > 65536 {
		return question.Question{}, question.ErrInvalid
	}
	v, err := urlValues(r)
	if err != nil {
		return question.Question{}, err
	}
	return (question.Question{Title: v["title"], Options: v["options"], Type: v["type"]}).Normalize()
}
