package provider

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/wchiway/ocs_bank/internal/question"
)

func providerImages(t *testing.T) []question.Image {
	t.Helper()
	var values []string
	for _, format := range []string{"png", "jpeg"} {
		img := image.NewRGBA(image.Rect(0, 0, 2, 2))
		img.SetRGBA(0, 0, color.RGBA{R: 128, A: 255})
		var buf bytes.Buffer
		var err error
		if format == "png" {
			err = png.Encode(&buf, img)
		} else {
			err = jpeg.Encode(&buf, img, nil)
		}
		if err != nil {
			t.Fatal(err)
		}
		// Match OCS's normalization, including JPEG bytes advertised as PNG.
		values = append(values, "data:image/png;base64,"+base64.StdEncoding.EncodeToString(buf.Bytes()))
	}
	images, err := question.ParseImages(values)
	if err != nil {
		t.Fatal(err)
	}
	return images
}

func TestNativeImagePayloads(t *testing.T) {
	t.Parallel()
	images := providerImages(t)
	for _, tc := range []struct{ protocol, body string }{{"chat", chatOK}, {"message", `{"type":"message","role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":"{\"answer\":\"yes\"}"}]}`}, {"response", responsesOK}} {
		t.Run(tc.protocol, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var payload map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error(err)
					return
				}
				var messages []struct {
					Role    string          `json:"role"`
					Content json.RawMessage `json:"content"`
				}
				field := "messages"
				if tc.protocol == "response" {
					field = "input"
				}
				if err := json.Unmarshal(payload[field], &messages); err != nil {
					t.Error("image input must be a message array", err)
					return
				}
				user := messages[len(messages)-1]
				if user.Role != "user" {
					t.Error("missing user message")
				}
				var blocks []struct {
					Type     string          `json:"type"`
					Text     string          `json:"text"`
					ImageURL json.RawMessage `json:"image_url"`
					Source   struct {
						Type      string `json:"type"`
						MediaType string `json:"media_type"`
						Data      string `json:"data"`
					} `json:"source"`
				}
				if err := json.Unmarshal(user.Content, &blocks); err != nil || len(blocks) != len(images)+1 {
					t.Error("missing image content blocks", err)
					return
				}
				textIndex := 0
				if tc.protocol == "message" {
					textIndex = len(images)
				}
				var text struct {
					Title   string `json:"title"`
					Options string `json:"options"`
					Type    string `json:"type"`
				}
				if err := json.Unmarshal([]byte(blocks[textIndex].Text), &text); err != nil {
					t.Error(err)
				}
				if text.Title != "look at [图片1]" || text.Options != "A. [图片1]\nB. [图片2]" || text.Type != "single" || strings.Contains(blocks[textIndex].Text, "base64") {
					t.Error("AI did not receive suggestion text separately")
				}
				for i, image := range images {
					index := i + 1
					if tc.protocol == "message" {
						index = i
					}
					block := blocks[index]
					switch tc.protocol {
					case "chat":
						var url struct {
							URL string `json:"url"`
						}
						if err := json.Unmarshal(block.ImageURL, &url); err != nil || block.Type != "image_url" || url.URL != image.DataURL() {
							t.Error("chat image shape/order incorrect", err)
						}
					case "message":
						if block.Type != "image" || block.Source.Type != "base64" || block.Source.MediaType != image.MediaType() || block.Source.Data != image.Base64() || strings.HasPrefix(block.Source.Data, "data:") {
							t.Error("messages image shape/MIME/order incorrect")
						}
					case "response":
						var url string
						if err := json.Unmarshal(block.ImageURL, &url); err != nil || block.Type != "input_image" || url != image.DataURL() {
							t.Error("responses image shape/order incorrect", err)
						}
					}
				}
				_, _ = w.Write([]byte(strings.ReplaceAll(tc.body, `\"answer\":\"yes\"`, `\"answer\":\"[图片2]\"`)))
			}))
			defer s.Close()
			client, err := New(testConfig(s.URL, tc.protocol))
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			q := question.Question{Title: "original https://example.invalid/a.png", Options: "A. https://example.invalid/a.png\nB. https://example.invalid/b.jpg", Type: "single", Images: images, SuggestionTitle: "look at [图片1]", SuggestionOptions: "A. [图片1]\nB. [图片2]"}
			res, err := client.Generate(context.Background(), q)
			if err != nil || res.Answer != "https://example.invalid/b.jpg" {
				t.Fatal("image choice not returned in original form", res.Answer, err)
			}
		})
	}
}

func TestTextPayloadCompatibility(t *testing.T) {
	t.Parallel()
	cfg := testConfig("http://127.0.0.1", "chat")
	chat := chatPayload(cfg, "text", nil)
	messages := messagesPayload(cfg, "text", nil)
	responses := responsesPayload(cfg, "text", nil)
	if chat.Messages[1].Content != "text" || chat.Messages[0].Content != prompt || messages.Messages[0].Content != "text" || messages.System != prompt || responses.Input != "text" || responses.Instructions != prompt {
		t.Fatal("text-only requests no longer use the original schema")
	}
}

func TestImageFailureRemainsSafe(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("private unsupported-image response"))
	}))
	defer s.Close()
	client, err := New(testConfig(s.URL, "chat"))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	q := question.Question{Title: "q", Images: providerImages(t)}
	res, err := client.Generate(context.Background(), q)
	if !errors.Is(err, question.ErrProtocol) || res.Answer != "" || strings.Contains(err.Error(), "private") || calls.Load() != 1 {
		t.Fatal("unsafe image failure", err)
	}
	q.Images = []question.Image{{}}
	if _, err := client.Generate(context.Background(), q); !errors.Is(err, question.ErrImageInvalid) || calls.Load() != 1 {
		t.Fatal("invalid domain image reached upstream", err)
	}
}
