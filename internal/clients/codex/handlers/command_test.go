package handlers

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/metadata"
)

func TestCodexCommandHandler_Install(t *testing.T) {
	targetBase := t.TempDir()

	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name:    "deploy",
			Version: "1.0.0",
			Type:    asset.TypeCommand,
		},
		Command: &metadata.CommandConfig{
			PromptFile: "COMMAND.md",
		},
	}

	zipData := createTestZip(t, map[string]string{
		"metadata.toml": `[asset]
name = "deploy"
version = "1.0.0"
type = "command"

[command]
prompt-file = "COMMAND.md"
`,
		"COMMAND.md": "# Deploy Command\n\nDeploy the application.",
	})

	handler := NewCommandHandler(meta)
	if err := handler.Install(context.Background(), zipData, targetBase); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	// Verify command file was created
	commandFile := filepath.Join(targetBase, DirCommands, "deploy.md")
	if _, err := os.Stat(commandFile); os.IsNotExist(err) {
		t.Error("Command file should exist")
	}

	// Verify content
	content, err := os.ReadFile(commandFile)
	if err != nil {
		t.Fatalf("Failed to read command file: %v", err)
	}
	if string(content) != "# Deploy Command\n\nDeploy the application." {
		t.Errorf("Unexpected content: %s", string(content))
	}
}

func TestCodexCommandHandler_Remove(t *testing.T) {
	targetBase := t.TempDir()

	// Create command file manually
	commandsDir := filepath.Join(targetBase, DirCommands)
	if err := os.MkdirAll(commandsDir, 0755); err != nil {
		t.Fatal(err)
	}
	commandFile := filepath.Join(commandsDir, "deploy.md")
	if err := os.WriteFile(commandFile, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name:    "deploy",
			Version: "1.0.0",
			Type:    asset.TypeCommand,
		},
	}

	handler := NewCommandHandler(meta)
	if err := handler.Remove(context.Background(), targetBase); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	// Verify command file was removed
	if _, err := os.Stat(commandFile); !os.IsNotExist(err) {
		t.Error("Command file should be removed")
	}
}

func TestCodexCommandHandler_VerifyInstalled(t *testing.T) {
	targetBase := t.TempDir()

	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name:    "deploy",
			Version: "1.0.0",
			Type:    asset.TypeCommand,
		},
	}

	handler := NewCommandHandler(meta)

	// Not installed
	installed, _ := handler.VerifyInstalled(targetBase)
	if installed {
		t.Error("Should not be installed initially")
	}

	// Create command file
	commandsDir := filepath.Join(targetBase, DirCommands)
	if err := os.MkdirAll(commandsDir, 0755); err != nil {
		t.Fatal(err)
	}
	commandFile := filepath.Join(commandsDir, "deploy.md")
	if err := os.WriteFile(commandFile, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	installed, msg := handler.VerifyInstalled(targetBase)
	if !installed {
		t.Errorf("Should be installed: %s", msg)
	}
}

func TestCodexCommandHandler_RemoveNonExistent(t *testing.T) {
	targetBase := t.TempDir()

	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name:    "non-existent",
			Version: "1.0.0",
			Type:    asset.TypeCommand,
		},
	}

	handler := NewCommandHandler(meta)
	// Should not error on non-existent
	if err := handler.Remove(context.Background(), targetBase); err != nil {
		t.Errorf("Remove should not error for non-existent: %v", err)
	}
}

