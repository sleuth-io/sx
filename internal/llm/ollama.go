package llm

import (
	"context"
	"encoding/json"
)

// ollamaProvider talks to a local (or LAN) Ollama server via its native
// chat API. Ollama supports constrained output natively: the "format"
// field accepts a full JSON Schema.
type ollamaProvider struct {
	baseURL string
	model   string
}

func (p *ollamaProvider) ID() string { return ProviderOllama }

func (p *ollamaProvider) Complete(ctx context.Context, req Request) (Response, error) {
	if err := validateRequest(req); err != nil {
		return Response{}, err
	}
	model := req.Model
	if model == "" {
		model = p.model
	}
	body := map[string]any{
		"model":    model,
		"messages": req.Messages,
		"stream":   false,
	}
	if len(req.Schema) > 0 {
		body["format"] = json.RawMessage(req.Schema)
	}
	if req.MaxTokens > 0 {
		body["options"] = map[string]any{"num_predict": req.MaxTokens}
	}
	var out struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		PromptEvalCount int `json:"prompt_eval_count"`
		EvalCount       int `json:"eval_count"`
	}
	if err := postJSON(ctx, p.baseURL+"/api/chat", nil, body, &out); err != nil {
		return Response{}, err
	}
	text, err := finishStructured(req, out.Message.Content)
	if err != nil {
		return Response{}, err
	}
	return Response{
		Text:     text,
		Provider: ProviderOllama,
		Model:    model,
		Usage:    Usage{InputTokens: out.PromptEvalCount, OutputTokens: out.EvalCount},
	}, nil
}
