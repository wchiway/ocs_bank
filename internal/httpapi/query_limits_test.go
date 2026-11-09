package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wchiway/ocs_bank/internal/question"
)

func TestImageCountStopsBeforeDecodingArrayTail(t *testing.T) {
	t.Parallel()
	value, _ := json.Marshal(queryImage(t))
	prefix := `{"title":"q","images":[` + strings.TrimSuffix(strings.Repeat(string(value)+",", question.MaxImages), ",") + `,"`
	tail := &unread{}
	_, err := decodeQueryBody(io.MultiReader(strings.NewReader(prefix), tail))
	if !errors.Is(err, question.ErrImageInvalid) || tail.read {
		t.Fatal("decoder read past the permitted image count", err, tail.read)
	}
}

func TestInlineImagesWithoutArrayDoNotReachService(t *testing.T) {
	t.Parallel()
	value := queryImage(t)
	for _, inline := range []string{value, strings.Replace(value, "image/png", "IMAGE/PNG", 1), strings.Replace(value, ";base64", "; charset=utf-8;base64", 1)} {
		for _, path := range []string{"/api/query", "/ocs/query"} {
			q := &captureQuery{}
			body, _ := json.Marshal(map[string]any{"title": inline, "images": []string{}})
			r := httptest.NewRequest("POST", path, bytes.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			New(q, quietOptions()).ServeHTTP(w, r)
			want := 400
			if path == "/ocs/query" {
				want = 200
			}
			if w.Code != want || q.calls != 0 || !strings.Contains(w.Body.String(), `"error_code":"image_invalid"`) || strings.Contains(w.Body.String(), "base64") {
				t.Fatal("inline image reached service or leaked into response", w.Code, w.Body)
			}
		}
	}
}
