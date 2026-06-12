package handlers

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/metadata"
)

func TestCodexAgentHandler_InstallRemoveVerify(t *testing.T) {
	targetBase := t.TempDir()

	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name:    "security_reviewer",
			Version: "1.0.0",
			Type:    asset.TypeAgent,
		},
		Agent: &metadata.AgentConfig{
			PromptFile: "security_reviewer.toml",
		},
	}

	agentTOML := `name = "security_reviewer"
description = "Security reviewer"
developer_instructions = "Review security risks."
`
	zipData := createTestZip(t, map[string]string{
		"metadata.toml": `[asset]
name = "security_reviewer"
version = "1.0.0"
type = "agent"

[agent]
prompt-file = "security_reviewer.toml"
`,
		"security_reviewer.toml": agentTOML,
	})

	handler := NewAgentHandler(meta)
	installed, msg := handler.VerifyInstalled(targetBase)
	if installed {
		t.Fatalf("VerifyInstalled before install = true, want false: %s", msg)
	}

	if err := handler.Install(context.Background(), zipData, targetBase); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	agentPath := filepath.Join(targetBase, DirAgents, "security_reviewer.toml")
	got, err := os.ReadFile(agentPath)
	if err != nil {
		t.Fatalf("Failed to read installed agent file: %v", err)
	}
	if string(got) != agentTOML {
		t.Errorf("Installed agent TOML = %q, want %q", string(got), agentTOML)
	}

	installed, msg = handler.VerifyInstalled(targetBase)
	if !installed {
		t.Fatalf("VerifyInstalled after install = false, want true: %s", msg)
	}

	if err := handler.Remove(context.Background(), targetBase); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	if _, err := os.Stat(agentPath); !os.IsNotExist(err) {
		t.Fatalf("Agent file still exists after remove")
	}
}
