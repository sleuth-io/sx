package llm

import (
	"context"
	"strings"
)

// anthropicDefaultModel is used when the user leaves the model blank.
const anthropicDefaultModel = "claude-opus-4-8"

const anthropicAPIURL = "https://api.anthropic.com"

// anthropicProvider calls the Anthropic Messages API with the user's
// own key (the sanctioned BYO-key path — reusing a Claude subscription
// from third-party software is against Anthropic's ToS, which is why
// the claude-cli provider is a separate, user-initiated choice).
type anthropicProvider struct {
	apiKey  string
	model   string
	baseURL string // test override; "" = api.anthropic.com
}

func (p *anthropicProvider) ID() string { return ProviderAnthropic }

func (p *anthropicProvider) Complete(ctx context.Context, req Request) (Response, error) {
	if err := validateRequest(req); err != nil {
		return Response{}, err
	}
	model := req.Model
	if model == "" {
		model = p.model
	}
	if model == "" {
		model = anthropicDefaultModel
	}
	system, rest := splitSystem(req.Messages)
	// Structured output rides the prompt: schema-following is a core
	// competency of every current Claude model, and one instruction
	// path works for all of them.
	if len(req.Schema) > 0 {
		instr := schemaInstruction(req.Schema)
		if system == "" {
			system = instr
		} else {
			system += "\n\n" + instr
		}
	}
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = DefaultMaxTokens
	}
	body := map[string]any{
		"model":      model,
		"max_tokens": maxTokens,
		"messages":   rest,
	}
	if system != "" {
		body["system"] = system
	}
	base := p.baseURL
	if base == "" {
		base = anthropicAPIURL
	}
	headers := map[string]string{
		"x-api-key":         p.apiKey,
		"anthropic-version": "2023-06-01",
	}
	var out struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
		Usage      struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := postJSON(ctx, base+"/v1/messages", headers, body, &out); err != nil {
		return Response{}, err
	}
	var parts []string
	for _, c := range out.Content {
		if c.Type == "text" && c.Text != "" {
			parts = append(parts, c.Text)
		}
	}
	text, err := finishStructured(req, strings.Join(parts, "\n"))
	if err != nil {
		return Response{}, err
	}
	return Response{
		Text:     text,
		Provider: ProviderAnthropic,
		Model:    model,
		Usage:    Usage{InputTokens: out.Usage.InputTokens, OutputTokens: out.Usage.OutputTokens},
	}, nil
}
