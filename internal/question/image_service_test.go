package question

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

func TestImageSingleflightAndCacheIsolation(t *testing.T) {
	t.Parallel()
	first := Question{Title: "same", Type: "completion", Images: parsedImages(t, syntheticImage(t, "png", 1))}
	second := first
	second.Images = parsedImages(t, syntheticImage(t, "png", 2))
	m := &memory{values: map[string]Result{}}
	release := make(chan struct{})
	var calls atomic.Int32
	s := service(t, m, generatorFunc(func(ctx context.Context, q Question) (Result, error) {
		calls.Add(1)
		select {
		case <-release:
			return Result{Answer: q.Images[0].digest}, nil
		case <-ctx.Done():
			return Result{}, ctx.Err()
		}
	}), 2, 6)
	done := make(chan error, 3)
	for _, q := range []Question{first, first, second} {
		go func() {
			r, err := s.Query(context.Background(), q)
			if err == nil && r.Answer != q.Images[0].digest {
				err = errors.New("answer crossed image identities")
			}
			done <- err
		}()
	}
	waitPending(t, s, 3)
	close(release)
	for range 3 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 2 {
		t.Fatal("image flights were shared incorrectly", calls.Load())
	}
	for _, q := range []Question{first, second} {
		r, err := s.Query(context.Background(), q)
		if err != nil || !r.Cached || r.Answer != q.Images[0].digest || calls.Load() != 2 {
			t.Fatal("image cache isolation failed", err)
		}
	}
}

func TestImageErrorsNeverCache(t *testing.T) {
	t.Parallel()
	for _, failure := range []error{ErrProtocol, ErrUnavailable, ErrOutputLimit, ErrAnswerInvalid} {
		m := &memory{values: map[string]Result{}}
		var calls atomic.Int32
		s := service(t, m, generatorFunc(func(context.Context, Question) (Result, error) {
			calls.Add(1)
			return Result{}, failure
		}), 1, 2)
		q := Question{Title: "q", Images: parsedImages(t, syntheticImage(t, "png", 1))}
		for range 2 {
			if _, err := s.Query(context.Background(), q); !errors.Is(err, failure) {
				t.Fatal(err)
			}
		}
		if calls.Load() != 2 || m.writes != 0 {
			t.Fatal("failed image generation cached")
		}
	}
}
