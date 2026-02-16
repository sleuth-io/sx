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

func TestRuleHandler_Install_AlwaysApply(t *testing.T) {
	tmpDir := t.TempDir()

	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name:        "coding-standards",
			Description: "Follow these coding standards",
		},
		Rule: &metadata.RuleConfig{
			Title:      "Coding Standards",
			PromptFile: "RULE.md",
		},
	}

	handler := NewRuleHandler(meta, "")
	zipData := createTestRuleZip(t, "Always follow these rules.")

	if err := handler.Install(context.TODO(), zipData, tmpDir); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	content := readInstalledRule(t, tmpDir, "coding-standards")

	// Should have alwaysApply since no path scope and no explicit globs
	if !strings.Contains(content, "alwaysApply: true") {
		t.Errorf("Expected alwaysApply: true for repo-wide rule without globs, got: %s", content)
	}

	// Should have description
	if !strings.Contains(content, "description: Follow these coding standards") {
		t.Errorf("Expected description from asset metadata")
	}

	// Should have title as heading
	if !strings.Contains(content, "# Coding Standards") {
		t.Errorf("Expected title as heading")
	}

	// Should have content
	if !strings.Contains(content, "Always follow these rules.") {
		t.Errorf("Expected rule content")
	}

	// Verify frontmatter structure
	if !strings.HasPrefix(content, "---\n") {
		t.Errorf("Expected content to start with frontmatter")
	}
}

func TestRuleHandler_Install_WithPathScope(t *testing.T) {
	tmpDir := t.TempDir()

	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name:        "backend-rules",
			Description: "Backend specific rules",
		},
		Rule: &metadata.RuleConfig{
			PromptFile: "RULE.md",
		},
	}

	handler := NewRuleHandler(meta, "backend/")
	zipData := createTestRuleZip(t, "Backend content.")

	if err := handler.Install(context.TODO(), zipData, tmpDir); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	content := readInstalledRule(t, tmpDir, "backend-rules")

	// Should NOT have alwaysApply since it has path scope
	if strings.Contains(content, "alwaysApply") {
		t.Errorf("Should not have alwaysApply when path scoped")
	}

	// Should have auto-generated glob from path
	if !strings.Contains(content, "globs: backend/**/*") {
		t.Errorf("Expected auto-generated glob from path scope, got: %s", content)
	}
}

func TestRuleHandler_Install_ExplicitGlobs(t *testing.T) {
	tmpDir := t.TempDir()

	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name:        "test-rules",
			Description: "Test file rules",
		},
		Rule: &metadata.RuleConfig{
			PromptFile: "RULE.md",
			Globs:      []string{"**/*_test.go"},
		},
	}

	handler := NewRuleHandler(meta, "")
	zipData := createTestRuleZip(t, "Test content.")

	if err := handler.Install(context.TODO(), zipData, tmpDir); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	content := readInstalledRule(t, tmpDir, "test-rules")

	// Should have explicit glob
	if !strings.Contains(content, "globs: **/*_test.go") {
		t.Errorf("Expected explicit glob, got: %s", content)
	}

	// Should NOT have alwaysApply when explicit globs provided
	if strings.Contains(content, "alwaysApply") {
		t.Errorf("Should not have alwaysApply with explicit globs")
	}
}

func TestRuleHandler_Install_MultipleGlobs(t *testing.T) {
	tmpDir := t.TempDir()

	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name: "multi-glob-rules",
		},
		Rule: &metadata.RuleConfig{
			PromptFile: "RULE.md",
			Globs:      []string{"**/*.go", "**/*.mod", "**/*.sum"},
		},
	}

	handler := NewRuleHandler(meta, "")
	zipData := createTestRuleZip(t, "Content.")

	if err := handler.Install(context.TODO(), zipData, tmpDir); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	content := readInstalledRule(t, tmpDir, "multi-glob-rules")

	// Multiple globs should be formatted as YAML array
	if !strings.Contains(content, "globs:") {
		t.Errorf("Expected globs key")
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

func TestRuleHandler_Install_RuleDescription(t *testing.T) {
	tmpDir := t.TempDir()

	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name:        "custom-desc",
			Description: "Generic asset description",
		},
		Rule: &metadata.RuleConfig{
			PromptFile:  "RULE.md",
			Description: "Rule-specific description",
			Cursor: map[string]any{
				"always-apply": true,
			},
		},
	}

	handler := NewRuleHandler(meta, "")
	zipData := createTestRuleZip(t, "Content.")

	if err := handler.Install(context.TODO(), zipData, tmpDir); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	content := readInstalledRule(t, tmpDir, "custom-desc")

	// Should use rule-level description
	if !strings.Contains(content, "description: Rule-specific description") {
		t.Errorf("Expected rule-specific description to override asset description, got: %s", content)
	}
}

func TestRuleHandler_Install_FallbackToAssetName(t *testing.T) {
	tmpDir := t.TempDir()

	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name: "my-rule",
		},
		Rule: &metadata.RuleConfig{
			PromptFile: "RULE.md",
		},
	}

	handler := NewRuleHandler(meta, "")
	zipData := createTestRuleZip(t, "Content.")

	if err := handler.Install(context.TODO(), zipData, tmpDir); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	content := readInstalledRule(t, tmpDir, "my-rule")

	// Should use asset name as title
	if !strings.Contains(content, "# my-rule") {
		t.Errorf("Expected asset name as title when no rule title set")
	}
}

func TestRuleHandler_Remove(t *testing.T) {
	tmpDir := t.TempDir()

	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "remove-test"},
	}

	handler := NewRuleHandler(meta, "")

	// Create the file first
	rulesDir := filepath.Join(tmpDir, "rules")
	if err := os.MkdirAll(rulesDir, 0755); err != nil {
		t.Fatalf("Failed to create rules dir: %v", err)
	}
	filePath := filepath.Join(rulesDir, "remove-test.mdc")
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

func TestRuleHandler_VerifyInstalled(t *testing.T) {
	tmpDir := t.TempDir()

	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "verify-test"},
	}

	handler := NewRuleHandler(meta, "")

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
	filePath := filepath.Join(rulesDir, "verify-test.mdc")
	if err := os.WriteFile(filePath, []byte("content"), 0644); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

	// Should be installed now
	installed, msg := handler.VerifyInstalled(tmpDir)
	if !installed {
		t.Errorf("Should be installed after creating file, msg: %s", msg)
	}
}

// Helper functions

func readInstalledRule(t *testing.T, tmpDir, name string) string {
	t.Helper()
	filePath := filepath.Join(tmpDir, "rules", name+".mdc")
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("Failed to read installed rule: %v", err)
	}
	return string(content)
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
