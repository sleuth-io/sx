package llm

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// Provider detection. The app is a GUI process, so on macOS it does NOT
// inherit the user's shell PATH (~/.zprofile etc.) — exec.LookPath alone
// misses Homebrew and npm-global installs. We check PATH first, then a
// short list of well-known install dirs. "Found" means installed, not
// authenticated: auth is verified lazily on first call, where the CLI's
// own error message is surfaced.

// guiExtraDirs lists install locations to probe beyond PATH. A var so
// tests can stub it.
var guiExtraDirs = func() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	dirs := []string{
		"/opt/homebrew/bin",
		"/usr/local/bin",
	}
	if home != "" {
		dirs = append(dirs,
			filepath.Join(home, ".local", "bin"),
			filepath.Join(home, ".npm-global", "bin"),
			filepath.Join(home, "bin"),
			filepath.Join(home, ".bun", "bin"),
		)
	}
	return dirs
}

// cliBinaryName maps a CLI provider id to its executable name.
func cliBinaryName(providerID string) string {
	switch providerID {
	case ProviderClaudeCLI:
		return "claude"
	case ProviderCodexCLI:
		return "codex"
	case ProviderGeminiCLI:
		return "gemini"
	}
	return ""
}

// findCLI locates a CLI provider's binary, PATH first then the GUI
// extra dirs.
func findCLI(providerID string) (string, bool) {
	bin := cliBinaryName(providerID)
	if bin == "" {
		return "", false
	}
	if p, err := exec.LookPath(bin); err == nil {
		return p, true
	}
	for _, dir := range guiExtraDirs() {
		p := filepath.Join(dir, bin)
		info, err := os.Stat(p)
		if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
			continue
		}
		return p, true
	}
	return "", false
}

// defaultOllamaBaseURL is where a stock Ollama install listens.
const defaultOllamaBaseURL = "http://127.0.0.1:11434"

func ollamaBaseURL(configured string) string {
	if configured != "" {
		return configured
	}
	return defaultOllamaBaseURL
}

// ollamaModels returns the locally available model names, or nil when
// no Ollama server answers at baseURL.
func ollamaModels(ctx context.Context, baseURL string) []string {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ollamaBaseURL(baseURL)+"/api/tags", nil)
	if err != nil {
		return nil
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var body struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := decodeJSONBody(resp, &body); err != nil {
		return nil
	}
	names := make([]string, 0, len(body.Models))
	for _, m := range body.Models {
		names = append(names, m.Name)
	}
	return names
}

// ProviderInfo describes one provider option for the settings picker.
type ProviderInfo struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"` // "cli" | "local" | "api"
	Label  string `json:"label"`
	Detail string `json:"detail,omitempty"` // binary path, server URL, …
	// Available: the provider can be selected right now. CLI providers
	// need the binary installed; Ollama needs a responding server; API
	// providers are always selectable (the key is entered in settings).
	Available bool `json:"available"`
	// Models: for Ollama, the locally pulled models to choose from.
	Models []string `json:"models,omitempty"`
	// NeedsAPIKey / NeedsModel drive which settings inputs to show.
	NeedsAPIKey bool `json:"needsApiKey"`
	NeedsModel  bool `json:"needsModel"`
}

// DetectProviders enumerates every provider option and its current
// availability. baseURL is the user's configured Ollama address ("" for
// the default).
func DetectProviders(ctx context.Context, baseURL string) []ProviderInfo {
	out := make([]ProviderInfo, 0, 7)
	for _, cli := range []struct{ id, label string }{
		{ProviderClaudeCLI, "Claude Code CLI"},
		{ProviderCodexCLI, "Codex CLI"},
		{ProviderGeminiCLI, "Gemini CLI"},
	} {
		info := ProviderInfo{ID: cli.id, Kind: "cli", Label: cli.label}
		if path, ok := findCLI(cli.id); ok {
			info.Available = true
			info.Detail = path
		}
		out = append(out, info)
	}
	ollama := ProviderInfo{
		ID: ProviderOllama, Kind: "local", Label: "Ollama (local models)",
		Detail: ollamaBaseURL(baseURL), NeedsModel: true,
	}
	if models := ollamaModels(ctx, baseURL); len(models) > 0 {
		ollama.Available = true
		ollama.Models = models
	}
	out = append(out, ollama)
	out = append(out,
		ProviderInfo{ID: ProviderAnthropic, Kind: "api", Label: "Anthropic API", Available: true, NeedsAPIKey: true},
		ProviderInfo{ID: ProviderOpenAI, Kind: "api", Label: "OpenAI-compatible API", Available: true, NeedsAPIKey: true, NeedsModel: true},
		ProviderInfo{ID: ProviderGoogle, Kind: "api", Label: "Google Gemini API", Available: true, NeedsAPIKey: true, NeedsModel: true},
	)
	return out
}
