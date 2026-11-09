package question

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
)

func syntheticImage(t *testing.T, format string, seed uint8) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.SetRGBA(0, 0, color.RGBA{R: seed, A: 255})
	var buf bytes.Buffer
	var err error
	switch format {
	case "png":
		err = png.Encode(&buf, img)
	case "jpeg":
		err = jpeg.Encode(&buf, img, nil)
	case "gif":
		err = gif.Encode(&buf, img, nil)
	default:
		t.Fatal("unsupported test image format")
	}
	if err != nil {
		t.Fatal(err)
	}
	return "data:image/" + format + ";base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

func parsedImages(t *testing.T, values ...string) []Image {
	t.Helper()
	images, err := ParseImages(values)
	if err != nil {
		t.Fatal(err)
	}
	return images
}

func TestParseImages(t *testing.T) {
	t.Parallel()
	for _, format := range []string{"png", "jpeg", "gif"} {
		value := syntheticImage(t, format, 12)
		images := parsedImages(t, value)
		if len(images) != 1 || images[0].MediaType() != "image/"+format || images[0].DataURL() != value {
			t.Fatal("image format or bytes changed")
		}
	}
	jpegURL := syntheticImage(t, "jpeg", 12)
	mislabeled := strings.Replace(jpegURL, "image/jpeg", "image/png", 1)
	if parsedImages(t, mislabeled)[0].DataURL() != jpegURL {
		t.Fatal("OCS MIME normalization was not corrected")
	}
	pngURL := syntheticImage(t, "png", 12)
	withCharset := strings.Replace(pngURL, ";base64", ";charset=utf-8;base64", 1)
	if parsedImages(t, withCharset)[0].DataURL() != pngURL {
		t.Fatal("data URL parameters were not normalized")
	}
	if images, err := ParseImages(nil); err != nil || len(images) != 0 {
		t.Fatal("empty image array rejected", err)
	}
}

func TestInvalidImages(t *testing.T) {
	t.Parallel()
	valid := syntheticImage(t, "png", 1)
	for _, values := range [][]string{
		{"https://example.invalid/image.png"},
		{"data:text/plain;base64,aGVsbG8="},
		{"data:image/svg+xml;base64,PHN2Zy8+"},
		{"data:image/png;base64,not-base64"},
		{"data:image/png;base64,aGVsbG8="},
		{"data:image/png;base64,"},
		{"data:image/png,"},
		{strings.Repeat("x", 130) + ",aGVsbG8="},
		{valid, ""},
		{valid, valid, valid, valid, valid, valid, valid},
		{"data:image/png;base64," + strings.Repeat("A", base64.StdEncoding.EncodedLen(MaxImageBytes)+4)},
	} {
		if _, err := ParseImages(values); !errors.Is(err, ErrImageInvalid) || ErrorCategory(err) != "image_invalid" {
			t.Fatal("invalid images accepted or classified incorrectly", err)
		}
	}
	_, encoded, _ := strings.Cut(valid, ",")
	data, _ := base64.StdEncoding.DecodeString(encoded)
	for _, size := range [][2]uint32{{MaxImageDimension + 1, 1}, {6000, 4000}} {
		copyData := append([]byte(nil), data...)
		binary.BigEndian.PutUint32(copyData[16:20], size[0])
		binary.BigEndian.PutUint32(copyData[20:24], size[1])
		binary.BigEndian.PutUint32(copyData[29:33], crc32.ChecksumIEEE(copyData[12:29]))
		if _, err := ParseImages([]string{"data:image/png;base64," + base64.StdEncoding.EncodeToString(copyData)}); !errors.Is(err, ErrImageInvalid) {
			t.Fatal("oversized dimensions accepted", size, err)
		}
	}
}

func TestTotalImageLimit(t *testing.T) {
	_, encoded, _ := strings.Cut(syntheticImage(t, "png", 1), ",")
	data, _ := base64.StdEncoding.DecodeString(encoded)
	padded := make([]byte, MaxImageBytes)
	copy(padded, data)
	value := "data:image/png;base64," + base64.StdEncoding.EncodeToString(padded)
	if _, err := ParseImages([]string{value, value, value, value, value}); !errors.Is(err, ErrImageInvalid) {
		t.Fatal("aggregate image limit not enforced", err)
	}
}

