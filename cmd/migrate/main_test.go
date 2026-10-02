package main

import (
	"database/sql"
	"encoding/json"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"testing"
)

func TestApplySafeguards(t *testing.T) {
	if err := run([]string{"-source", "missing", "-apply"}); err == nil {
		t.Fatal("apply accepted without review")
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "legacy.db")
	db, err := sql.Open("sqlite", source)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE question_answer(id INTEGER PRIMARY KEY,question TEXT,answer TEXT,options TEXT,type TEXT); INSERT INTO question_answer VALUES(1,'q','answer','','completion')`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	// Inspection needs neither service configuration nor provider credentials.
	if err = run([]string{"-source", source}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("IMPORT_TEST_KEY", "not-a-real-key")
	cfg := map[string]any{"database": source, "model": "test", "base_url": "https://example.com/v1", "api_key_env": "IMPORT_TEST_KEY"}
	p := filepath.Join(dir, "config.json")
	b, _ := json.Marshal(cfg)
	os.WriteFile(p, b, 0600)
	if err = run([]string{"-source", source, "-apply", "-reviewed", "-config", p}); err == nil {
		t.Fatal("same source and target accepted")
	}
	cfg["database"] = filepath.Join(dir, "target.db")
	b, _ = json.Marshal(cfg)
	os.WriteFile(p, b, 0600)
	if err = run([]string{"-source", source, "-apply", "-reviewed", "-config", p}); err != nil {
		t.Fatal(err)
	}
	target, err := sql.Open("sqlite", cfg["database"].(string))
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	var n int
	if err = target.QueryRow(`SELECT count(*) FROM answers WHERE source='legacy-reviewed'`).Scan(&n); err != nil || n != 1 {
		t.Fatal(n, err)
	}
}
