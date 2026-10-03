package store

import (
	"context"
	"github.com/wchiway/ocs_bank/internal/legacy"
	"github.com/wchiway/ocs_bank/internal/question"
	"path/filepath"
	"testing"
	"time"
)

func TestImportAtomicAndPreservesExisting(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "bank.db"), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	q := question.Question{Title: "q", Type: "completion"}
	items := []legacy.Candidate{{Question: q, Answer: "old"}}
	if err = s.Put(ctx, q.Key("ns"), q, question.Result{Answer: "new"}); err != nil {
		t.Fatal(err)
	}
	if n, e := s.Import(ctx, items); e != nil || n != 1 {
		t.Fatal(n, e)
	}
	r, e := s.GetReviewed(ctx, q)
	if e != nil || r.Answer != "old" {
		t.Fatal(r, e)
	}
	items[0].Answer = "replacement"
	if n, e := s.Import(ctx, items); e != nil || n != 0 {
		t.Fatal(n, e)
	}
	if r, e := s.GetReviewed(ctx, q); e != nil || r.Answer != "old" {
		t.Fatal(r, e)
	}
	items[0].Question.Title = "new question"
	items = append(items, legacy.Candidate{Question: question.Question{Title: "bad"}, Answer: ""})
	if _, e = s.Import(ctx, items); e == nil {
		t.Fatal("invalid import accepted")
	}
	if _, e = s.GetReviewed(ctx, items[0].Question); e != question.ErrNotFound {
		t.Fatal("partial import committed", e)
	}
	if n, e := s.Import(ctx, items[:1]); e != nil || n != 1 {
		t.Fatal(n, e)
	}
}
