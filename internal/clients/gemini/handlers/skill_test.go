package handlers

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sleuth-io/sx/internal/metadata"
	"github.com/sleuth-io/sx/internal/utils"
)

func TestSkillHandler_Install(t *testing.T) {
	tempDir := t.TempDir()

	// Create test skill content with sx syntax
	skillContent := `Review the code and provide feedback.

Use $ARGUMENTS to specify the file to review.

Also check @./docs/guidelines.md for reference.`

	// Create a zip with the skill content
	zipData, err := utils.CreateZipFromContent("SKILL.md", []byte(skillContent))
	if err != nil {
		t.Fatalf("Failed to create zip: %v", err)
	}

	// Add metadata
	metadataContent := `[asset]
name = "code-review"
version = "1.0.0"
type = "skill"
description = "Review code for best practices"
`
	zipData, err = utils.AddFileToZip(zipData, "metadata.toml", []byte(metadataContent))
	if err != nil {
		t.Fatalf("Failed to add metadata to zip: %v", err)
	}

	handler := NewSkillHandler(&metadata.Metadata{
		Asset: metadata.Asset{
			Name:        "code-review",
			Version:     "1.0.0",
			Description: "Review code for best practices",
		},
	})

	// Install
	ctx := context.Background()
	if err := handler.Install(ctx, zipData, tempDir); err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	// Verify the TOML file was created
	tomlPath := filepath.Join(tempDir, "commands", "code-review.toml")
	content, err := os.ReadFile(tomlPath)
	if err != nil {
		t.Fatalf("Failed to read TOML file: %v", err)
	}

	tomlContent := string(content)

	// Check description is present
	if !strings.Contains(tomlContent, `description = "Review code for best practices"`) {
		t.Error("TOML should contain description")
	}

	// Check prompt is present
	if !strings.Contains(tomlContent, `prompt = """`) {
		t.Error("TOML should contain prompt")
	}

	// Check $ARGUMENTS was converted to {{args}}
	if strings.Contains(tomlContent, "$ARGUMENTS") {
		t.Error("$ARGUMENTS should have been converted to {{args}}")
	}
	if !strings.Contains(tomlContent, "{{args}}") {
		t.Error("TOML should contain {{args}}")
	}

	// Check @./path was converted to @{path}
	if strings.Contains(tomlContent, "@./docs") {
		t.Error("@./path should have been converted to @{path}")
	}
	if !strings.Contains(tomlContent, "@{docs/guidelines.md}") {
		t.Error("TOML should contain @{docs/guidelines.md}")
	}

	// Verify installed
	installed, msg := handler.VerifyInstalled(tempDir)
	if !installed {
		t.Errorf("VerifyInstalled() = false, want true: %s", msg)
	}
}

func TestSkillHandler_Remove(t *testing.T) {
	tempDir := t.TempDir()

	// Create commands directory and a TOML file
	commandsDir := filepath.Join(tempDir, "commands")
	if err := os.MkdirAll(commandsDir, 0755); err != nil {
		t.Fatalf("Failed to create commands dir: %v", err)
	}

	tomlPath := filepath.Join(commandsDir, "test-skill.toml")
	if err := os.WriteFile(tomlPath, []byte("prompt = \"test\""), 0644); err != nil {
		t.Fatalf("Failed to create TOML file: %v", err)
	}

	handler := NewSkillHandler(&metadata.Metadata{
		Asset: metadata.Asset{
			Name: "test-skill",
		},
	})

	// Remove
	ctx := context.Background()
	if err := handler.Remove(ctx, tempDir); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}

	// Verify file is gone
	if _, err := os.Stat(tomlPath); !os.IsNotExist(err) {
		t.Error("TOML file should have been removed")
	}

	// Verify not installed
	installed, _ := handler.VerifyInstalled(tempDir)
	if installed {
		t.Error("VerifyInstalled() = true after removal, want false")
	}
}

func TestConvertPromptSyntax(t *testing.T) {
	handler := &SkillHandler{}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "convert $ARGUMENTS",
			input:    "Review $ARGUMENTS for issues",
			expected: "Review {{args}} for issues",
		},
		{
			name:     "convert @./ path",
			input:    "Check @./docs/style.md for guidelines",
			expected: "Check @{docs/style.md} for guidelines",
		},
		{
			name:     "multiple conversions",
			input:    "Review $ARGUMENTS using @./rules.md",
			expected: "Review {{args}} using @{rules.md}",
		},
		{
			name:     "no conversion needed",
			input:    "Simple prompt without special syntax",
			expected: "Simple prompt without special syntax",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := handler.convertPromptSyntax(tt.input)
			if result != tt.expected {
				t.Errorf("convertPromptSyntax() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestBuildTOMLContent(t *testing.T) {
	handler := &SkillHandler{
		metadata: &metadata.Metadata{
			Asset: metadata.Asset{
				Description: "Test skill description",
			},
		},
	}

	result := handler.buildTOMLContent("Test prompt content")

	if !strings.Contains(result, `description = "Test skill description"`) {
		t.Error("TOML should contain description")
	}

	if !strings.Contains(result, `prompt = """`) {
		t.Error("TOML should contain multi-line prompt start")
	}

	if !strings.Contains(result, "Test prompt content") {
		t.Error("TOML should contain prompt content")
	}

	if !strings.Contains(result, `"""`) {
		t.Error("TOML should contain multi-line prompt end")
	}
}
