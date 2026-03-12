package handlers

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/metadata"
)

func TestCommandHandler_Install_ProjectScope(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a fake .cline directory (project scope)
	targetBase := filepath.Join(tmpDir, ".cline")
	if err := os.MkdirAll(targetBase, 0755); err != nil {
		t.Fatalf("Failed to create target dir: %v", err)
	}

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

	zipData := createZipFromFiles(t, map[string]string{
		"metadata.toml": `[asset]
name = "deploy"
version = "1.0.0"
type = "command"

[command]
prompt-file = "COMMAND.md"
`,
		"COMMAND.md": "# Deploy Workflow\n\nDeploy the application to production.",
	})

	handler := NewCommandHandler(meta)
	if err := handler.Install(context.Background(), zipData, targetBase); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	// Verify workflow was created in .clinerules/workflows/
	workflowFile := filepath.Join(tmpDir, ".clinerules", "workflows", "deploy.md")
	content, err := os.ReadFile(workflowFile)
	if err != nil {
		t.Fatalf("Failed to read workflow file: %v", err)
	}

	if !strings.Contains(string(content), "Deploy Workflow") {
		t.Errorf("Workflow content should contain 'Deploy Workflow', got: %s", string(content))
	}
}

func TestCommandHandler_Install_GlobalScope(t *testing.T) {
	// Create temp home directory
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	// Global scope: targetBase is ~/.cline
	targetBase := filepath.Join(tmpHome, ".cline")
	if err := os.MkdirAll(targetBase, 0755); err != nil {
		t.Fatalf("Failed to create target dir: %v", err)
	}

	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name:    "review",
			Version: "1.0.0",
			Type:    asset.TypeCommand,
		},
		Command: &metadata.CommandConfig{
			PromptFile: "COMMAND.md",
		},
	}

	zipData := createZipFromFiles(t, map[string]string{
		"metadata.toml": `[asset]
name = "review"
version = "1.0.0"
type = "command"

[command]
prompt-file = "COMMAND.md"
`,
		"COMMAND.md": "# Code Review Workflow",
	})

	handler := NewCommandHandler(meta)
	if err := handler.Install(context.Background(), zipData, targetBase); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	// Verify workflow was created in ~/Documents/Cline/Workflows/
	workflowFile := filepath.Join(tmpHome, "Documents", "Cline", "Workflows", "review.md")
	if _, err := os.Stat(workflowFile); os.IsNotExist(err) {
		t.Errorf("Workflow file should exist at %s", workflowFile)
	}
}

func TestCommandHandler_Install_WithSkillMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	targetBase := filepath.Join(tmpDir, ".cline")
	if err := os.MkdirAll(targetBase, 0755); err != nil {
		t.Fatalf("Failed to create target dir: %v", err)
	}

	// Command with skill metadata (for skill → command transformation)
	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name:    "my-skill",
			Version: "1.0.0",
			Type:    asset.TypeCommand,
		},
		Skill: &metadata.SkillConfig{
			PromptFile: "SKILL.md",
		},
	}

	zipData := createZipFromFiles(t, map[string]string{
		"metadata.toml": `[asset]
name = "my-skill"
version = "1.0.0"
type = "command"

[skill]
prompt-file = "SKILL.md"
`,
		"SKILL.md": "# Skill as workflow",
	})

	handler := NewCommandHandler(meta)
	if err := handler.Install(context.Background(), zipData, targetBase); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	// Verify workflow was created
	workflowFile := filepath.Join(tmpDir, ".clinerules", "workflows", "my-skill.md")
	if _, err := os.Stat(workflowFile); os.IsNotExist(err) {
		t.Error("Workflow file should exist")
	}
}

func TestCommandHandler_Install_NoPromptFile(t *testing.T) {
	tmpDir := t.TempDir()
	targetBase := filepath.Join(tmpDir, ".cline")
	if err := os.MkdirAll(targetBase, 0755); err != nil {
		t.Fatalf("Failed to create target dir: %v", err)
	}

	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name:    "bad-cmd",
			Version: "1.0.0",
			Type:    asset.TypeCommand,
		},
		// No Command or Skill config
	}

	zipData := createZipFromFiles(t, map[string]string{
		"metadata.toml": `[asset]
name = "bad-cmd"
version = "1.0.0"
type = "command"
`,
	})

	handler := NewCommandHandler(meta)
	err := handler.Install(context.Background(), zipData, targetBase)
	if err == nil {
		t.Error("Expected error for missing prompt file")
	}
	if !strings.Contains(err.Error(), "no prompt file") {
		t.Errorf("Expected 'no prompt file' error, got: %v", err)
	}
}

