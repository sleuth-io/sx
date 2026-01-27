package handlers

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sleuth-io/sx/internal/metadata"
)

func TestRuleHandler_BuildRuleContent_NoGlobs(t *testing.T) {
	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name: "test-rule",
		},
	}

	handler := NewRuleHandler(meta, nil)
	content := handler.buildRuleContent("Follow these rules.")

	// Should not have frontmatter since no globs or description
	if strings.HasPrefix(content, "---\n") {
		t.Errorf("Should not have frontmatter when no globs or description")
	}

	// Should have title as heading
	if !strings.Contains(content, "# test-rule") {
		t.Errorf("Expected title as heading")
	}

	// Should have content
	if !strings.Contains(content, "Follow these rules.") {
		t.Errorf("Expected rule content")
	}
}

func TestRuleHandler_BuildRuleContent_WithGlobs(t *testing.T) {
	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name:        "go-rules",
			Description: "Go coding standards",
		},
		Rule: &metadata.RuleConfig{
			Title: "Go Rules",
			Globs: []string{"**/*.go"},
		},
	}

	handler := NewRuleHandler(meta, nil)
	content := handler.buildRuleContent("Write clean Go code.")

	// Should have frontmatter
	if !strings.HasPrefix(content, "---\n") {
		t.Errorf("Expected content to start with frontmatter")
	}

	// Should have description
	if !strings.Contains(content, "description: Go coding standards") {
		t.Errorf("Expected description in frontmatter")
	}

	// Should have paths (Claude Code uses "paths" not "globs")
	if !strings.Contains(content, "paths:") {
		t.Errorf("Expected paths in frontmatter")
	}
	if !strings.Contains(content, "  - **/*.go") {
		t.Errorf("Expected glob pattern in paths array")
	}

	// Should have title as heading
	if !strings.Contains(content, "# Go Rules") {
		t.Errorf("Expected title as heading")
	}

	// Should have content
	if !strings.Contains(content, "Write clean Go code.") {
		t.Errorf("Expected rule content")
	}
}

func TestRuleHandler_BuildRuleContent_MultipleGlobs(t *testing.T) {
	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name: "multi-glob-rules",
		},
		Rule: &metadata.RuleConfig{
			Globs: []string{"**/*.go", "**/*.mod", "**/*.sum"},
		},
	}

	handler := NewRuleHandler(meta, nil)
	content := handler.buildRuleContent("Content.")

	// Multiple globs should be formatted as YAML array
	if !strings.Contains(content, "paths:") {
		t.Errorf("Expected paths key")
	}
	if !strings.Contains(content, "  - **/*.go") {
		t.Errorf("Expected first glob as array item")
	}
	if !strings.Contains(content, "  - **/*.mod") {
		t.Errorf("Expected second glob as array item")
	}
	if !strings.Contains(content, "  - **/*.sum") {
		t.Errorf("Expected third glob as array item")
	}
}

func TestRuleHandler_BuildRuleContent_DescriptionOnly(t *testing.T) {
	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name:        "desc-only",
			Description: "Just a description",
		},
	}

	handler := NewRuleHandler(meta, nil)
	content := handler.buildRuleContent("Content.")

	// Should have frontmatter with just description
	if !strings.HasPrefix(content, "---\n") {
		t.Errorf("Expected frontmatter when description is present")
	}
	if !strings.Contains(content, "description: Just a description") {
		t.Errorf("Expected description in frontmatter")
	}
	// Should not have paths if no globs
	if strings.Contains(content, "paths:") {
		t.Errorf("Should not have paths when no globs")
	}
}

func TestRuleHandler_BuildRuleContent_ContentAlreadyHasHeading(t *testing.T) {
	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name: "has-heading",
		},
		Rule: &metadata.RuleConfig{
			Title: "Custom Title",
		},
	}

	handler := NewRuleHandler(meta, nil)
	content := handler.buildRuleContent("# Existing Heading\n\nContent here.")

	// Should NOT add a duplicate heading since content already has one
	if strings.Contains(content, "# Custom Title") {
		t.Errorf("Should not add title heading when content already has one")
	}
	if !strings.Contains(content, "# Existing Heading") {
		t.Errorf("Should preserve existing heading")
	}
}

func TestRuleHandler_Install_CreatesFile(t *testing.T) {
	tmpDir := t.TempDir()

	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name:        "test-rule",
			Description: "Test description",
		},
		Rule: &metadata.RuleConfig{
			Title:      "Test Rule",
			PromptFile: "RULE.md",
		},
	}

	handler := NewRuleHandler(meta, nil)

	// Create a test zip with rule content
	zipData := createTestRuleZip(t, "Test rule content.")

	if err := handler.Install(context.TODO(), zipData, tmpDir); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	// Verify file was created in rules/ directory
	filePath := filepath.Join(tmpDir, "rules", "test-rule.md")
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Errorf("Expected rule file to exist at %s", filePath)
	}

	// Verify content
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	if !strings.Contains(string(content), "Test rule content") {
		t.Errorf("File should contain rule content")
	}
	if !strings.Contains(string(content), "# Test Rule") {
		t.Errorf("File should contain title")
	}
}

