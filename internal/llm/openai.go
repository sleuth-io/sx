package llm

import (
	"context"
	"strings"
)

const openAIAPIURL = "https://api.openai.com"

// openAIProvider speaks the chat-completions dialect, which is the
// lingua franca of hosted and self-hosted LLM serving: with a custom
// base URL this one provider covers OpenAI itself plus OpenRouter,
// Groq, Mistral, Together, vLLM, LM Studio, and anything else exposing
// /v1/chat/completions.
type openAIProvider struct {
	apiKey  string
	model   string
	baseURL string // "" = api.openai.com; otherwise any compatible endpoint
}

func (p *openAIProvider) ID() string { return ProviderOpenAI }

func (p *openAIProvider) Complete(ctx context.Context, req Request) (Response, error) {
	if err := validateRequest(req); err != nil {
		return Response{}, err
	}
	model := req.Model
	if model == "" {
		model = p.model
	}
	messages := req.Messages
	// Structured output rides the prompt rather than response_format:
	// json_schema support is uneven across compatible servers, and a
	// rejected parameter would fail the whole call.
	if len(req.Schema) > 0 {
		messages = append([]Message{{Role: "system", Content: schemaInstruction(req.Schema)}}, messages...)
	}
	body := map[string]any{
		"model":    model,
		"messages": messages,
	}
	base := strings.TrimSuffix(p.baseURL, "/")
	if base == "" {
		base = openAIAPIURL
	}
	if req.MaxTokens > 0 {
		// OpenAI proper renamed the cap to max_completion_tokens;
		// most compatible servers still only know max_tokens.
		if base == openAIAPIURL {
			body["max_completion_tokens"] = req.MaxTokens
		} else {
			body["max_tokens"] = req.MaxTokens
		}
	}
	headers := map[string]string{"Authorization": "Bearer " + p.apiKey}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := postJSON(ctx, base+"/v1/chat/completions", headers, body, &out); err != nil {
		return Response{}, err
	}
	var raw string
	if len(out.Choices) > 0 {
		raw = out.Choices[0].Message.Content
	}
	text, err := finishStructured(req, raw)
	if err != nil {
		return Response{}, err
	}
	return Response{
		Text:     text,
		Provider: ProviderOpenAI,
		Model:    model,
		Usage:    Usage{InputTokens: out.Usage.PromptTokens, OutputTokens: out.Usage.CompletionTokens},
	}, nil
}
