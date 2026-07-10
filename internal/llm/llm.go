// Package llm provides provider-agnostic LLM completion for the sx app.
// The user picks ONE configured provider (an installed CLI, a local
// Ollama server, or a hosted API reached with their own key) and every
// caller — extensions via the sx.llm bridge, future core features —
// goes through the same Complete call. Nothing in the request or
// response types is specific to any vendor.
package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Message is one turn of a conversation. Role is "system", "user", or
// "assistant"; providers that have no native system slot fold system
// messages into the prompt.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Request is a provider-neutral completion request. When Schema (a JSON
// Schema document) is set, the provider must return a JSON document
// matching it in Response.Text — natively where the backend supports
// constrained output, via prompt instruction plus extraction otherwise.
type Request struct {
	Messages  []Message       `json:"messages"`
	Schema    json.RawMessage `json:"schema,omitempty"`
	Model     string          `json:"model,omitempty"`
	MaxTokens int             `json:"maxTokens,omitempty"`
}

// Usage reports token consumption when the backend exposes it; zero
// values mean "unknown" (CLI providers often can't report it).
type Usage struct {
	InputTokens  int `json:"inputTokens"`
	OutputTokens int `json:"outputTokens"`
}

// Response is the result of a completion. Text holds plain prose, or a
// bare JSON document when the request carried a Schema.
type Response struct {
	Text     string `json:"text"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Usage    Usage  `json:"usage"`
}

// Provider is one way of reaching a model. Implementations must be safe
// for concurrent use.
type Provider interface {
	ID() string
	Complete(ctx context.Context, req Request) (Response, error)
}

// Provider IDs. These are the values persisted in the app's LLM config
// and shown in the settings picker; they never change meaning.
const (
	ProviderClaudeCLI = "claude-cli"
	ProviderCodexCLI  = "codex-cli"
	ProviderGeminiCLI = "gemini-cli"
	ProviderOllama    = "ollama"
	ProviderAnthropic = "anthropic"
	ProviderOpenAI    = "openai"
	ProviderGoogle    = "google"
)

// Config is the user's provider selection, persisted by the app. The
// API key is resolved from the OS keyring at call time and never
// stored here. BaseURL overrides the provider's default endpoint:
// Ollama's server address, an OpenAI-compatible endpoint (OpenRouter,
// Groq, vLLM, LM Studio, …), or a gateway in front of Anthropic or
// Google. CLI providers ignore it.
type Config struct {
	Provider string `json:"provider"`
	Model    string `json:"model,omitempty"`
	BaseURL  string `json:"baseUrl,omitempty"`
}

// DefaultMaxTokens bounds output when the caller doesn't ask for a
// specific budget.
const DefaultMaxTokens = 8192

// New builds the Provider for a config. apiKey is the user's key for
// hosted-API providers (empty for CLI and Ollama providers).
func New(cfg Config, apiKey string) (Provider, error) {
	switch cfg.Provider {
	case ProviderClaudeCLI, ProviderCodexCLI, ProviderGeminiCLI:
		path, ok := findCLI(cfg.Provider)
		if !ok {
			return nil, fmt.Errorf("%s not found on this machine — reselect a provider in Settings", cliBinaryName(cfg.Provider))
		}
		return &cliProvider{id: cfg.Provider, binPath: path, model: cfg.Model}, nil
	case ProviderOllama:
		if cfg.Model == "" {
			return nil, errors.New("no Ollama model selected — pick one in Settings")
		}
		return &ollamaProvider{baseURL: ollamaBaseURL(cfg.BaseURL), model: cfg.Model}, nil
	case ProviderAnthropic:
		if apiKey == "" {
			return nil, errors.New("no Anthropic API key set — add one in Settings")
		}
		return &anthropicProvider{apiKey: apiKey, model: cfg.Model, baseURL: cfg.BaseURL}, nil
	case ProviderOpenAI:
		if apiKey == "" {
			return nil, errors.New("no API key set — add one in Settings")
		}
		if cfg.Model == "" {
			return nil, errors.New("no model set — enter one in Settings")
		}
		return &openAIProvider{apiKey: apiKey, model: cfg.Model, baseURL: cfg.BaseURL}, nil
	case ProviderGoogle:
		if apiKey == "" {
			return nil, errors.New("no Google API key set — add one in Settings")
		}
		if cfg.Model == "" {
			return nil, errors.New("no model set — enter one in Settings")
		}
		return &googleProvider{apiKey: apiKey, model: cfg.Model, baseURL: cfg.BaseURL}, nil
	case "":
		return nil, errors.New("no AI provider configured — choose one in Settings")
	default:
		return nil, fmt.Errorf("unknown AI provider %q", cfg.Provider)
	}
}

// splitSystem separates system messages (joined into one string) from
// the conversational turns, for backends with a dedicated system slot.
func splitSystem(msgs []Message) (system string, rest []Message) {
	var sys []string
	for _, m := range msgs {
		if m.Role == "system" {
			sys = append(sys, m.Content)
			continue
		}
		rest = append(rest, m)
	}
	return strings.Join(sys, "\n\n"), rest
}

// flattenPrompt renders a conversation as one prompt string for
// backends that only accept a single prompt (the CLI providers).
func flattenPrompt(msgs []Message) string {
	var b strings.Builder
	for _, m := range msgs {
		switch m.Role {
		case "system":
			b.WriteString(m.Content)
		case "assistant":
			b.WriteString("Assistant: " + m.Content)
		default:
			b.WriteString(m.Content)
		}
		b.WriteString("\n\n")
	}
	return strings.TrimSpace(b.String())
}

// schemaInstruction is appended to the prompt for providers without
// native constrained output. Extraction (extractJSON) tolerates fenced
// or prefixed replies, but asking for bare JSON keeps that path rare.
func schemaInstruction(schema json.RawMessage) string {
	return "Respond with ONLY a JSON document that validates against this JSON Schema — " +
		"no markdown fences, no commentary before or after:\n" + string(schema)
}

func validateRequest(req Request) error {
	if len(req.Messages) == 0 {
		return errors.New("request has no messages")
	}
	for _, m := range req.Messages {
		switch m.Role {
		case "system", "user", "assistant":
		default:
			return fmt.Errorf("invalid message role %q", m.Role)
		}
	}
	if len(req.Schema) > 0 && !json.Valid(req.Schema) {
		return errors.New("schema is not valid JSON")
	}
	return nil
}
