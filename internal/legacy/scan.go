// Package legacy inspects an offline SQLite backup without modifying it.
package legacy

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/wchiway/ocs_bank/internal/question"
	_ "modernc.org/sqlite"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Candidate struct {
	Question question.Question
	Answer   string
}
type Report struct {
	Total           int            `json:"total"`
	Invalid         int            `json:"invalid"`
	Duplicates      int            `json:"duplicates"`
	ConflictingKeys int            `json:"conflicting_keys"`
	Eligible        int            `json:"eligible"`
	Imported        int            `json:"imported"`
	Reasons         map[string]int `json:"reasons"`
}

func Scan(ctx context.Context, path string) (Report, []Candidate, error) {
	report := Report{Reasons: map[string]int{}}
	abs, err := filepath.Abs(path)
	if err != nil {
		return report, nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return report, nil, err
	}
	if !info.Mode().IsRegular() {
		return report, nil, fmt.Errorf("source must be a regular SQLite backup file")
	}
	// Immutable mode is safe only for offline, checkpointed snapshots, not a live database.
	for _, suffix := range []string{"-wal", "-journal"} {
		if info, err := os.Stat(abs + suffix); err == nil && info.Size() > 0 {
			return report, nil, fmt.Errorf("source has %s; create a consistent SQLite backup first", suffix)
		}
	}
	u := url.URL{Scheme: "file", Path: abs, RawQuery: "mode=ro&immutable=1"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return report, nil, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	rows, err := db.QueryContext(ctx, `SELECT question,answer,options,type FROM question_answer ORDER BY id`)
	if err != nil {
		return report, nil, err
	}
	defer rows.Close()
	candidates := map[string]Candidate{}
	conflicts := map[string]bool{}
	for rows.Next() {
		report.Total++
		var title, answer, options, typ sql.NullString
		if err = rows.Scan(&title, &answer, &options, &typ); err != nil {
			return report, nil, err
		}
		q, err := (question.Question{Title: title.String, Options: options.String, Type: typ.String}).Normalize()
		reason := ""
		a := strings.TrimSpace(answer.String)
		switch {
		case err != nil:
			reason = "invalid_question"
		case q.Type != "single" && q.Type != "multiple" && q.Type != "judgement" && q.Type != "completion":
			reason = "missing_or_unknown_type"
		case (q.Type == "single" || q.Type == "multiple") && strings.TrimSpace(q.Options) == "":
			reason = "missing_options"
		case a == "" || len(a) > 32768:
			reason = "invalid_answer"
		case errorAnswer(a):
			reason = "legacy_error_text"
		case strings.HasPrefix(a, "{") || strings.HasPrefix(a, "```"):
			reason = "unparsed_answer"
		}
		if reason == "" {
			a, err = question.ValidateAnswer(q, a)
			if err != nil {
				reason = "invalid_answer_format"
			}
		}
		if reason != "" {
			report.Invalid++
			report.Reasons[reason]++
			continue
		}
		key := q.Key("legacy-scan")
		if old, ok := candidates[key]; ok {
			report.Duplicates++
			if old.Answer != a {
				conflicts[key] = true
			}
		} else {
			candidates[key] = Candidate{Question: q, Answer: a}
		}
	}
	if err = rows.Err(); err != nil {
		return report, nil, err
	}
	keys := make([]string, 0, len(candidates))
	for k := range candidates {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var eligible []Candidate
	for _, k := range keys {
		if !conflicts[k] {
			eligible = append(eligible, candidates[k])
		}
	}
	report.ConflictingKeys = len(conflicts)
	report.Eligible = len(eligible)
	return report, eligible, nil
}
func errorAnswer(s string) bool {
	for _, part := range []string{"API调用失败", "无法从API获取答案", "API请求异常", "API调用超时", "API响应解析失败", "多次尝试后仍无法获取答案"} {
		if strings.Contains(s, part) {
			return true
		}
	}
	return false
}
