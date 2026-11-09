package store

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/png"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wchiway/ocs_bank/internal/legacy"
	"github.com/wchiway/ocs_bank/internal/question"
)

func TestImageCacheDoesNotReuseLegacyReviewedRows(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "images.db"), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	q := question.Question{Title: "q", Type: "completion"}
	if _, err := s.Import(ctx, []legacy.Candidate{{Question: q, Answer: "reviewed"}}); err != nil {
		t.Fatal(err)
	}
	if res, err := s.GetReviewed(ctx, q); err != nil || !res.Reviewed || res.Answer != "reviewed" {
		t.Fatal("text reviewed priority changed", err)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	q.Images, err = question.ParseImages([]string{"data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())})
	if err != nil {
		t.Fatal(err)
	}
	q.SuggestionTitle = "look at [图片1]"
	if _, err := s.GetReviewed(ctx, q); !errors.Is(err, question.ErrNotFound) {
		t.Fatal("text-only reviewed row answered an image question", err)
	}
	key := q.Key("vision")
	if err := s.Put(ctx, key, q, question.Result{Answer: "image answer", Model: "vision"}); err != nil {
		t.Fatal(err)
	}
	res, err := s.Get(ctx, key)
	if err != nil || !res.Cached || res.Answer != "image answer" {
		t.Fatal("image answer not cached", err)
	}
	var title, options, answer, version string
	if err := s.db.QueryRowContext(ctx, `SELECT question,options,answer,prompt_version FROM answers WHERE cache_key=?`, key).Scan(&title, &options, &answer, &version); err != nil {
		t.Fatal(err)
	}
	if title != q.Title || options != q.Options || answer != res.Answer || version != q.Version() || strings.Contains(title+options+answer+version, "base64") {
		t.Fatal("image bytes or wrong prompt version persisted")
	}
}
