package store

import (
	"context"
	"github.com/wchiway/ocs_bank/internal/legacy"
	"github.com/wchiway/ocs_bank/internal/question"
	"strings"
	"time"
)

// Import atomically inserts reviewed legacy answers without overwriting any existing key.
func (s *SQLite) Import(ctx context.Context, namespace string, items []legacy.Candidate) (int, error) {
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
		if strings.TrimSpace(item.Answer) == "" || len(item.Answer) > 32768 {
			return 0, question.ErrProtocol
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO answers(cache_key,question,options,type,answer,source,model,prompt_version,created_at,expires_at) VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(cache_key) DO NOTHING`, q.Key(namespace), q.Title, q.Options, q.Type, item.Answer, "legacy-reviewed", "unknown", question.PromptVersion, now.Unix(), now.Add(s.ttl).Unix())
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
