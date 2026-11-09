package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"

	"github.com/wchiway/ocs_bank/internal/question"
)

const maxQueryBodyBytes = 17 << 20

type queryBody struct {
	Title             string
	Options           string
	Type              string
	Images            []string
	SuggestionTitle   string
	SuggestionOptions string
	Version           json.RawMessage
}

func urlValues(r *http.Request) (map[string]string, error) {
	v, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return nil, err
	}
	for _, key := range []string{"images", "suggestion_title", "suggestion_options"} {
		if _, ok := v[key]; ok {
			return nil, question.ErrImageInvalid
		}
	}
	m := map[string]string{}
	for _, key := range []string{"title", "options", "type"} {
		if len(v[key]) > 1 {
			return nil, question.ErrInvalid
		}
		m[key] = v.Get(key)
	}
	return m, nil
}

func parseQuery(w http.ResponseWriter, r *http.Request) (question.Question, error) {
	if err := r.Context().Err(); err != nil {
		return question.Question{}, err
	}
	if len(r.URL.RawQuery) > 65536 {
		return question.Question{}, question.ErrInvalid
	}
	if r.Method != http.MethodPost {
		v, err := urlValues(r)
		if err != nil {
			return question.Question{}, err
		}
		return (question.Question{Title: v["title"], Options: v["options"], Type: v["type"]}).Normalize()
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" || r.URL.RawQuery != "" {
		return question.Question{}, question.ErrInvalid
	}
	if r.ContentLength > maxQueryBodyBytes {
		return question.Question{}, &http.MaxBytesError{Limit: maxQueryBodyBytes}
	}
	body, err := decodeQueryBody(http.MaxBytesReader(w, r.Body, maxQueryBodyBytes))
	if err != nil {
		return question.Question{}, err
	}
	images, err := question.ParseImages(body.Images)
	if err != nil {
		return question.Question{}, err
	}
	if err := r.Context().Err(); err != nil {
		return question.Question{}, err
	}
	// Older OCS versions substituted missing suggestion fields with this literal.
	if body.SuggestionTitle == "undefined" {
		body.SuggestionTitle = ""
	}
	if body.SuggestionOptions == "undefined" {
		body.SuggestionOptions = ""
	}
	return (question.Question{Title: body.Title, Options: body.Options, Type: body.Type, Images: images, SuggestionTitle: body.SuggestionTitle, SuggestionOptions: body.SuggestionOptions}).Normalize()
}

func decodeQueryBody(reader io.Reader) (queryBody, error) {
	var body queryBody
	decoder := json.NewDecoder(reader)
	token, err := decoder.Token()
	if err != nil {
		return body, err
	}
	if token != json.Delim('{') {
		return body, question.ErrInvalid
	}
	seen := map[string]bool{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return body, err
		}
		key, ok := token.(string)
		if !ok || seen[key] {
			return body, question.ErrInvalid
		}
		seen[key] = true
		var value any
		switch key {
		case "title":
			value = &body.Title
		case "options":
			value = &body.Options
		case "type":
			value = &body.Type
		case "images":
			value = &body.Images
		case "suggestion_title":
			value = &body.SuggestionTitle
		case "suggestion_options":
			value = &body.SuggestionOptions
		case "v":
			value = &body.Version
		default:
			return body, question.ErrInvalid
		}
		if err := decoder.Decode(value); err != nil {
			var typeError *json.UnmarshalTypeError
			if key == "images" && errors.As(err, &typeError) {
				return body, question.ErrImageInvalid
			}
			return body, err
		}
	}
	if _, err := decoder.Token(); err != nil {
		return body, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		if err == nil {
			err = question.ErrInvalid
		}
		return body, err
	}
	return body, nil
}
