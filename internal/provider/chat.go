package provider

import (
	"encoding/json"
	"github.com/wchiway/ocs_bank/internal/config"
	"github.com/wchiway/ocs_bank/internal/question"
)

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type chatRequest struct {
	Model               string        `json:"model"`
	Messages            []chatMessage `json:"messages"`
	MaxTokens           int           `json:"max_tokens,omitempty"`
	MaxCompletionTokens int           `json:"max_completion_tokens,omitempty"`
	Stream              bool          `json:"stream"`
}

func chatPayload(c config.Config, input string) chatRequest {
	r := chatRequest{Model: c.Model, Messages: []chatMessage{{"system", prompt}, {"user", input}}}
	if c.ChatTokenParameter == "max_completion_tokens" {
		r.MaxCompletionTokens = c.MaxTokens
	} else {
		r.MaxTokens = c.MaxTokens
	}
	return r
}
func parseChat(c config.Config, raw []byte) (question.Result, error) {
	var v struct {
		Model   string          `json:"model"`
		Error   json.RawMessage `json:"error"`
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
				Refusal string          `json:"refusal"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			Input  int64 `json:"prompt_tokens"`
			Output int64 `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := decode(raw, &v); err != nil {
		return question.Result{}, err
	}
	if len(v.Choices) != 1 || (len(v.Error) > 0 && string(v.Error) != "null") {
		return question.Result{}, question.ErrProtocol
	}
	choice := v.Choices[0]
	if choice.Message.Refusal != "" {
		return question.Result{}, question.ErrRefused
	}
	if choice.Message.Role != "assistant" {
		return question.Result{}, question.ErrProtocol
	}
	if err := completed(choice.FinishReason, "stop"); err != nil {
		return question.Result{}, err
	}
	text, err := textBlocks(choice.Message.Content)
	if err != nil {
		return question.Result{}, err
	}
	if v.Model == "" {
		v.Model = c.Model
	}
	return question.Result{Answer: text, Model: v.Model, InputTokens: v.Usage.Input, OutputTokens: v.Usage.Output}, nil
}