func TestRuleHandler_Install_WithGlobs(t *testing.T) {
	tmpDir := t.TempDir()

	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name:        "go-rules",
			Description: "Go coding standards",
		},
		Rule: &metadata.RuleConfig{
			Title:      "Go Rules",
			PromptFile: "RULE.md",
			Globs:      []string{"**/*.go"},
		},
	}

	handler := NewRuleHandler(meta, nil)
	zipData := createTestRuleZip(t, "Write clean Go code.")

	if err := handler.Install(context.TODO(), zipData, tmpDir); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	// Verify file was created
	filePath := filepath.Join(tmpDir, "rules", "go-rules.md")
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	// Should have frontmatter with paths
	if !strings.HasPrefix(string(content), "---\n") {
		t.Errorf("Expected frontmatter")
	}
	if !strings.Contains(string(content), "paths:") {
		t.Errorf("Expected paths in frontmatter")
	}
	if !strings.Contains(string(content), "**/*.go") {
		t.Errorf("Expected glob pattern")
	}
}

func TestRuleHandler_Remove(t *testing.T) {
	tmpDir := t.TempDir()

	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "remove-test"},
	}

	handler := NewRuleHandler(meta, nil)

	// Create the file first
	rulesDir := filepath.Join(tmpDir, "rules")
	if err := os.MkdirAll(rulesDir, 0755); err != nil {
		t.Fatalf("Failed to create rules dir: %v", err)
	}
	filePath := filepath.Join(rulesDir, "remove-test.md")
	if err := os.WriteFile(filePath, []byte("content"), 0644); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

	// Remove it
	if err := handler.Remove(context.TODO(), tmpDir); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	// Verify file was removed
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Errorf("File should have been removed")
	}
}

func TestRuleHandler_Remove_NonExistent(t *testing.T) {
	tmpDir := t.TempDir()

	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "nonexistent"},
	}

	handler := NewRuleHandler(meta, nil)

	// Should not error when file doesn't exist
	if err := handler.Remove(context.TODO(), tmpDir); err != nil {
		t.Errorf("Remove should not error for nonexistent file: %v", err)
	}
}

func TestRuleHandler_VerifyInstalled(t *testing.T) {
	tmpDir := t.TempDir()

	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "verify-test"},
	}

	handler := NewRuleHandler(meta, nil)

	// Should not be installed initially
	installed, _ := handler.VerifyInstalled(tmpDir)
	if installed {
		t.Error("Should not be installed initially")
	}

	// Create the file
	rulesDir := filepath.Join(tmpDir, "rules")
	if err := os.MkdirAll(rulesDir, 0755); err != nil {
		t.Fatalf("Failed to create rules dir: %v", err)
	}
	filePath := filepath.Join(rulesDir, "verify-test.md")
	if err := os.WriteFile(filePath, []byte("content"), 0644); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

	// Should be installed now
	installed, msg := handler.VerifyInstalled(tmpDir)
	if !installed {
		t.Errorf("Should be installed after creating file, msg: %s", msg)
	}
}

func TestRuleHandler_GetTitle(t *testing.T) {
	tests := []struct {
		name     string
		meta     *metadata.Metadata
		expected string
	}{
		{
			name: "uses rule title when set",
			meta: &metadata.Metadata{
				Asset: metadata.Asset{Name: "asset-name"},
				Rule: &metadata.RuleConfig{
					Title: "Custom Title",
				},
			},
			expected: "Custom Title",
		},
		{
			name: "falls back to asset name",
			meta: &metadata.Metadata{
				Asset: metadata.Asset{Name: "asset-name"},
			},
			expected: "asset-name",
		},
		{
			name: "falls back when title is empty",
			meta: &metadata.Metadata{
				Asset: metadata.Asset{Name: "asset-name"},
				Rule: &metadata.RuleConfig{
					Title: "",
				},
			},
			expected: "asset-name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewRuleHandler(tt.meta, nil)
			got := handler.getTitle()
			if got != tt.expected {
				t.Errorf("getTitle() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestRuleHandler_GetInstallPath(t *testing.T) {
	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "test"},
	}

	handler := NewRuleHandler(meta, nil)

	if handler.GetInstallPath() != ".claude/rules/" {
		t.Errorf("GetInstallPath() = %q, want %q", handler.GetInstallPath(), ".claude/rules/")
	}
}

// createTestRuleZip creates a minimal zip file with rule content
func createTestRuleZip(t *testing.T, content string) []byte {
	t.Helper()

	metadataContent := `[asset]
name = "test"
type = "rule"
version = "1.0.0"

[rule]
prompt-file = "RULE.md"
`

	return createZipFromFiles(t, map[string]string{
		"metadata.toml": metadataContent,
		"RULE.md":       content,
	})
}

// createZipFromFiles creates a zip file in memory from a map of filename -> content
func createZipFromFiles(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var buf bytes.Buffer
	w := zip.NewWriter(&buf)

	for name, content := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatalf("Failed to create zip entry: %v", err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			t.Fatalf("Failed to write zip content: %v", err)
		}
	}

	if err := w.Close(); err != nil {
		t.Fatalf("Failed to close zip: %v", err)
	}

	return buf.Bytes()
}
