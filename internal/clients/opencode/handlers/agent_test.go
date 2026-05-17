package handlers

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/metadata"
)

func TestOpenCodeAgentHandler_InstallRemoveVerify(t *testing.T) {
	targetBase := t.TempDir()

	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name:    "deployer",
			Version: "1.0.0",
			Type:    asset.TypeAgent,
		},
		Agent: &metadata.AgentConfig{PromptFile: "AGENT.md"},
	}

	zipData := createTestZip(t, map[string]string{
		"metadata.toml": `[asset]
name = "deployer"
version = "1.0.0"
type = "agent"

[agent]
prompt-file = "AGENT.md"
`,
		"AGENT.md": "---\ndescription: Deploys things\nmode: subagent\n---\n\nYou deploy things.",
	})

	h := NewAgentHandler(meta)
	if err := h.Install(context.Background(), zipData, targetBase); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	agentFile := filepath.Join(targetBase, DirAgents, "deployer.md")
	data, err := os.ReadFile(agentFile)
	if err != nil {
		t.Fatalf("Agent file should exist: %v", err)
	}
	if len(data) == 0 {
		t.Error("Agent file should not be empty")
	}

	installed, msg := h.VerifyInstalled(targetBase)
	if !installed {
		t.Errorf("Should report installed: %s", msg)
	}

	if err := h.Remove(context.Background(), targetBase); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}
	if _, err := os.Stat(agentFile); !os.IsNotExist(err) {
		t.Error("Agent file should be removed")
	}

	// Second remove should be a no-op
	if err := h.Remove(context.Background(), targetBase); err != nil {
		t.Errorf("Second Remove should be a no-op, got: %v", err)
	}
}

func TestOpenCodeAgentHandler_DefaultsPromptFile(t *testing.T) {
	targetBase := t.TempDir()

	// No Agent config — the handler should fall back to AGENT.md.
	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name:    "minimal",
			Version: "1.0.0",
			Type:    asset.TypeAgent,
		},
	}

	zipData := createTestZip(t, map[string]string{
		"AGENT.md": "---\ndescription: minimal\n---\nminimal body",
	})

	h := NewAgentHandler(meta)
	if err := h.Install(context.Background(), zipData, targetBase); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(targetBase, DirAgents, "minimal.md")); err != nil {
		t.Errorf("Agent file should exist with default prompt-file: %v", err)
	}
}
