package provider

import (
	"encoding/json"
	"github.com/wchiway/ocs_bank/internal/config"
	"github.com/wchiway/ocs_bank/internal/question"
	"strings"
)

type responsesRequest struct {
	Model           string              `json:"model"`
	Instructions    string              `json:"instructions"`
	Input           any                 `json:"input"`
	MaxOutputTokens int                 `json:"max_output_tokens"`
	Stream          bool                `json:"stream"`
	Store           bool                `json:"store"`
	Reasoning       *responsesReasoning `json:"reasoning,omitempty"`
}

type responsesReasoning struct {
	Effort string `json:"effort"`
}

type responsesInputMessage struct {
	Role    string             `json:"role"`
	Content []responsesContent `json:"content"`
}
type responsesContent struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
}

func responsesPayload(c config.Config, input string, images []question.Image) responsesRequest {
	var content any = input
	if len(images) > 0 {
		blocks := make([]responsesContent, 0, len(images)+1)
		blocks = append(blocks, responsesContent{Type: "input_text", Text: input})
		for _, image := range images {
			blocks = append(blocks, responsesContent{Type: "input_image", ImageURL: image.DataURL()})
		}
		content = []responsesInputMessage{{Role: "user", Content: blocks}}
	}
	r := responsesRequest{Model: c.Model, Instructions: generationPrompt(images), Input: content, MaxOutputTokens: c.MaxTokens}
	if c.ReasoningEffort != "" {
		r.Reasoning = &responsesReasoning{Effort: c.ReasoningEffort}
	}
	return r
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
	if v.Status == "incomplete" {
		var details struct {
			Reason string `json:"reason"`
		}
		if json.Unmarshal(v.Incomplete, &details) == nil && details.Reason == "max_output_tokens" {
			return question.Result{}, question.ErrOutputLimit
		}
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
