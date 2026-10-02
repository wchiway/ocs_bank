package legacy

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestScanReadOnlyAndFiltering(t *testing.T) {
	p := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`CREATE TABLE question_answer(id INTEGER PRIMARY KEY,question TEXT,answer TEXT,options TEXT,type TEXT)`); err != nil {
		t.Fatal(err)
	}
	for _, r := range [][4]string{{"one", "a", "a b", "single"}, {"one", "a", "a b", "single"}, {"conflict", "a", "a b", "single"}, {"conflict", "b", "a b", "single"}, {"bad", "API调用超时，请稍后再试", "", "completion"}, {"unknown", "x", "", ""}, {"missing", "a", "", "single"}, {"blank", "", "", "completion"}, {"fill", "x", "", "填空题"}} {
		if _, err = db.Exec(`INSERT INTO question_answer(question,answer,options,type) VALUES(?,?,?,?)`, r[0], r[1], r[2], r[3]); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	before, _ := os.ReadFile(p)
	report, items, err := Scan(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if report.Total != 9 || report.Invalid != 4 || report.Duplicates != 2 || report.ConflictingKeys != 1 || len(items) != 2 {
		t.Fatalf("%+v", report)
	}
	after, _ := os.ReadFile(p)
	if sha256.Sum256(before) != sha256.Sum256(after) {
		t.Fatal("source modified")
	}
	if err = os.WriteFile(p+"-wal", []byte("not an offline snapshot"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err = Scan(context.Background(), p); err == nil {
		t.Fatal("accepted live WAL")
	}
}
