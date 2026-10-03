package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wchiway/ocs_bank/internal/question"
	"github.com/wchiway/ocs_bank/internal/store"
)

func TestMaintenanceCLIWithoutConfigOrSecrets(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bank.db")
	s, err := store.Open(context.Background(), path, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Put(context.Background(), "key", question.Question{Title: "q"}, question.Result{Answer: "answer", Model: "test"}); err != nil {
		t.Fatal(err)
	}
	if err = s.Invalidate(context.Background(), "key"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	var out bytes.Buffer
	if err = run([]string{"-database", path, "-action", "prune"}, &out); err != nil {
		t.Fatal(err)
	}
	var preview store.PruneResult
	if err = json.Unmarshal(out.Bytes(), &preview); err != nil || preview.Applied || preview.CacheMatched != 1 {
		t.Fatal(preview, err)
	}
	out.Reset()
	if err = run([]string{"-database", path, "-action", "prune", "-apply"}, &out); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(dir, "backup.db")
	if err = run([]string{"-database", path, "-action", "backup", "-output", backup}, &out); err != nil {
		t.Fatal(err)
	}
	if err = run([]string{"-database", backup, "-action", "restore", "-output", filepath.Join(dir, "restored.db")}, &out); err != nil {
		t.Fatal(err)
	}
	if err = run([]string{"-database", path, "-action", "stats"}, &out); err != nil {
		t.Fatal(err)
	}
	// The fixture contains no config or dotenv file; neither is needed or created.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() == ".env" || entry.Name() == "config.json" {
			t.Fatal("unexpected configuration file")
		}
	}
	for _, args := range [][]string{{}, {"-database", path, "-action", "unknown"}, {"-database", path, "-action", "stats", "-apply"}, {"-database", path, "-action", "backup"}, {"-database", path, "-action", "prune", "-batch-size", "0"}} {
		if err = run(args, &out); err == nil {
			t.Fatal("invalid flags accepted", args)
		}
	}
}
