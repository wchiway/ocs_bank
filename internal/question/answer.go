package question

import (
	"encoding/json"
	"strings"
)

// ParseAnswer accepts a JSON object, optionally in a single Markdown code fence.
// The legacy anwser spelling is tolerated, but conflicting fields are rejected.
func ParseAnswer(text string) (string, error) {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "```json\n") && strings.HasSuffix(text, "\n```") {
		text = strings.TrimSuffix(strings.TrimPrefix(text, "```json\n"), "\n```")
	} else if strings.HasPrefix(text, "```\n") && strings.HasSuffix(text, "\n```") {
		text = strings.TrimSuffix(strings.TrimPrefix(text, "```\n"), "\n```")
	}
	var v struct {
		Answer *string `json:"answer"`
		Legacy *string `json:"anwser"`
	}
	if json.Unmarshal([]byte(text), &v) != nil {
		return "", ErrProtocol
	}
	if v.Answer != nil && v.Legacy != nil && *v.Answer != *v.Legacy {
		return "", ErrProtocol
	}
	if v.Answer == nil {
		v.Answer = v.Legacy
	}
	if v.Answer == nil {
		return "", ErrProtocol
	}
	answer := strings.TrimSpace(*v.Answer)
	if answer == "" || len(answer) > 32768 {
		return "", ErrProtocol
	}
	return answer, nil
}