func TestCommandHandler_Remove(t *testing.T) {
	tmpDir := t.TempDir()
	targetBase := filepath.Join(tmpDir, ".cline")
	if err := os.MkdirAll(targetBase, 0755); err != nil {
		t.Fatalf("Failed to create target dir: %v", err)
	}

	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name:    "remove-test",
			Version: "1.0.0",
			Type:    asset.TypeCommand,
		},
	}

	// Create the workflow file first
	workflowsDir := filepath.Join(tmpDir, ".clinerules", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows dir: %v", err)
	}
	workflowFile := filepath.Join(workflowsDir, "remove-test.md")
	if err := os.WriteFile(workflowFile, []byte("content"), 0644); err != nil {
		t.Fatalf("Failed to write workflow: %v", err)
	}

	handler := NewCommandHandler(meta)
	if err := handler.Remove(context.Background(), targetBase); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	// Verify workflow was removed
	if _, err := os.Stat(workflowFile); !os.IsNotExist(err) {
		t.Error("Workflow file should have been removed")
	}
}

func TestCommandHandler_Remove_NotExists(t *testing.T) {
	tmpDir := t.TempDir()
	targetBase := filepath.Join(tmpDir, ".cline")
	if err := os.MkdirAll(targetBase, 0755); err != nil {
		t.Fatalf("Failed to create target dir: %v", err)
	}

	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "nonexistent"},
	}

	handler := NewCommandHandler(meta)
	// Should not error when file doesn't exist
	if err := handler.Remove(context.Background(), targetBase); err != nil {
		t.Errorf("Remove should not error for nonexistent file: %v", err)
	}
}

func TestCommandHandler_VerifyInstalled(t *testing.T) {
	tmpDir := t.TempDir()
	targetBase := filepath.Join(tmpDir, ".cline")
	if err := os.MkdirAll(targetBase, 0755); err != nil {
		t.Fatalf("Failed to create target dir: %v", err)
	}

	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "verify-test"},
	}

	handler := NewCommandHandler(meta)

	// Should not be installed initially
	installed, _ := handler.VerifyInstalled(targetBase)
	if installed {
		t.Error("Should not be installed initially")
	}

	// Create the workflow file
	workflowsDir := filepath.Join(tmpDir, ".clinerules", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows dir: %v", err)
	}
	workflowFile := filepath.Join(workflowsDir, "verify-test.md")
	if err := os.WriteFile(workflowFile, []byte("content"), 0644); err != nil {
		t.Fatalf("Failed to write workflow: %v", err)
	}

	// Should be installed now
	installed, msg := handler.VerifyInstalled(targetBase)
	if !installed {
		t.Errorf("Should be installed, got: %s", msg)
	}
}

func TestCommandHandler_DetermineWorkflowsDir(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "test"},
	}
	handler := NewCommandHandler(meta)

	tests := []struct {
		name       string
		targetBase string
		expected   string
	}{
		{
			name:       "global scope",
			targetBase: filepath.Join(tmpHome, ".cline"),
			expected:   filepath.Join(tmpHome, "Documents", "Cline", "Workflows"),
		},
		{
			name:       "project scope",
			targetBase: "/path/to/repo/.cline",
			expected:   "/path/to/repo/.clinerules/workflows",
		},
		{
			name:       "path scope",
			targetBase: "/path/to/repo/services/api/.cline",
			expected:   "/path/to/repo/services/api/.clinerules/workflows",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := handler.determineWorkflowsDir(tt.targetBase)
			if err != nil {
				t.Fatalf("determineWorkflowsDir failed: %v", err)
			}
			if got != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, got)
			}
		})
	}
}

func TestCommandHandler_GetPromptFile(t *testing.T) {
	tests := []struct {
		name     string
		meta     *metadata.Metadata
		expected string
	}{
		{
			name: "uses command prompt file",
			meta: &metadata.Metadata{
				Command: &metadata.CommandConfig{PromptFile: "CMD.md"},
			},
			expected: "CMD.md",
		},
		{
			name: "uses skill prompt file as fallback",
			meta: &metadata.Metadata{
				Skill: &metadata.SkillConfig{PromptFile: "SKILL.md"},
			},
			expected: "SKILL.md",
		},
		{
			name: "prefers command over skill",
			meta: &metadata.Metadata{
				Command: &metadata.CommandConfig{PromptFile: "CMD.md"},
				Skill:   &metadata.SkillConfig{PromptFile: "SKILL.md"},
			},
			expected: "CMD.md",
		},
		{
			name:     "empty when no config",
			meta:     &metadata.Metadata{},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewCommandHandler(tt.meta)
			got := handler.getPromptFile()
			if got != tt.expected {
				t.Errorf("getPromptFile() = %v, want %v", got, tt.expected)
			}
		})
	}
}
