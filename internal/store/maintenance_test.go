package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wchiway/ocs_bank/internal/legacy"
	"github.com/wchiway/ocs_bank/internal/question"
	"github.com/wchiway/ocs_bank/internal/telemetry"
)

func maintenanceFixture(t *testing.T) (*SQLite, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bank.db")
	s, err := Open(context.Background(), path, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, path
}

func TestPrunePreviewFiltersAndBatch(t *testing.T) {
	s, path := maintenanceFixture(t)
	ctx := context.Background()
	for i, key := range []string{"a", "b", "other", "fresh", "reviewed-old"} {
		model := "one"
		if key == "other" {
			model = "two"
		}
		source := "chat_completions"
		if key == "reviewed-old" {
			source = "legacy-reviewed"
		}
		if err := s.Put(ctx, key, question.Question{Title: key}, question.Result{Answer: "x", Source: source, Model: model}); err != nil {
			t.Fatal(err)
		}
		if i != 3 {
			if _, err := s.db.Exec(`UPDATE answers SET expires_at=0 WHERE cache_key=?`, key); err != nil {
				t.Fatal(err)
			}
		}
	}
	q := question.Question{Title: "manual", Type: "completion"}
	if _, err := s.Import(ctx, []legacy.Candidate{{Question: q, Answer: "verified"}}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []telemetry.Call{
		{Protocol: "chat", Model: "one", Started: time.Now().Add(-48 * time.Hour), Result: "ok", UsageKnown: true, InputTokens: 5, OutputTokens: 2},
		{Protocol: "chat", Model: "two", Started: time.Now().Add(-48 * time.Hour), Result: "timeout"},
		{Protocol: "chat", Model: "one", Started: time.Now(), Result: "ok", UsageKnown: true},
	} {
		if err := s.RecordCall(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	stats, err := s.Statistics(ctx)
	if err != nil || stats.Calls != 3 || stats.UnknownUsageCalls != 1 || stats.InputTokens != 5 {
		t.Fatal(stats, err)
	}
	reader, err := OpenExisting(ctx, path, false)
	if err != nil {
		t.Fatal(err)
	}
	opts := PruneOptions{Model: "one", CallsBefore: time.Now().Add(-24 * time.Hour), BatchSize: 1}
	r, err := reader.Prune(ctx, opts)
	reader.Close()
	if err != nil || r.CacheMatched != 2 || r.CallsMatched != 1 || r.CacheDeleted != 0 || r.Applied {
		t.Fatal(r, err)
	}
	opts.Apply = true
	r, err = s.Prune(ctx, opts)
	if err != nil || r.CacheDeleted != 1 || r.CallsDeleted != 1 {
		t.Fatal(r, err)
	}
	stats, err = s.Statistics(ctx)
	if err != nil || stats.CacheRecords != 4 || stats.Calls != 2 || stats.ReviewedRecords != 1 {
		t.Fatal(stats, err)
	}
	if _, err = s.GetReviewed(ctx, q); err != nil {
		t.Fatal(err)
	}
	var old int
	if err = s.db.QueryRow(`SELECT count(*) FROM answers WHERE source='legacy-reviewed'`).Scan(&old); err != nil || old != 1 {
		t.Fatal("review candidates removed", err)
	}
	if _, err = s.Prune(ctx, PruneOptions{BatchSize: 0, Apply: true}); err == nil {
		t.Fatal("unbounded prune accepted")
	}
}

func TestLiveWALBackupAndRestore(t *testing.T) {
	s, path := maintenanceFixture(t)
	ctx := context.Background()
	if _, err := s.db.Exec(`PRAGMA wal_autocheckpoint=0`); err != nil {
		t.Fatal(err)
	}
	q := question.Question{Title: "committed WAL row", Type: "completion"}
	if _, err := s.Import(ctx, []legacy.Candidate{{Question: q, Answer: "present"}}); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path + "-wal"); err != nil || info.Size() == 0 {
		t.Fatal("fixture needs live WAL", err)
	}
	reader, err := OpenExisting(ctx, path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	backup := filepath.Join(filepath.Dir(path), "backup ' quoted.db")
	if err = reader.Backup(ctx, backup); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(backup); err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("backup permissions", info, err)
	}
	restored := filepath.Join(filepath.Dir(path), "restored.db")
	if err = Restore(ctx, backup, restored); err != nil {
		t.Fatal(err)
	}
	r, err := OpenExisting(ctx, restored, false)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if got, err := r.GetReviewed(ctx, q); err != nil || got.Answer != "present" {
		t.Fatal(got, err)
	}
	before, err := os.ReadFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	if err = reader.Backup(ctx, backup); err == nil {
		t.Fatal("overwrote backup")
	}
	if err = Restore(ctx, backup, path); err == nil {
		t.Fatal("overwrote live source")
	}
	after, err := os.ReadFile(backup)
	if err != nil || string(before) != string(after) {
		t.Fatal("backup changed", err)
	}
	empty := filepath.Join(filepath.Dir(path), "empty.db")
	if err = os.WriteFile(empty, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err = reader.Backup(ctx, empty); err == nil {
		t.Fatal("overwrote existing empty file")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	failed := filepath.Join(filepath.Dir(path), "failed.db")
	if err = reader.Backup(canceled, failed); err == nil {
		t.Fatal("canceled backup accepted")
	}
	if _, err = os.Stat(failed); !os.IsNotExist(err) {
		t.Fatal("published incomplete backup", err)
	}
}

func TestMaintenanceDoesNotCreateOrMigrate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.db")
	if _, err := OpenExisting(context.Background(), path, true); err == nil {
		t.Fatal("created missing database")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	s, path := maintenanceFixture(t)
	if _, err := s.db.Exec(`INSERT INTO schema_migrations VALUES(99)`); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenExisting(context.Background(), path, false); err == nil {
		t.Fatal("accepted future schema")
	}
}
