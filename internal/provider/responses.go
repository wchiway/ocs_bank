package provider

import (
	"encoding/json"
	"github.com/wchiway/ocs_bank/internal/config"
	"github.com/wchiway/ocs_bank/internal/question"
	"strings"
)

type responsesRequest struct {
	Model           string `json:"model"`
	Instructions    string `json:"instructions"`
	Input           string `json:"input"`
	MaxOutputTokens int    `json:"max_output_tokens"`
	Stream          bool   `json:"stream"`
	Store           bool   `json:"store"`
}

func responsesPayload(c config.Config, input string) responsesRequest {
	return responsesRequest{Model: c.Model, Instructions: prompt, Input: input, MaxOutputTokens: c.MaxTokens}
}
func parseResponses(c config.Config, raw []byte) (question.Result, error) {
	var v struct {
		Status     string          `json:"status"`
		Model      string          `json:"model"`
		Error      json.RawMessage `json:"error"`
		Incomplete json.RawMessage `json:"incomplete_details"`
		Output     []struct {
			Type    string          `json:"type"`
			Status  string          `json:"status"`
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"output"`
		Usage struct {
			Input  int64 `json:"input_tokens"`
			Output int64 `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := decode(raw, &v); err != nil {
		return question.Result{}, err
	}
	if v.Status != "completed" || (len(v.Error) > 0 && string(v.Error) != "null") || (len(v.Incomplete) > 0 && string(v.Incomplete) != "null") {
		return question.Result{}, question.ErrProtocol
	}
	var out strings.Builder
	for _, item := range v.Output {
		if item.Type == "reasoning" {
			continue
		}
		if item.Type != "message" || item.Role != "assistant" || item.Status != "completed" {
			return question.Result{}, question.ErrProtocol
		}
		text, err := textBlocks(item.Content)
		if err != nil {
			return question.Result{}, err
		}
		out.WriteString(text)
	}
	if out.Len() == 0 {
		return question.Result{}, question.ErrProtocol
	}
	if v.Model == "" {
		v.Model = c.Model
	}
	return question.Result{Answer: out.String(), Model: v.Model, InputTokens: v.Usage.Input, OutputTokens: v.Usage.Output}, nil
}
