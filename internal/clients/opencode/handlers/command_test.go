package handlers

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/metadata"
)

func TestOpenCodeCommandHandler_InstallRemoveVerify(t *testing.T) {
	targetBase := t.TempDir()

	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name:    "my-command",
			Version: "1.0.0",
			Type:    asset.TypeCommand,
		},
		Command: &metadata.CommandConfig{PromptFile: "COMMAND.md"},
	}

	zipData := createTestZip(t, map[string]string{
		"metadata.toml": `[asset]
name = "my-command"
version = "1.0.0"
type = "command"

[command]
prompt-file = "COMMAND.md"
`,
		"COMMAND.md": "---\ndescription: Test\n---\n\nDo stuff with $ARGUMENTS",
	})

	h := NewCommandHandler(meta)
	if err := h.Install(context.Background(), zipData, targetBase); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	commandFile := filepath.Join(targetBase, DirCommands, "my-command.md")
	data, err := os.ReadFile(commandFile)
	if err != nil {
		t.Fatalf("Command file should exist: %v", err)
	}
	if len(data) == 0 {
		t.Error("Command file should not be empty")
	}

	installed, msg := h.VerifyInstalled(targetBase)
	if !installed {
		t.Errorf("Should report installed: %s", msg)
	}

	if err := h.Remove(context.Background(), targetBase); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}
	if _, err := os.Stat(commandFile); !os.IsNotExist(err) {
		t.Error("Command file should be removed")
	}

	// Remove again should be a no-op
	if err := h.Remove(context.Background(), targetBase); err != nil {
		t.Errorf("Second Remove should be a no-op, got: %v", err)
	}
}
