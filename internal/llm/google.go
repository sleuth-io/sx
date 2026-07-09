package llm

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

const googleAPIURL = "https://generativelanguage.googleapis.com"

// googleProvider calls the Gemini API (generateContent) with the
// user's own key.
type googleProvider struct {
	apiKey  string
	model   string
	baseURL string // test override; "" = generativelanguage.googleapis.com
}

func (p *googleProvider) ID() string { return ProviderGoogle }

func (p *googleProvider) Complete(ctx context.Context, req Request) (Response, error) {
	if err := validateRequest(req); err != nil {
		return Response{}, err
	}
	model := req.Model
	if model == "" {
		model = p.model
	}
	system, rest := splitSystem(req.Messages)
	type part struct {
		Text string `json:"text"`
	}
	type content struct {
		Role  string `json:"role,omitempty"`
		Parts []part `json:"parts"`
	}
	contents := make([]content, 0, len(rest))
	for _, m := range rest {
		role := "user"
		if m.Role == "assistant" {
			role = "model"
		}
		contents = append(contents, content{Role: role, Parts: []part{{Text: m.Content}}})
	}
	genCfg := map[string]any{}
	if req.MaxTokens > 0 {
		genCfg["maxOutputTokens"] = req.MaxTokens
	}
	if len(req.Schema) > 0 {
		// Gemini's native responseSchema is an OpenAPI subset, not full
		// JSON Schema — passing arbitrary schemas through gets rejected.
		// JSON mime type plus a prompt instruction works for any schema.
		genCfg["responseMimeType"] = "application/json"
		instr := schemaInstruction(req.Schema)
		if system == "" {
			system = instr
		} else {
			system += "\n\n" + instr
		}
	}
	body := map[string]any{"contents": contents}
	if system != "" {
		body["systemInstruction"] = content{Parts: []part{{Text: system}}}
	}
	if len(genCfg) > 0 {
		body["generationConfig"] = genCfg
	}
	base := p.baseURL
	if base == "" {
		base = googleAPIURL
	}
	endpoint := fmt.Sprintf("%s/v1beta/models/%s:generateContent?key=%s",
		base, url.PathEscape(model), url.QueryEscape(p.apiKey))
	var out struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		UsageMetadata struct {
			PromptTokenCount     int `json:"promptTokenCount"`
			CandidatesTokenCount int `json:"candidatesTokenCount"`
		} `json:"usageMetadata"`
	}
	if err := postJSON(ctx, endpoint, nil, body, &out); err != nil {
		return Response{}, err
	}
	var parts []string
	if len(out.Candidates) > 0 {
		for _, pt := range out.Candidates[0].Content.Parts {
			if pt.Text != "" {
				parts = append(parts, pt.Text)
			}
		}
	}
	text, err := finishStructured(req, strings.Join(parts, "\n"))
	if err != nil {
		return Response{}, err
	}
	return Response{
		Text:     text,
		Provider: ProviderGoogle,
		Model:    model,
		Usage: Usage{
			InputTokens:  out.UsageMetadata.PromptTokenCount,
			OutputTokens: out.UsageMetadata.CandidatesTokenCount,
		},
	}, nil
}
