package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wchiway/ocs_bank/internal/legacy"
	"github.com/wchiway/ocs_bank/internal/question"
)

func TestUpgradeReviewedAnswers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	script, err := os.ReadFile("../../migrations/001_answers.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(script)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY); INSERT INTO schema_migrations VALUES(1)`); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		key, title, answer, source string
		invalid                    int
	}{
		{"a", "expired reviewed", "good", "legacy-reviewed", 0},
		{"b", "expired reviewed", "good", "legacy-reviewed", 0},
		{"c", "conflict", "one", "legacy-reviewed", 0},
		{"d", "conflict", "two", "legacy-reviewed", 0},
		{"e", "invalidated", "good", "legacy-reviewed", 1},
		{"f", "AI only", "good", "chat_completions", 0},
	} {
		_, err = db.Exec(`INSERT INTO answers(cache_key,question,options,type,answer,source,model,prompt_version,created_at,expires_at,invalidated) VALUES(?,?,'','completion',?,?,'old','answer-v1',1,2,?)`, row.key, row.title, row.answer, row.source, row.invalid)
		if err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	s, err := Open(context.Background(), path, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, title := range []string{"expired reviewed", "conflict", "invalidated", "AI only"} {
		r, err := s.GetReviewed(context.Background(), question.Question{Title: title, Type: "completion"})
		if title == "expired reviewed" {
			if err != nil || !r.Reviewed || r.Answer != "good" {
				t.Fatal(r, err)
			}
		} else if !errors.Is(err, question.ErrNotFound) {
			t.Fatal(title, err)
		}
	}
	var n int
	if err = s.db.QueryRow(`SELECT count(*) FROM answers`).Scan(&n); err != nil || n != 6 {
		t.Fatal("migration removed original records", n, err)
	}
}

type forbiddenGenerator struct{}

func (forbiddenGenerator) Generate(context.Context, question.Question) (question.Result, error) {
	return question.Result{}, errors.New("unexpected AI call")
}

func TestReviewedPrioritySurvivesModelsAndRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "bank.db")
	s, err := Open(ctx, path, time.Nanosecond)
	if err != nil {
		t.Fatal(err)
	}
	q := question.Question{Title: "pick", Options: "A. apple\nB. pear", Type: "single"}
	if _, err = s.Import(ctx, []legacy.Candidate{{Question: q, Answer: "pear"}}); err != nil {
		t.Fatal(err)
	}
	if err = s.Put(ctx, q.Key("model-one"), q, question.Result{Answer: "apple"}); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(ctx, path, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, ns := range []string{"model-one", "model-two"} {
		svc, err := question.NewService(ctx, s, forbiddenGenerator{}, question.ServiceOptions{Namespace: ns, Timeout: time.Second, MaxConcurrent: 1, MaxPending: 1})
		if err != nil {
			t.Fatal(err)
		}
		r, err := svc.Query(ctx, q)
		svc.Close()
		if err != nil || !r.Reviewed || !r.Cached || r.Answer != "pear" {
			t.Fatal(r, err)
		}
	}
}
