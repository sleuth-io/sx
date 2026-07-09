package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sleuth-io/sx/internal/llm"
	"github.com/sleuth-io/sx/internal/logger"
	"github.com/sleuth-io/sx/internal/utils"
)

// sx.llm core service (API 1.9.0, docs/app-plugins-spec.md). Sandboxed
// extensions can't shell out or reach localhost, so LLM access has to
// live in core: the user configures ONE provider here — an installed
// CLI, a local Ollama server, or any hosted API with their own key —
// and every extension holding the llm:use permission goes through the
// same LLMComplete bridge. The provider choice is machine-level (one
// llm.json for all profiles) because it's the user's tooling, not
// library state; API keys live in the OS keyring, never on disk and
// never in the webview.

// llmConfigFile holds the provider selection under the sx config dir.
const llmConfigFile = "llm.json"

// llmKeyringAccount namespaces provider API keys in the same keyring
// service as extension secrets ("sx-app-plugins") so they show up
// together — and are revocable together — in the OS keychain UI.
// Unlike extension secrets, there is no file fallback: an LLM key is
// entered once in settings, and refusing to write it to disk beats
// working headless.
func llmKeyringAccount(provider string) string {
	return "llm-provider/" + provider
}

var llmProviderIDs = map[string]bool{
	llm.ProviderClaudeCLI: true, llm.ProviderCodexCLI: true, llm.ProviderGeminiCLI: true,
	llm.ProviderOllama: true, llm.ProviderAnthropic: true,
	llm.ProviderOpenAI: true, llm.ProviderGoogle: true,
}

// llmCtx guards against a nil app context (unit tests, early startup).
func (a *App) llmCtx() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

func llmConfigPath() (string, error) {
	dir, err := utils.GetConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, llmConfigFile), nil
}

func loadLLMConfig() (llm.Config, error) {
	path, err := llmConfigPath()
	if err != nil {
		return llm.Config{}, err
	}
	data, err := os.ReadFile(path) // #nosec G304 -- fixed name under the sx config dir
	if os.IsNotExist(err) {
		return llm.Config{}, nil
	}
	if err != nil {
		return llm.Config{}, err
	}
	var cfg llm.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return llm.Config{}, fmt.Errorf("corrupt %s: %w", llmConfigFile, err)
	}
	return cfg, nil
}

// LLMStatusView is everything the settings panel needs in one call.
type LLMStatusView struct {
	Config    llm.Config         `json:"config"`
	Providers []llm.ProviderInfo `json:"providers"`
	// KeySet reports, per API provider, whether a key is stored — the
	// key itself never crosses the bridge.
	KeySet map[string]bool `json:"keySet"`
}

// LLMStatus reports the configured provider, every detectable provider
// option, and which providers already have a stored API key.
func (a *App) LLMStatus() (LLMStatusView, error) {
	cfg, err := loadLLMConfig()
	if err != nil {
		return LLMStatusView{}, err
	}
	keySet := map[string]bool{}
	for _, id := range []string{llm.ProviderAnthropic, llm.ProviderOpenAI, llm.ProviderGoogle} {
		_, ok, kerr := activePluginSecretStore.Get(llmKeyringAccount(id))
		if kerr != nil {
			logger.Get().Warn("llm: keyring read failed", "provider", id, "error", kerr)
		}
		keySet[id] = ok
	}
	return LLMStatusView{
		Config:    cfg,
		Providers: llm.DetectProviders(a.llmCtx(), cfg.BaseURL),
		KeySet:    keySet,
	}, nil
}

// LLMSetConfig persists the provider selection. An empty provider
// clears the configuration.
func (a *App) LLMSetConfig(cfg llm.Config) error {
	if cfg.Provider != "" && !llmProviderIDs[cfg.Provider] {
		return fmt.Errorf("unknown AI provider %q", cfg.Provider)
	}
	path, err := llmConfigPath()
	if err != nil {
		return err
	}
	if cfg == (llm.Config{}) {
		if rmErr := os.Remove(path); rmErr != nil && !os.IsNotExist(rmErr) {
			return rmErr
		}
		return nil
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(path, data)
}

// LLMSetAPIKey stores (or with "" deletes) a provider's API key in the
// OS keyring. There is deliberately no LLMGetAPIKey: keys go in, never
// back out to the webview.
func (a *App) LLMSetAPIKey(provider, key string) error {
	if !llmProviderIDs[provider] {
		return fmt.Errorf("unknown AI provider %q", provider)
	}
	if len(key) > maxPluginSecretBytes {
		return fmt.Errorf("API key exceeds %d bytes", maxPluginSecretBytes)
	}
	account := llmKeyringAccount(provider)
	if key == "" {
		return activePluginSecretStore.Delete(account)
	}
	if err := activePluginSecretStore.Set(account, key); err != nil {
		return fmt.Errorf("could not store the key in the OS keychain: %w", err)
	}
	return nil
}

// llmProvider builds the configured provider, resolving the API key
// from the keyring at call time.
func (a *App) llmProvider() (llm.Provider, error) {
	cfg, err := loadLLMConfig()
	if err != nil {
		return nil, err
	}
	var apiKey string
	if cfg.Provider == llm.ProviderAnthropic || cfg.Provider == llm.ProviderOpenAI || cfg.Provider == llm.ProviderGoogle {
		v, ok, kerr := activePluginSecretStore.Get(llmKeyringAccount(cfg.Provider))
		if kerr != nil {
			return nil, fmt.Errorf("could not read the API key from the OS keychain: %w", kerr)
		}
		if ok {
			apiKey = v
		}
	}
	return llm.New(cfg, apiKey)
}

// LLMComplete runs one completion for an extension. Permission
// (llm:use) is enforced by the loader like every other bridge call;
// the id is taken for attribution in logs.
func (a *App) LLMComplete(id string, requestJSON string) (string, error) {
	if err := validatePluginID(id); err != nil {
		return "", err
	}
	var req llm.Request
	if err := json.Unmarshal([]byte(requestJSON), &req); err != nil {
		return "", fmt.Errorf("invalid request: %w", err)
	}
	provider, err := a.llmProvider()
	if err != nil {
		return "", err
	}
	resp, err := provider.Complete(a.llmCtx(), req)
	if err != nil {
		logger.Get().Warn("llm: completion failed", "extension", id, "provider", provider.ID(), "error", err)
		return "", err
	}
	logger.Get().Info("llm: completion",
		"extension", id, "provider", resp.Provider, "model", resp.Model,
		"inputTokens", resp.Usage.InputTokens, "outputTokens", resp.Usage.OutputTokens)
	out, err := json.Marshal(resp)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// LLMTest sends a trivial prompt through the configured provider so
// the settings panel can verify the setup end to end.
func (a *App) LLMTest() (string, error) {
	provider, err := a.llmProvider()
	if err != nil {
		return "", err
	}
	resp, err := provider.Complete(a.llmCtx(), llm.Request{
		Messages:  []llm.Message{{Role: "user", Content: "Reply with exactly: OK"}},
		MaxTokens: 16,
	})
	if err != nil {
		return "", err
	}
	return resp.Text, nil
}
