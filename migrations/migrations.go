// Package migrations owns the versioned SQLite schema.
package migrations

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
)

//go:embed *.sql
var files embed.FS

const Version = 2

func Apply(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY)`); err != nil {
		return err
	}
	var version int
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0) FROM schema_migrations`).Scan(&version); err != nil {
		return err
	}
	if version > Version {
		return fmt.Errorf("unsupported database schema version %d", version)
	}
	for next := version + 1; next <= Version; next++ {
		name := map[int]string{1: "001_answers.sql", 2: "002_reviewed_calls.sql"}[next]
		script, err := files.ReadFile(name)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, string(script)); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES (?)`, next); err != nil {
			return err
		}
	}
	return tx.Commit()
}
