package store

import (
	"context"
	"database/sql"
	"errors"
	"github.com/wchiway/ocs_bank/internal/question"
)

func (s *SQLite) GetReviewed(ctx context.Context, q question.Question) (question.Result, error) {
	var r question.Result
	err := s.db.QueryRowContext(ctx, `SELECT answer,source FROM reviewed_answers WHERE question=? AND options=? AND type=?`, q.Title, q.Options, q.Type).Scan(&r.Answer, &r.Source)
	if errors.Is(err, sql.ErrNoRows) {
		return r, question.ErrNotFound
	}
	if err != nil {
		if ctx.Err() != nil {
			return r, ctx.Err()
		}
		return r, errors.Join(question.ErrStorage, err)
	}
	r.Cached, r.Reviewed = true, true
	return r, nil
}
