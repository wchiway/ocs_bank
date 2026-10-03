package question

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wchiway/ocs_bank/internal/telemetry"
)

type memory struct {
	mu        sync.Mutex
	values    map[string]Result
	failWrite bool
	writes    int
}

func TestGenerationMetricsAndClosedCache(t *testing.T) {
	m := &memory{values: map[string]Result{}, failWrite: true}
	s := service(t, m, generatorFunc(func(context.Context, Question) (Result, error) { return Result{Answer: "yes"}, nil }), 1, 2)
	metrics := &telemetry.Metrics{}
	s.options.Metrics = metrics
	if _, err := s.Query(context.Background(), Question{Title: "q"}); err != nil {
		t.Fatal(err)
	}
	v := metrics.Snapshot()
	if v["cache_write_failures_total"] != 1 || v["generation_count"] != 1 || v["queue_count"] != 1 || v["pending_generations"] != 0 || v["active_generations"] != 0 {
		t.Fatal(v)
	}
	s.Close()
	m.values[(Question{Title: "cached"}).Key("test")] = Result{Answer: "cached"}
	if _, err := s.Query(context.Background(), Question{Title: "cached"}); !errors.Is(err, context.Canceled) {
		t.Fatal("closed service accepted cache hit", err)
	}
}

func (m *memory) Get(ctx context.Context, k string) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.values[k]
	if !ok {
		return Result{}, ErrNotFound
	}
	r.Cached = true
	return r, nil
}
func (m *memory) Put(ctx context.Context, k string, q Question, r Result) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failWrite {
		return errors.New("disk full")
	}
	m.writes++
	m.values[k] = r
	return nil
}

type generatorFunc func(context.Context, Question) (Result, error)

func (f generatorFunc) Generate(ctx context.Context, q Question) (Result, error) { return f(ctx, q) }
func service(t *testing.T, m *memory, g Generator, max, pending int) *Service {
	t.Helper()
	s, err := NewService(context.Background(), m, g, ServiceOptions{Namespace: "test", Timeout: 2 * time.Second, MaxConcurrent: max, MaxPending: pending, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}
func waitPending(t *testing.T, s *Service, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		n := s.pending
		s.mu.Unlock()
		if n == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("pending requests did not reach", want)
}
func TestSharedRequestCancellation(t *testing.T) {
	m := &memory{values: map[string]Result{}}
	release := make(chan struct{})
	started := make(chan struct{})
	var calls atomic.Int32
	s := service(t, m, generatorFunc(func(ctx context.Context, q Question) (Result, error) {
		calls.Add(1)
		close(started)
		select {
		case <-release:
			return Result{Answer: "yes"}, nil
		case <-ctx.Done():
			return Result{}, ctx.Err()
		}
	}), 2, 10)
	ctx, cancel := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() { _, e := s.Query(ctx, Question{Title: "same"}); first <- e }()
	<-started
	second := make(chan error, 1)
	go func() {
		r, e := s.Query(context.Background(), Question{Title: "same"})
		if e == nil && r.Answer != "yes" {
			e = errors.New("wrong answer")
		}
		second <- e
	}()
	waitPending(t, s, 2)
	cancel()
	if e := <-first; !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	close(release)
	if e := <-second; e != nil {
		t.Fatal(e)
	}
	if calls.Load() != 1 {
		t.Fatal("duplicate generation")
	}
	r, e := s.Query(context.Background(), Question{Title: "same"})
	if e != nil || !r.Cached || calls.Load() != 1 {
		t.Fatal("cache miss", e)
	}
}
func TestAllWaitersCancelAndClose(t *testing.T) {
	m := &memory{values: map[string]Result{}}
	started := make(chan struct{})
	stopped := make(chan struct{})
	s := service(t, m, generatorFunc(func(ctx context.Context, q Question) (Result, error) {
		close(started)
		<-ctx.Done()
		close(stopped)
		return Result{}, ctx.Err()
	}), 1, 2)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := s.Query(ctx, Question{Title: "x"}); done <- err }()
	<-started
	cancel()
	<-done
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("orphan provider kept running")
	}
	s.Close()
	if _, err := s.Query(context.Background(), Question{Title: "new"}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestBoundedConcurrencyAndQueue(t *testing.T) {
	m := &memory{values: map[string]Result{}}
	release := make(chan struct{})
	var active, peak atomic.Int32
	s := service(t, m, generatorFunc(func(ctx context.Context, q Question) (Result, error) {
		n := active.Add(1)
		for old := peak.Load(); n > old; old = peak.Load() {
			if peak.CompareAndSwap(old, n) {
				break
			}
		}
		defer active.Add(1 - 2)
		select {
		case <-release:
			return Result{Answer: q.Title}, nil
		case <-ctx.Done():
			return Result{}, ctx.Err()
		}
	}), 1, 3)
	done := make(chan error, 3)
	for _, title := range []string{"a", "b", "c"} {
		go func(title string) { _, e := s.Query(context.Background(), Question{Title: title}); done <- e }(title)
	}
	waitPending(t, s, 3)
	if _, e := s.Query(context.Background(), Question{Title: "d"}); !errors.Is(e, ErrBusy) {
		t.Fatal(e)
	}
	close(release)
	for range 3 {
		if e := <-done; e != nil {
			t.Fatal(e)
		}
	}
	if peak.Load() != 1 {
		t.Fatal("concurrency limit exceeded")
	}
}
func TestNoErrorCachingAndWriteFailure(t *testing.T) {
	for _, tc := range []struct {
		name string
		res  Result
		err  error
		fail bool
		want error
	}{{"provider error", Result{}, ErrUnavailable, false, ErrUnavailable}, {"empty answer", Result{}, nil, false, ErrProtocol}, {"write failure", Result{Answer: "yes"}, nil, true, nil}} {
		t.Run(tc.name, func(t *testing.T) {
			m := &memory{values: map[string]Result{}, failWrite: tc.fail}
			var calls int
			s := service(t, m, generatorFunc(func(context.Context, Question) (Result, error) { calls++; return tc.res, tc.err }), 1, 2)
			for range 2 {
				r, e := s.Query(context.Background(), Question{Title: "x"})
				if !errors.Is(e, tc.want) {
					t.Fatal(e)
				}
				if tc.fail && r.Answer != "yes" {
					t.Fatal(r)
				}
			}
			if m.writes != 0 || calls != 2 {
				t.Fatal("invalid result cached")
			}
		})
	}
}
