package httpapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wchiway/ocs_bank/internal/question"
)

type captureQuery struct {
	question question.Question
	calls    int
}

func (q *captureQuery) Query(_ context.Context, value question.Question) (question.Result, error) {
	q.question = value
	q.calls++
	return question.Result{Answer: "yes"}, nil
}

func queryImage(t *testing.T) string {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

func quietOptions() Options { return Options{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))} }

func TestPostQueries(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"/api/query", "/ocs/query"} {
		for _, withImage := range []bool{false, true} {
			q := &captureQuery{}
			body := map[string]any{"title": "q", "options": "A. yes\nB. no", "type": "single", "v": "2"}
			if withImage {
				body["images"] = []string{queryImage(t)}
				body["suggestion_title"] = "look at [图片1]"
				body["suggestion_options"] = "A. yes\nB. no"
			} else {
				body["images"] = []string{}
				body["suggestion_title"] = "undefined"
				body["suggestion_options"] = "undefined"
			}
			encoded, _ := json.Marshal(body)
			r := httptest.NewRequest("POST", path, bytes.NewReader(encoded))
			r.Header.Set("Content-Type", "application/json; charset=utf-8")
			w := httptest.NewRecorder()
			New(q, quietOptions()).ServeHTTP(w, r)
			if w.Code != 200 || q.calls != 1 || !strings.Contains(w.Body.String(), `"data":"yes"`) {
				t.Fatal(path, withImage, w.Code, w.Body)
			}
			if q.question.Title != "q" || q.question.Options != "A. yes\nB. no" || q.question.Type != "single" {
				t.Fatal("original fields changed")
			}
			if withImage {
				if len(q.question.Images) != 1 || q.question.AITitle() != "look at [图片1]" || q.question.Images[0].MediaType() != "image/png" {
					t.Fatal("image request not decoded")
				}
			} else if len(q.question.Images) != 0 || q.question.SuggestionTitle != "" || q.question.SuggestionOptions != "" {
				t.Fatal("text fallback incorrect")
			}
		}
	}
}

