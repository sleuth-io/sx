//go:build llmintegration

package llm

// Real-CLI integration tests — the substantiation for the restriction
// flags in cli.go (`claude --tools ""`, `codex --sandbox read-only`,
// `gemini --approval-mode default`). The unit tests' fake binaries can
// only prove the flags are SENT; these prove the real, installed CLIs
// ACCEPT them and still complete (an unknown flag exits non-zero and
// fails the test).
//
// They call the user's own authenticated CLIs and spend real tokens,
// so they are behind a build tag and skip per-CLI when the binary is
// absent. Run explicitly:
//
//	go test -tags llmintegration ./internal/llm/ -run TestRealCLI -v
import (
	"context"
	"strings"
	"testing"
)

func realCLITurn(t *testing.T, providerID string) {
	t.Helper()
	path, ok := findCLI(providerID)
	if !ok {
		t.Skipf("%s not installed on this machine", cliBinaryName(providerID))
	}
	p := &cliProvider{id: providerID, binPath: path}
	resp, err := p.Complete(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "Reply with exactly: OK"}},
	})
	if err != nil {
		// This is the failure a renamed/unknown restriction flag causes:
		// the CLI exits non-zero and the error carries its stderr.
		t.Fatalf("%s rejected the hardened invocation: %v", providerID, err)
	}
	if !strings.Contains(resp.Text, "OK") {
		t.Fatalf("%s reply = %q", providerID, resp.Text)
	}
}

func TestRealCLIClaude(t *testing.T) { realCLITurn(t, ProviderClaudeCLI) }
func TestRealCLICodex(t *testing.T)  { realCLITurn(t, ProviderCodexCLI) }
func TestRealCLIGemini(t *testing.T) { realCLITurn(t, ProviderGeminiCLI) }
