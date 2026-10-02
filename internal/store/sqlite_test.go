package store

import (
	"context"
	"errors"
	"github.com/wchiway/ocs_bank/internal/question"
	"path/filepath"
	"testing"
	"time"
)

func TestSQLitePersistenceAndInvalidation(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "nested", "bank.db")
	s, err := Open(ctx, path, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	q := question.Question{Title: "q", Options: "a b", Type: "single"}
	key := q.Key("x")
	if _, err = s.Get(ctx, key); !errors.Is(err, question.ErrNotFound) {
		t.Fatal(err)
	}
	r := question.Result{Answer: "a", Source: "chat", Model: "test", InputTokens: 2}
	if err = s.Put(ctx, key, q, r); err != nil {
		t.Fatal(err)
	}
	r.Answer = "b"
	if err = s.Put(ctx, key, q, r); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(ctx, path, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.Get(ctx, key)
	if err != nil || got.Answer != "b" || !got.Cached || got.InputTokens != 2 {
		t.Fatal(got, err)
	}
	var n int
	s.db.QueryRow(`SELECT count(*) FROM answers`).Scan(&n)
	if n != 1 {
		t.Fatal("duplicate rows")
	}
	q.Options = "c d"
	if _, err = s.Get(ctx, q.Key("x")); !errors.Is(err, question.ErrNotFound) {
		t.Fatal("options collision")
	}
	if err = s.Invalidate(ctx, key); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Get(ctx, key); !errors.Is(err, question.ErrNotFound) {
		t.Fatal("invalidated cache hit")
	}
	if err = s.Put(ctx, key, q, question.Result{}); !errors.Is(err, question.ErrProtocol) {
		t.Fatal("empty stored")
	}
}
func TestExpiryAndFutureSchema(t *testing.T) {
	ctx := context.Background()
	p := filepath.Join(t.TempDir(), "bank.db")
	s, err := Open(ctx, p, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	q := question.Question{Title: "x"}
	if err = s.Put(ctx, "x", q, question.Result{Answer: "a"}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`UPDATE answers SET expires_at=0`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Get(ctx, "x"); !errors.Is(err, question.ErrNotFound) {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`INSERT INTO schema_migrations VALUES(99)`); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if next, err := Open(ctx, p, time.Hour); err == nil {
		next.Close()
		t.Fatal("accepted newer schema")
	}
}