func TestInvalidPostQueries(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ body, contentType, code string }{
		{`[]`, "application/json", "invalid"},
		{`null`, "application/json", "invalid"},
		{`{"title":"q"} {}`, "application/json", "invalid"},
		{`{"title":"q","title":"other"}`, "application/json", "invalid"},
		{`{"title":"q","unknown":"private"}`, "application/json", "invalid"},
		{`{"title":123}`, "application/json", "invalid"},
		{`{"title":null}`, "application/json", "invalid"},
		{`{"title":"q","options":null}`, "application/json", "invalid"},
		{`{"title":"q","type":null}`, "application/json", "invalid"},
		{`{"title":"q","suggestion_title":null}`, "application/json", "invalid"},
		{`{"title":"q","suggestion_options":null}`, "application/json", "invalid"},
		{`{"title":"q","images":null}`, "application/json", "image_invalid"},
		{`{"title":"q","images":[null]}`, "application/json", "image_invalid"},
		{`{"title":"q"}`, "text/plain", "invalid"},
		{`{"title":"q"}`, "", "invalid"},
		{`{"title":"q","images":"not an array"}`, "application/json", "image_invalid"},
		{`{"title":"q","images":["https://example.invalid/image.png"]}`, "application/json", "image_invalid"},
		{`{"title":"q","images":["data:image/png;base64,cHJpdmF0ZQ=="]}`, "application/json", "image_invalid"},
		{`{"title":"q","suggestion_title":"[图片1]"}`, "application/json", "image_invalid"},
	} {
		for _, path := range []string{"/api/query", "/ocs/query"} {
			q := &captureQuery{}
			r := httptest.NewRequest("POST", path, strings.NewReader(tc.body))
			r.Header.Set("Content-Type", tc.contentType)
			w := httptest.NewRecorder()
			New(q, quietOptions()).ServeHTTP(w, r)
			want := 400
			if path == "/ocs/query" {
				want = 200
			}
			var got response
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if w.Code != want || got.Success || got.Data != nil || got.ErrorCode != tc.code || q.calls != 0 || strings.Contains(w.Body.String(), "private") {
				t.Fatal(path, w.Code, w.Body, q.calls)
			}
		}
	}
	q := &captureQuery{}
	for _, path := range []string{"/api/query?title=q&images=x", "/api/query?title=q&suggestion_title=x"} {
		w := httptest.NewRecorder()
		New(q, quietOptions()).ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 400 || q.calls != 0 {
			t.Fatal("GET image parameters were silently ignored")
		}
	}
	r := httptest.NewRequest("POST", "/api/query?title=other", strings.NewReader(`{"title":"q"}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	New(q, quietOptions()).ServeHTTP(w, r)
	if w.Code != 400 || q.calls != 0 {
		t.Fatal("ambiguous URL/body query accepted")
	}
}

type spaces struct{}

func (spaces) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = ' '
	}
	return len(p), nil
}

func TestPostBodyLimit(t *testing.T) {
	for _, path := range []string{"/api/query", "/ocs/query"} {
		for _, knownLength := range []bool{true, false} {
			q := &captureQuery{}
			var body io.Reader = strings.NewReader(`{"title":"q"}`)
			if !knownLength {
				body = io.MultiReader(body, io.LimitReader(spaces{}, maxQueryBodyBytes))
			}
			r := httptest.NewRequest("POST", path, body)
			r.Header.Set("Content-Type", "application/json")
			if knownLength {
				r.ContentLength = maxQueryBodyBytes + 1
			}
			w := httptest.NewRecorder()
			New(q, quietOptions()).ServeHTTP(w, r)
			want := 413
			if path == "/ocs/query" {
				want = 200
			}
			if w.Code != want || q.calls != 0 || !strings.Contains(w.Body.String(), `"error_code":"request_too_large"`) {
				t.Fatal("body limit not enforced", w.Code, w.Body, q.calls)
			}
		}
	}
}

type unread struct{ read bool }

func (b *unread) Read([]byte) (int, error) {
	b.read = true
	return 0, errors.New("body must not be read")
}

func TestPostAccessControlBeforeBodyRead(t *testing.T) {
	q := &captureQuery{}
	o := quietOptions()
	o.Token, o.RequestsPerMinute = "synthetic-token", 1
	h := New(q, o)
	body := &unread{}
	r := httptest.NewRequest("POST", "/api/query", body)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 || body.read || q.calls != 0 {
		t.Fatal("unauthorized body read")
	}
	r = httptest.NewRequest("POST", "/api/query", strings.NewReader(`{"title":"q"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer synthetic-token")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || q.calls != 1 {
		t.Fatal("authorized request rejected", w.Body)
	}
	body = &unread{}
	r = httptest.NewRequest("POST", "/api/query", body)
	r.Header.Set("Authorization", "Bearer synthetic-token")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 429 || body.read || q.calls != 1 {
		t.Fatal("rate-limited body read")
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("HEAD", "/ocs/query", nil))
	if w.Code != 200 || w.Body.Len() != 0 || q.calls != 1 {
		t.Fatal("HEAD probe compatibility lost")
	}
}

func TestPostReadDeadline(t *testing.T) {
	q := &captureQuery{}
	o := quietOptions()
	o.Timeout = 50 * time.Millisecond
	s := httptest.NewServer(New(q, o))
	t.Cleanup(s.Close)
	conn, err := net.DialTimeout("tcp", strings.TrimPrefix(s.URL, "http://"), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	_ = conn.SetDeadline(time.Now().Add(time.Second))
	_, err = io.WriteString(conn, "POST /api/query HTTP/1.1\r\nHost: localhost\r\nContent-Type: application/json\r\nContent-Length: 100\r\n\r\n{\"title\":\"q\"")
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: "POST"})
	if err != nil {
		t.Fatal("incomplete request did not finish within the deadline", err)
	}
	_ = res.Body.Close()
	if res.StatusCode != 408 || q.calls != 0 {
		t.Fatal("incomplete body reached query service", res.StatusCode, q.calls)
	}
}
