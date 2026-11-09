package question

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestInlineImageSourcesAreNotQuestionText(t *testing.T) {
	t.Parallel()
	value := syntheticImage(t, "png", 1)
	q := Question{Title: "look at " + value, Options: "A. " + value + "\nB. none", Type: "single", Images: parsedImages(t, value), SuggestionTitle: "look at [图片1]", SuggestionOptions: "A. [图片1]\nB. none"}
	normalized, err := q.Normalize()
	if err != nil || normalized.Title != q.SuggestionTitle || normalized.Options != q.SuggestionOptions {
		t.Fatal("inline DOM sources were not replaced", err)
	}
	body, err := json.Marshal(normalized)
	if err != nil || strings.Contains(string(body), "base64") {
		t.Fatal("inline image bytes entered question text", err)
	}
	if answer, err := ValidateAnswer(normalized, "[图片1]"); err != nil || answer != "[图片1]" {
		t.Fatal("inline image answer marker lost", err)
	}
	q.SuggestionTitle = ""
	if _, err := q.Normalize(); !errors.Is(err, ErrImageInvalid) {
		t.Fatal("inline image without marker text accepted", err)
	}
	q.SuggestionTitle = value
	if _, err := q.Normalize(); !errors.Is(err, ErrImageInvalid) {
		t.Fatal("inline bytes allowed in AI suggestion", err)
	}
	q = Question{Title: "[图片2]", Images: parsedImages(t, value)}
	if _, err := q.Normalize(); !errors.Is(err, ErrImageInvalid) {
		t.Fatal("out-of-range original image marker accepted", err)
	}
}
