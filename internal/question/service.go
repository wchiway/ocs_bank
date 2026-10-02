package question

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"
)

type ServiceOptions struct {
	Namespace     string
	Timeout       time.Duration
	MaxConcurrent int
	MaxPending    int
	Logger        *slog.Logger
}
type flight struct {
	done     chan struct{}
	cancel   context.CancelFunc
	waiters  int
	result   Result
	err      error
	finished bool
}
type Service struct {
	repo      Repository
	generator Generator
	options   ServiceOptions
	ctx       context.Context
	cancel    context.CancelFunc
	slots     chan struct{}
	mu        sync.Mutex
	flights   map[string]*flight
	pending   int
	wg        sync.WaitGroup
}

func NewService(parent context.Context, repo Repository, generator Generator, o ServiceOptions) (*Service, error) {
	if repo == nil || generator == nil || o.MaxConcurrent < 1 || o.MaxPending < o.MaxConcurrent || o.Timeout <= 0 {
		return nil, errors.New("invalid query service configuration")
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	ctx, cancel := context.WithCancel(parent)
	return &Service{repo: repo, generator: generator, options: o, ctx: ctx, cancel: cancel, slots: make(chan struct{}, o.MaxConcurrent), flights: map[string]*flight{}}, nil
}
func (s *Service) Close() { s.mu.Lock(); s.cancel(); s.mu.Unlock(); s.wg.Wait() }
func (s *Service) Query(ctx context.Context, q Question) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, s.options.Timeout)
	defer cancel()
	q, err := q.Normalize()
	if err != nil {
		return Result{}, err
	}
	if err = ctx.Err(); err != nil {
		return Result{}, err
	}
	key := q.Key(s.options.Namespace)
	cached, err := s.repo.Get(ctx, key)
	if err == nil {
		return cached, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Result{}, err
	}
	s.mu.Lock()
	if err = s.ctx.Err(); err != nil {
		s.mu.Unlock()
		return Result{}, err
	}
	if s.pending >= s.options.MaxPending {
		s.mu.Unlock()
		return Result{}, ErrBusy
	}
	c := s.flights[key]
	if c == nil {
		shared, cancel := context.WithTimeout(s.ctx, s.options.Timeout)
		c = &flight{done: make(chan struct{}), cancel: cancel}
		s.flights[key] = c
		s.wg.Add(1)
		go s.run(shared, key, q, c)
	}
	c.waiters++
	s.pending++
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.pending--
		c.waiters--
		if c.waiters == 0 && !c.finished {
			if s.flights[key] == c {
				delete(s.flights, key)
			}
			c.cancel()
		}
	}()
	select {
	case <-ctx.Done():
		return Result{}, ctx.Err()
	case <-c.done:
		if err = ctx.Err(); err != nil {
			return Result{}, err
		}
		return c.result, c.err
	}
}
func (s *Service) run(ctx context.Context, key string, q Question, c *flight) {
	defer s.wg.Done()
	defer c.cancel()
	result, err := s.generate(ctx, key, q)
	s.mu.Lock()
	c.result = result
	c.err = err
	c.finished = true
	if s.flights[key] == c {
		delete(s.flights, key)
	}
	close(c.done)
	s.mu.Unlock()
}
func (s *Service) generate(ctx context.Context, key string, q Question) (Result, error) {
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	// Another completed request may have populated SQLite while this call queued.
	if cached, err := s.repo.Get(ctx, key); err == nil {
		return cached, nil
	} else if !errors.Is(err, ErrNotFound) {
		return Result{}, err
	}
	r, err := s.generator.Generate(ctx, q)
	if err != nil {
		return Result{}, err
	}
	if err = ctx.Err(); err != nil {
		return Result{}, err
	}
	r.Answer = strings.TrimSpace(r.Answer)
	if r.Answer == "" || len(r.Answer) > 32768 {
		return Result{}, ErrProtocol
	}
	r.Cached = false
	if err = s.repo.Put(ctx, key, q, r); err != nil {
		s.options.Logger.Error("cache write failed", "category", "storage")
	}
	return r, nil
}
