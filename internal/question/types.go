package question

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrInvalid       = errors.New("invalid question")
	ErrImageInvalid  = fmt.Errorf("%w: invalid image input", ErrInvalid)
	ErrNotFound      = errors.New("answer not found")
	ErrUnavailable   = errors.New("service unavailable")
	ErrBusy          = errors.New("service busy")
	ErrProtocol      = errors.New("invalid upstream response")
	ErrOutputLimit   = fmt.Errorf("%w: output token limit reached", ErrProtocol)
	ErrAnswerFormat  = fmt.Errorf("%w: answer JSON required", ErrProtocol)
	ErrAnswerInvalid = fmt.Errorf("%w: answer validation failed", ErrProtocol)
	ErrAuth          = errors.New("upstream authentication failed")
	ErrRateLimit     = errors.New("upstream rate limited")
	ErrRefused       = errors.New("upstream refused request")
	ErrStorage       = errors.New("storage unavailable")
)

const PromptVersion = "answer-v2"

type Question struct {
	Title             string  `json:"title"`
	Options           string  `json:"options"`
	Type              string  `json:"type"`
	Images            []Image `json:"-"`
	SuggestionTitle   string  `json:"suggestion_title,omitempty"`
	SuggestionOptions string  `json:"suggestion_options,omitempty"`
}

func (q Question) Normalize() (Question, error) {
	q.Title = strings.TrimSpace(q.Title)
	q.SuggestionTitle = strings.TrimSpace(q.SuggestionTitle)
	q.SuggestionOptions = strings.TrimSpace(q.SuggestionOptions)
	// Inline DOM image sources also appear in OCS's original text. Use its
	// marker text so those image bytes do not enter prompts or SQLite rows.
	if len(q.Images) == 0 && (inlineImage.MatchString(q.Title) || inlineImage.MatchString(q.Options)) {
		return q, ErrImageInvalid
	}
	if len(q.Images) > 0 && inlineImage.MatchString(q.Title) {
		if q.SuggestionTitle == "" {
			return q, ErrImageInvalid
		}
		q.Title = q.SuggestionTitle
	}
	if len(q.Images) > 0 && inlineImage.MatchString(q.Options) {
		if q.SuggestionOptions == "" {
			return q, ErrImageInvalid
		}
		q.Options = q.SuggestionOptions
	}
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
	if len(q.Images) > 0 {
		for _, text := range []string{q.AITitle(), q.AIOptions()} {
			if inlineImage.MatchString(text) || !validImageReferences(text, len(q.Images)) {
				return q, ErrImageInvalid
			}
		}
	}
	if len(q.Images) > MaxImages || len(q.SuggestionTitle) > 16384 || len(q.SuggestionOptions) > 32768 {
		return q, ErrImageInvalid
	}
	total := 0
	for _, image := range q.Images {
		if image.digest == "" || image.data == "" || !supportedImageType(image.mediaType) || image.size < 1 || image.size > MaxImageBytes {
			return q, ErrImageInvalid
		}
		total += image.size
	}
	if total > MaxTotalImageBytes || !validImageReferences(q.SuggestionTitle, len(q.Images)) || !validImageReferences(q.SuggestionOptions, len(q.Images)) {
		return q, ErrImageInvalid
	}
	if len(q.Images) == 0 {
		q.SuggestionTitle, q.SuggestionOptions = "", ""
	}
	q.Images = append([]Image(nil), q.Images...)
	return q, nil
}
func (q Question) Key(namespace string) string {
	digests := make([]string, 0, len(q.Images))
	for _, image := range q.Images {
		digests = append(digests, image.digest)
	}
	b, _ := json.Marshal(struct {
		Question
		ImageDigests []string `json:"image_hashes,omitempty"`
		Namespace    string
		Version      string
	}{q, digests, namespace, q.Version()})
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
