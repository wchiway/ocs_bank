package telemetry

import (
	"context"
	"sync"
	"time"
)

// Metrics contains process-lifetime counters with a fixed set of caller-owned
// names. Never use request IDs, questions, model names or error text as names.
type Metrics struct {
	mu     sync.Mutex
	values map[string]int64
}

func (m *Metrics) Add(name string, value int64) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.values == nil {
		m.values = map[string]int64{}
	}
	m.values[name] += value
}

func (m *Metrics) Snapshot() map[string]int64 {
	result := map[string]int64{}
	if m == nil {
		return result
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, value := range m.values {
		result[key] = value
	}
	return result
}

func (m *Metrics) ObserveDuration(name string, elapsed time.Duration) {
	m.Add(name+"_count", 1)
	m.Add(name+"_ms_total", elapsed.Milliseconds())
}

type CountingRecorder struct {
	Next    Recorder
	Metrics *Metrics
}

func (r CountingRecorder) RecordCall(ctx context.Context, c Call) error {
	r.Metrics.Add("upstream_calls_total", 1)
	r.Metrics.Add("upstream_result_"+c.Result, 1)
	r.Metrics.ObserveDuration("upstream", c.Duration)
	if c.UsageKnown {
		r.Metrics.Add("input_tokens_known_total", c.InputTokens)
		r.Metrics.Add("output_tokens_known_total", c.OutputTokens)
	} else {
		r.Metrics.Add("upstream_usage_unknown_total", 1)
	}
	if err := r.Next.RecordCall(ctx, c); err != nil {
		r.Metrics.Add("call_record_write_failures_total", 1)
		return err
	}
	return nil
}
