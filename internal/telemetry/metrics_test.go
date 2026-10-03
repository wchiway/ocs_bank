package telemetry

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type failedRecorder struct{}

func (failedRecorder) RecordCall(context.Context, Call) error { return errors.New("synthetic failure") }

func TestConcurrentMetricsAndFailedRecord(t *testing.T) {
	m := &Metrics{}
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				m.Add("queries_total", 1)
				_ = m.Snapshot()
			}
		}()
	}
	wg.Wait()
	if m.Snapshot()["queries_total"] != 2000 {
		t.Fatal(m.Snapshot())
	}
	r := CountingRecorder{Next: failedRecorder{}, Metrics: m}
	if err := r.RecordCall(context.Background(), Call{Result: "timeout", Duration: time.Second}); err == nil {
		t.Fatal("lost persistence failure")
	}
	s := m.Snapshot()
	if s["upstream_calls_total"] != 1 || s["upstream_usage_unknown_total"] != 1 || s["call_record_write_failures_total"] != 1 || s["upstream_ms_total"] != 1000 {
		t.Fatal(s)
	}
	s["queries_total"] = 0
	if m.Snapshot()["queries_total"] != 2000 {
		t.Fatal("mutable snapshot")
	}
}
