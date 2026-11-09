package question

import (
	"encoding/base64"
	"errors"
	"image"
	"strings"
	"testing"
)

func TestAllAcceptedInlineImageFormsAreDetected(t *testing.T) {
	t.Parallel()
	value := syntheticImage(t, "png", 1)
	for _, inline := range []string{value, strings.Replace(value, "image/png", "IMAGE/PNG", 1), strings.Replace(value, ";base64", "; charset=utf-8;base64", 1)} {
		images := parsedImages(t, inline)
		if _, err := (Question{Title: inline}).Normalize(); !errors.Is(err, ErrImageInvalid) {
			t.Fatal("inline image without array accepted", err)
		}
		if _, err := (Question{Title: "q", Options: inline}).Normalize(); !errors.Is(err, ErrImageInvalid) {
			t.Fatal("inline option without array accepted", err)
		}
		if _, err := (Question{Title: inline, Images: images}).Normalize(); !errors.Is(err, ErrImageInvalid) {
			t.Fatal("inline image without suggestion text accepted", err)
		}
		q, err := (Question{Title: inline, Images: images, SuggestionTitle: "[图片1]"}).Normalize()
		if err != nil || q.Title != "[图片1]" || q.AITitle() != "[图片1]" {
			t.Fatal("valid inline image form not sanitized", err)
		}
	}
}

func TestWebPParsingAndMimeCorrection(t *testing.T) {
	t.Parallel()
	// A synthetic 1x1 WebP pixel, also decoded fully to validate the fixture.
	const encoded = "UklGRiIAAABXRUJQVlA4IBYAAAAwAQCdASoBAAEADsD+JaQAA3AAAAAA"
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if _, format, err := image.Decode(strings.NewReader(string(data))); err != nil || format != "webp" {
		t.Fatal("invalid WebP fixture", err)
	}
	for _, mediaType := range []string{"image/webp", "image/png"} {
		images := parsedImages(t, "data:"+mediaType+";base64,"+encoded)
		if images[0].MediaType() != "image/webp" || images[0].DataURL() != "data:image/webp;base64,"+encoded {
			t.Fatal("WebP format or MIME correction regressed")
		}
	}
}