func TestCodexCommandHandler_MultipleCommands(t *testing.T) {
	targetBase := t.TempDir()

	// Install first command
	meta1 := &metadata.Metadata{
		Asset:   metadata.Asset{Name: "cmd-a", Version: "1.0.0", Type: asset.TypeCommand},
		Command: &metadata.CommandConfig{PromptFile: "COMMAND.md"},
	}
	zip1 := createTestZip(t, map[string]string{
		"metadata.toml": `[asset]
name = "cmd-a"
version = "1.0.0"
type = "command"

[command]
prompt-file = "COMMAND.md"
`,
		"COMMAND.md": "Command A",
	})
	handler1 := NewCommandHandler(meta1)
	if err := handler1.Install(context.Background(), zip1, targetBase); err != nil {
		t.Fatalf("Install cmd-a failed: %v", err)
	}

	// Install second command
	meta2 := &metadata.Metadata{
		Asset:   metadata.Asset{Name: "cmd-b", Version: "1.0.0", Type: asset.TypeCommand},
		Command: &metadata.CommandConfig{PromptFile: "COMMAND.md"},
	}
	zip2 := createTestZip(t, map[string]string{
		"metadata.toml": `[asset]
name = "cmd-b"
version = "1.0.0"
type = "command"

[command]
prompt-file = "COMMAND.md"
`,
		"COMMAND.md": "Command B",
	})
	handler2 := NewCommandHandler(meta2)
	if err := handler2.Install(context.Background(), zip2, targetBase); err != nil {
		t.Fatalf("Install cmd-b failed: %v", err)
	}

	// Both should exist
	commandsDir := filepath.Join(targetBase, DirCommands)
	if _, err := os.Stat(filepath.Join(commandsDir, "cmd-a.md")); os.IsNotExist(err) {
		t.Error("cmd-a.md should exist")
	}
	if _, err := os.Stat(filepath.Join(commandsDir, "cmd-b.md")); os.IsNotExist(err) {
		t.Error("cmd-b.md should exist")
	}

	// Remove first, second should remain
	if err := handler1.Remove(context.Background(), targetBase); err != nil {
		t.Fatalf("Remove cmd-a failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(commandsDir, "cmd-a.md")); !os.IsNotExist(err) {
		t.Error("cmd-a.md should be removed")
	}
	if _, err := os.Stat(filepath.Join(commandsDir, "cmd-b.md")); os.IsNotExist(err) {
		t.Error("cmd-b.md should still exist")
	}
}

func TestCodexCommandHandler_GetPromptFile(t *testing.T) {
	tests := []struct {
		name     string
		meta     *metadata.Metadata
		expected string
	}{
		{
			name: "from command config",
			meta: &metadata.Metadata{
				Command: &metadata.CommandConfig{PromptFile: "COMMAND.md"},
			},
			expected: "COMMAND.md",
		},
		{
			name: "empty command config",
			meta: &metadata.Metadata{
				Command: &metadata.CommandConfig{},
			},
			expected: "",
		},
		{
			name:     "nil command config",
			meta:     &metadata.Metadata{},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewCommandHandler(tt.meta)
			got := handler.getPromptFile()
			if got != tt.expected {
				t.Errorf("getPromptFile() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestCodexCommandHandler_InstallNoPromptFile(t *testing.T) {
	targetBase := t.TempDir()

	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name:    "bad-command",
			Version: "1.0.0",
			Type:    asset.TypeCommand,
		},
		Command: &metadata.CommandConfig{
			PromptFile: "", // Empty prompt file
		},
	}

	zipData := createTestZip(t, map[string]string{
		"metadata.toml": `[asset]
name = "bad-command"
version = "1.0.0"
type = "command"

[command]
prompt-file = ""
`,
	})

	handler := NewCommandHandler(meta)
	err := handler.Install(context.Background(), zipData, targetBase)
	if err == nil {
		t.Error("Should error when no prompt file specified")
	}
}

func TestCodexCommandHandler_InstallMissingPromptFile(t *testing.T) {
	targetBase := t.TempDir()

	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name:    "bad-command",
			Version: "1.0.0",
			Type:    asset.TypeCommand,
		},
		Command: &metadata.CommandConfig{
			PromptFile: "COMMAND.md",
		},
	}

	// Zip without COMMAND.md
	zipData := createTestZip(t, map[string]string{
		"metadata.toml": `[asset]
name = "bad-command"
version = "1.0.0"
type = "command"

[command]
prompt-file = "COMMAND.md"
`,
	})

	handler := NewCommandHandler(meta)
	err := handler.Install(context.Background(), zipData, targetBase)
	if err == nil {
		t.Error("Should error when prompt file missing from zip")
	}
}
