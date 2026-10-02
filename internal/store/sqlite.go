package store

import (
	"context"
	"database/sql"
	"errors"
	"github.com/wchiway/ocs_bank/internal/question"
	"github.com/wchiway/ocs_bank/migrations"
	_ "modernc.org/sqlite"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type SQLite struct {
	db  *sql.DB
	ttl time.Duration
}

func Open(ctx context.Context, path string, ttl time.Duration) (*SQLite, error) {
	if path == "" || ttl <= 0 {
		return nil, errors.New("database path and positive TTL required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Dir(abs), 0700); err != nil {
		return nil, err
	}
	u := url.URL{Scheme: "file", Path: abs}
	values := url.Values{}
	values.Add("_pragma", "busy_timeout(5000)")
	values.Add("_pragma", "journal_mode(WAL)")
	values.Add("_pragma", "foreign_keys(1)")
	u.RawQuery = values.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err = db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	if err = migrations.Apply(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	return &SQLite{db: db, ttl: ttl}, nil
}
func (s *SQLite) Close() error { return s.db.Close() }
func (s *SQLite) Get(ctx context.Context, key string) (question.Result, error) {
	var r question.Result
	err := s.db.QueryRowContext(ctx, `SELECT answer, source, model, input_tokens, output_tokens FROM answers WHERE cache_key=? AND invalidated=0 AND expires_at>?`, key, time.Now().Unix()).Scan(&r.Answer, &r.Source, &r.Model, &r.InputTokens, &r.OutputTokens)
	if errors.Is(err, sql.ErrNoRows) {
		return r, question.ErrNotFound
	}
	if err != nil {
		return r, err
	}
	r.Cached = true
	return r, nil
}
func (s *SQLite) Put(ctx context.Context, key string, q question.Question, r question.Result) error {
	if strings.TrimSpace(r.Answer) == "" || len(r.Answer) > 32768 || key == "" {
		return question.ErrProtocol
	}
	now := time.Now()
	_, err := s.db.ExecContext(ctx, `INSERT INTO answers(cache_key,question,options,type,answer,source,model,prompt_version,input_tokens,output_tokens,created_at,expires_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(cache_key) DO UPDATE SET answer=excluded.answer,source=excluded.source,model=excluded.model,prompt_version=excluded.prompt_version,input_tokens=excluded.input_tokens,output_tokens=excluded.output_tokens,created_at=excluded.created_at,expires_at=excluded.expires_at,invalidated=0`, key, q.Title, q.Options, q.Type, r.Answer, r.Source, r.Model, question.PromptVersion, r.InputTokens, r.OutputTokens, now.Unix(), now.Add(s.ttl).Unix())
	return err
}
func (s *SQLite) Invalidate(ctx context.Context, key string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE answers SET invalidated=1 WHERE cache_key=?`, key)
	return err
}

var _ question.Repository = (*SQLite)(nil)
