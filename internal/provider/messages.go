package provider

import (
	"encoding/json"
	"github.com/wchiway/ocs_bank/internal/config"
	"github.com/wchiway/ocs_bank/internal/question"
)

type messagesRequest struct {
	Model     string        `json:"model"`
	System    string        `json:"system"`
	Messages  []chatMessage `json:"messages"`
	MaxTokens int           `json:"max_tokens"`
	Stream    bool          `json:"stream"`
}

func messagesPayload(c config.Config, input string) messagesRequest {
	return messagesRequest{Model: c.Model, System: prompt, Messages: []chatMessage{{"user", input}}, MaxTokens: c.MaxTokens}
}
func parseMessages(c config.Config, raw []byte) (question.Result, error) {
	var v struct {
		Type       string          `json:"type"`
		Role       string          `json:"role"`
		Model      string          `json:"model"`
		Content    json.RawMessage `json:"content"`
		StopReason string          `json:"stop_reason"`
		Usage      struct {
			Input  int64 `json:"input_tokens"`
			Output int64 `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := decode(raw, &v); err != nil {
		return question.Result{}, err
	}
	if v.Type != "message" || v.Role != "assistant" {
		return question.Result{}, question.ErrProtocol
	}
	if err := completed(v.StopReason, "end_turn", "stop_sequence"); err != nil {
		return question.Result{}, err
	}
	text, err := textBlocks(v.Content)
	if err != nil {
		return question.Result{}, err
	}
	if v.Model == "" {
		v.Model = c.Model
	}
	return question.Result{Answer: text, Model: v.Model, InputTokens: v.Usage.Input, OutputTokens: v.Usage.Output}, nil
}
