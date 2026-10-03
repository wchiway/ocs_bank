package store

import (
	"context"
	"github.com/wchiway/ocs_bank/internal/legacy"
	"github.com/wchiway/ocs_bank/internal/question"
	"time"
)

// Import atomically inserts reviewed legacy answers without overwriting any existing key.
func (s *SQLite) Import(ctx context.Context, items []legacy.Candidate) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	count := 0
	now := time.Now()
	for _, item := range items {
		q, err := item.Question.Normalize()
		if err != nil {
			return 0, err
		}
		answer, err := question.ValidateAnswer(q, item.Answer)
		if err != nil {
			return 0, err
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO reviewed_answers(question,options,type,answer,source,created_at,updated_at) VALUES(?,?,?,?,?,?,?) ON CONFLICT(question,options,type) DO NOTHING`, q.Title, q.Options, q.Type, answer, "legacy-reviewed", now.Unix(), now.Unix())
		if err != nil {
			return 0, err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return 0, err
		}
		count += int(n)
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return count, nil
}
