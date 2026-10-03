package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/wchiway/ocs_bank/migrations"
)

// OpenExisting never creates or migrates a database and never reads configuration.
// Read-only mode still permits VACUUM INTO a separate output database.
func OpenExisting(ctx context.Context, path string, writable bool) (*SQLite, error) {
	if path == "" {
		return nil, errors.New("database path is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("database must be a regular file")
	}
	mode := "ro"
	if writable {
		mode = "rw"
	}
	u := url.URL{Scheme: "file", Path: abs}
	values := url.Values{"mode": {mode}, "_pragma": {"busy_timeout(1000)"}}
	u.RawQuery = values.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	var version int
	if err = db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0) FROM schema_migrations`).Scan(&version); err != nil {
		db.Close()
		return nil, errors.New("cannot read database schema")
	}
	if version < 1 || version > migrations.Version {
		db.Close()
		return nil, fmt.Errorf("unsupported database schema version %d", version)
	}
	return &SQLite{db: db, schema: version}, nil
}

type Statistics struct {
	CacheRecords      int64            `json:"cache_records"`
	ReviewedRecords   int64            `json:"reviewed_records"`
	Calls             int64            `json:"calls"`
	UnknownUsageCalls int64            `json:"unknown_usage_calls"`
	InputTokens       int64            `json:"known_input_tokens"`
	OutputTokens      int64            `json:"known_output_tokens"`
	Results           map[string]int64 `json:"results"`
}

func (s *SQLite) Statistics(ctx context.Context) (Statistics, error) {
	var result Statistics
	if s.schema != migrations.Version {
		return result, errors.New("statistics requires current schema")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	err = tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM answers),(SELECT count(*) FROM reviewed_answers),count(*),COALESCE(sum(input_tokens IS NULL),0),COALESCE(sum(input_tokens),0),COALESCE(sum(output_tokens),0) FROM upstream_calls`).Scan(&result.CacheRecords, &result.ReviewedRecords, &result.Calls, &result.UnknownUsageCalls, &result.InputTokens, &result.OutputTokens)
	if err != nil {
		return result, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT result,count(*) FROM upstream_calls GROUP BY result`)
	if err != nil {
		return result, err
	}
	result.Results = map[string]int64{}
	for rows.Next() {
		var name string
		var count int64
		if err = rows.Scan(&name, &count); err != nil {
			rows.Close()
			return result, err
		}
		result.Results[name] = count
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	return result, tx.Commit()
}

type PruneOptions struct {
	Model       string
	CallsBefore time.Time
	BatchSize   int
	Apply       bool
}
type PruneResult struct {
	CacheMatched int64 `json:"cache_matched"`
	CallsMatched int64 `json:"calls_matched"`
	CacheDeleted int64 `json:"cache_deleted"`
	CallsDeleted int64 `json:"calls_deleted"`
	Applied      bool  `json:"applied"`
}

func (s *SQLite) Prune(ctx context.Context, o PruneOptions) (PruneResult, error) {
	r := PruneResult{Applied: o.Apply}
	if s.schema != migrations.Version {
		return r, errors.New("pruning requires current schema")
	}
	if o.BatchSize < 1 || o.BatchSize > 10000 {
		return r, errors.New("batch size must be between 1 and 10000")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return r, err
	}
	defer tx.Rollback()
	const cacheWhere = `source != 'legacy-reviewed' AND (invalidated=1 OR expires_at<=?) AND (?='' OR model=?)`
	args := []any{time.Now().Unix(), o.Model, o.Model}
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM answers WHERE `+cacheWhere, args...).Scan(&r.CacheMatched); err != nil {
		return r, err
	}
	if !o.CallsBefore.IsZero() {
		err = tx.QueryRowContext(ctx, `SELECT count(*) FROM upstream_calls WHERE started_at<? AND (?='' OR model=?)`, o.CallsBefore.Unix(), o.Model, o.Model).Scan(&r.CallsMatched)
		if err != nil {
			return r, err
		}
	}
	if o.Apply {
		result, err := tx.ExecContext(ctx, `DELETE FROM answers WHERE cache_key IN (SELECT cache_key FROM answers WHERE `+cacheWhere+` ORDER BY expires_at,cache_key LIMIT ?)`, append(args, o.BatchSize)...)
		if err != nil {
			return r, err
		}
		r.CacheDeleted, err = result.RowsAffected()
		if err != nil {
			return r, err
		}
		if !o.CallsBefore.IsZero() {
			result, err = tx.ExecContext(ctx, `DELETE FROM upstream_calls WHERE id IN (SELECT id FROM upstream_calls WHERE started_at<? AND (?='' OR model=?) ORDER BY started_at,id LIMIT ?)`, o.CallsBefore.Unix(), o.Model, o.Model, o.BatchSize)
			if err != nil {
				return r, err
			}
			r.CallsDeleted, err = result.RowsAffected()
			if err != nil {
				return r, err
			}
		}
	}
	if err = tx.Commit(); err != nil {
		return PruneResult{}, err
	}
	return r, nil
}

// Backup publishes a complete snapshot atomically without replacing any file.
func (s *SQLite) Backup(ctx context.Context, destination string) error {
	if destination == "" {
		return errors.New("output path is required")
	}
	abs, err := filepath.Abs(destination)
	if err != nil {
		return err
	}
	if _, err = os.Lstat(abs); err == nil {
		return errors.New("output already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(abs), ".ocs-backup-*.db")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer os.Remove(name)
	if err = temp.Close(); err != nil {
		return err
	}
	if _, err = s.db.ExecContext(ctx, `VACUUM INTO ?`, name); err != nil {
		return err
	}
	f, err := os.OpenFile(name, os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	err = f.Sync()
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	// Link fails if another process created the destination after our initial check.
	if err = os.Link(name, abs); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(abs))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func Restore(ctx context.Context, source, destination string) error {
	s, err := OpenExisting(ctx, source, false)
	if err != nil {
		return err
	}
	defer s.Close()
	rows, err := s.db.QueryContext(ctx, `PRAGMA integrity_check`)
	if err != nil {
		return err
	}
	valid := true
	count := 0
	for rows.Next() {
		var value string
		if err = rows.Scan(&value); err != nil {
			rows.Close()
			return err
		}
		count++
		if value != "ok" {
			valid = false
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if !valid || count != 1 {
		return errors.New("backup failed integrity check")
	}
	return s.Backup(ctx, destination)
}