func TestImageQuestionIdentity(t *testing.T) {
	t.Parallel()
	text := Question{Title: "q", Options: "a b", Type: "single"}
	legacy := sha256.Sum256([]byte(`{"title":"q","options":"a b","type":"single","Namespace":"test","Version":"answer-v2"}`))
	if text.Key("test") != hex.EncodeToString(legacy[:]) || text.Version() != PromptVersion {
		t.Fatal("legacy text cache identity changed")
	}
	first, second := syntheticImage(t, "png", 1), syntheticImage(t, "png", 2)
	q := text
	q.Images = parsedImages(t, first, second)
	q.SuggestionTitle = "look at [图片1] and [图片2]"
	if q.Key("test") == text.Key("test") || q.Version() == PromptVersion {
		t.Fatal("image questions collided with text cache")
	}
	other := q
	other.Images = parsedImages(t, second, first)
	if q.Key("test") == other.Key("test") {
		t.Fatal("image order not included in key")
	}
	other.Images = parsedImages(t, first, first)
	if q.Key("test") == other.Key("test") {
		t.Fatal("different image content collided")
	}
	other = q
	other.SuggestionTitle = "different interpretation [图片1] [图片2]"
	if q.Key("test") == other.Key("test") {
		t.Fatal("suggestion text not included in key")
	}
	body, err := json.Marshal(q)
	if err != nil || strings.Contains(string(body), "base64") {
		t.Fatal("image bytes leaked into question text", err)
	}
	if q.AITitle() != q.SuggestionTitle || q.AIOptions() != q.Options {
		t.Fatal("AI text fallback incorrect")
	}
	normalized, err := q.Normalize()
	if err != nil {
		t.Fatal(err)
	}
	originalDigest := normalized.Images[0].digest
	q.Images[0] = normalized.Images[1]
	if normalized.Images[0].digest != originalDigest {
		t.Fatal("normalized question shares mutable image slice")
	}
}

func TestMissingAndInvalidImageReferences(t *testing.T) {
	t.Parallel()
	for _, q := range []Question{
		{Title: "q", SuggestionTitle: "[图片1]"},
		{Title: "q", Images: []Image{{}}},
		{Title: "q", Images: parsedImages(t, syntheticImage(t, "png", 1)), SuggestionTitle: "[图片0]"},
		{Title: "q", Images: parsedImages(t, syntheticImage(t, "png", 1)), SuggestionOptions: "[图片2]"},
	} {
		if _, err := q.Normalize(); !errors.Is(err, ErrImageInvalid) {
			t.Fatal("missing image accepted", err)
		}
	}
}

func TestRestoreImageChoice(t *testing.T) {
	t.Parallel()
	images := parsedImages(t, syntheticImage(t, "png", 1), syntheticImage(t, "png", 2))
	for _, tc := range []struct {
		options, suggestion, kind, answer, want string
	}{
		{"A. https://example.invalid/a.png\nB. https://example.invalid/b.png", "A. [图片1]\nB. [图片2]", "single", "[图片2]", "https://example.invalid/b.png"},
		{"https://example.invalid/a.png\nhttps://example.invalid/b.png", "[图片1]\n[图片2]", "single", "[图片1]", "[图片1]"},
		{"A. https://example.invalid/a.png\nB. https://example.invalid/b.png", "A. [图片1]\nB. [图片2]", "multiple", "[图片2]###[图片1]", "https://example.invalid/b.png###https://example.invalid/a.png"},
		{"A. text\nB. https://example.invalid/b.png", "A. text\nB. [图片2]", "single", "text", "text"},
	} {
		q := Question{Title: "q", Options: tc.options, SuggestionOptions: tc.suggestion, Images: images, Type: tc.kind}
		answer, err := ValidateAnswer(q, tc.answer)
		if err != nil || answer != tc.want {
			t.Fatal("image choice not restored", answer, err)
		}
		if again, err := ValidateAnswer(q, answer); err != nil || again != answer {
			t.Fatal("restored/cached answer not idempotent", err)
		}
	}
	for _, suggestion := range []string{"A. [图片1]", "A. [图片1]\nB. [图片1]"} {
		q := Question{Title: "q", Options: "A. first\nB. second", SuggestionOptions: suggestion, Images: images, Type: "single"}
		if _, err := ValidateAnswer(q, "[图片1]"); !errors.Is(err, ErrAnswerInvalid) {
			t.Fatal("ambiguous image option accepted", err)
		}
	}
	q := Question{Title: "q", Type: "unknown", Images: images}
	if _, err := ValidateAnswer(q, "[图片3]"); !errors.Is(err, ErrAnswerInvalid) {
		t.Fatal("hallucinated image reference accepted", err)
	}
}
