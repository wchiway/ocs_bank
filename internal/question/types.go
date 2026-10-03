package question

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
)

var (
	ErrInvalid     = errors.New("invalid question")
	ErrNotFound    = errors.New("answer not found")
	ErrUnavailable = errors.New("service unavailable")
	ErrBusy        = errors.New("service busy")
	ErrProtocol    = errors.New("invalid upstream response")
	ErrAuth        = errors.New("upstream authentication failed")
	ErrRateLimit   = errors.New("upstream rate limited")
	ErrRefused     = errors.New("upstream refused request")
	ErrStorage     = errors.New("storage unavailable")
)

const PromptVersion = "answer-v2"

type Question struct {
	Title   string `json:"title"`
	Options string `json:"options"`
	Type    string `json:"type"`
}

func (q Question) Normalize() (Question, error) {
	q.Title = strings.TrimSpace(q.Title)
	q.Type = strings.ToLower(strings.TrimSpace(q.Type))
	switch q.Type {
	case "判断题", "判断", "judgment":
		q.Type = "judgement"
	case "单选题", "单选":
		q.Type = "single"
	case "多选题", "多选":
		q.Type = "multiple"
	case "填空题", "填空":
		q.Type = "completion"
	}
	if q.Title == "" || len(q.Title) > 16384 || len(q.Options) > 32768 || len(q.Type) > 128 {
		return q, ErrInvalid
	}
	return q, nil
}
func (q Question) Key(namespace string) string {
	b, _ := json.Marshal(struct {
		Question
		Namespace string
		Version   string
	}{q, namespace, PromptVersion})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

type Result struct {
	Answer       string
	Source       string
	Model        string
	InputTokens  int64
	OutputTokens int64
	Cached       bool
	Reviewed     bool
}

func (r Result) Layer() string {
	if r.Reviewed {
		return "reviewed"
	}
	if r.Cached {
		return "cache"
	}
	if r.Answer != "" {
		return "generated"
	}
	return "none"
}

type Querier interface {
	Query(context.Context, Question) (Result, error)
}
type Generator interface {
	Generate(context.Context, Question) (Result, error)
}
type Repository interface {
	Get(context.Context, string) (Result, error)
	Put(context.Context, string, Question, Result) error
}

type ReviewedRepository interface {
	GetReviewed(context.Context, Question) (Result, error)
}
