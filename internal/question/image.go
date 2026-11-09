package question

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"mime"
	"regexp"
	"strconv"
	"strings"

	_ "golang.org/x/image/webp"
)

const (
	MaxImages          = 6
	MaxImageBytes      = 3 << 20
	MaxTotalImageBytes = 12 << 20
	MaxImagePixels     = 20_000_000
	MaxImageDimension  = 8192
)

// Image holds validated, immutable image data. It is never marshaled as question text.
type Image struct {
	mediaType string
	data      string
	digest    string
	size      int
}

func (i Image) MediaType() string { return i.mediaType }
func (i Image) Base64() string    { return i.data }
func (i Image) DataURL() string   { return "data:" + i.mediaType + ";base64," + i.data }

var imageReference = regexp.MustCompile(`\[图片([0-9]+)\]`)
var inlineImage = regexp.MustCompile(`data:image/[^\s,]+;base64,`)

// ParseImages accepts inline images only; it never downloads client-supplied URLs.
func ParseImages(values []string) ([]Image, error) {
	if len(values) > MaxImages {
		return nil, ErrImageInvalid
	}
	images := make([]Image, 0, len(values))
	total := 0
	for _, value := range values {
		header, encoded, ok := strings.Cut(strings.TrimSpace(value), ",")
		if !ok || len(header) > 128 || !strings.HasPrefix(header, "data:") || !strings.HasSuffix(header, ";base64") {
			return nil, ErrImageInvalid
		}
		declared, _, err := mime.ParseMediaType(strings.TrimSuffix(strings.TrimPrefix(header, "data:"), ";base64"))
		if err != nil || !supportedImageType(declared) || len(encoded) > base64.StdEncoding.EncodedLen(MaxImageBytes) {
			return nil, ErrImageInvalid
		}
		data, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || len(data) == 0 || len(data) > MaxImageBytes {
			return nil, ErrImageInvalid
		}
		total += len(data)
		if total > MaxTotalImageBytes {
			return nil, ErrImageInvalid
		}
		cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > MaxImageDimension || cfg.Height > MaxImageDimension || cfg.Width > MaxImagePixels/cfg.Height {
			return nil, ErrImageInvalid
		}
		mediaType := map[string]string{"png": "image/png", "jpeg": "image/jpeg", "gif": "image/gif", "webp": "image/webp"}[format]
		if mediaType == "" {
			return nil, ErrImageInvalid
		}
		// OCS can label GM-downloaded JPEG/WebP bytes as PNG without transcoding.
		// Use the detected format instead of forwarding that incorrect MIME type.
		h := sha256.Sum256(data)
		images = append(images, Image{mediaType: mediaType, data: base64.StdEncoding.EncodeToString(data), digest: hex.EncodeToString(h[:]), size: len(data)})
	}
	return images, nil
}

func supportedImageType(mediaType string) bool {
	switch mediaType {
	case "image/png", "image/jpeg", "image/jpg", "image/gif", "image/webp":
		return true
	}
	return false
}

func validImageReferences(text string, count int) bool {
	for _, match := range imageReference.FindAllStringSubmatch(text, -1) {
		n, err := strconv.Atoi(match[1])
		if err != nil || n < 1 || n > count {
			return false
		}
	}
	return true
}

func (q Question) AITitle() string {
	if len(q.Images) > 0 && q.SuggestionTitle != "" {
		return q.SuggestionTitle
	}
	return q.Title
}

func (q Question) AIOptions() string {
	if len(q.Images) > 0 && q.SuggestionOptions != "" {
		return q.SuggestionOptions
	}
	return q.Options
}

func (q Question) Version() string {
	if len(q.Images) > 0 {
		return "answer-image-v1"
	}
	return PromptVersion
}

// restoreImageChoice maps unambiguous labeled options back to the original text.
// Opaque/multiline options retain their markers for OCS to restore without guessing.
func restoreImageChoice(q Question, part string) (string, error) {
	if len(q.Images) == 0 || q.SuggestionOptions == "" || (q.Type != "single" && q.Type != "multiple") {
		return part, nil
	}
	if choiceContents(q.Options) == nil || choiceContents(q.SuggestionOptions) == nil {
		return part, nil
	}
	original := optionLines(q.Options)
	suggested := optionLines(q.SuggestionOptions)
	for _, value := range original {
		if part == value {
			return part, nil
		}
	}
	if len(original) == len(suggested) {
		found := -1
		for i, value := range suggested {
			if part == value {
				if found >= 0 {
					return "", ErrAnswerInvalid
				}
				found = i
			}
		}
		if found >= 0 {
			return original[found], nil
		}
	}
	if imageReference.MatchString(part) {
		return "", ErrAnswerInvalid
	}
	return part, nil
}

func optionLines(raw string) []string {
	lines := strings.Split(strings.TrimSpace(strings.ReplaceAll(raw, "\r\n", "\n")), "\n")
	labelled := choiceContents(raw) != nil
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if labelled {
			line = strings.TrimSpace(optionLabel.FindStringSubmatch(line)[2])
		}
		lines[i] = line
	}
	return lines
}
