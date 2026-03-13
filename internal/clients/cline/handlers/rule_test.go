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

func TestRuleHandler_BuildMDContent_NoGlobs(t *testing.T) {
	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name:        "coding-standards",
			Description: "Follow these coding standards",
		},
		Rule: &metadata.RuleConfig{},
	}

	handler := NewRuleHandler(meta)
	content := handler.buildMDContent("Always follow these rules.")

	// Should have description
	if !strings.Contains(content, "description: Follow these coding standards") {
		t.Errorf("Expected description from asset metadata, got: %s", content)
	}

	// Should NOT have paths when no globs specified
	if strings.Contains(content, "paths:") {
		t.Errorf("Should not have paths when no globs specified")
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

func TestRuleHandler_BuildMDContent_SingleGlob(t *testing.T) {
	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name:        "test-rules",
			Description: "Test file rules",
		},
		Rule: &metadata.RuleConfig{
			Globs: []string{"**/*_test.go"},
		},
	}

	handler := NewRuleHandler(meta)
	content := handler.buildMDContent("Test content.")

	// Should have single glob with paths: (Cline uses paths, not globs)
	if !strings.Contains(content, "paths:") {
		t.Errorf("Expected paths: in frontmatter, got: %s", content)
	}
	if !strings.Contains(content, "**/*_test.go") {
		t.Errorf("Expected glob pattern, got: %s", content)
	}
}

func TestRuleHandler_BuildMDContent_MultipleGlobs(t *testing.T) {
	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name: "multi-glob-rules",
		},
		Rule: &metadata.RuleConfig{
			Globs: []string{"**/*.go", "**/*.mod", "**/*.sum"},
		},
	}

	handler := NewRuleHandler(meta)
	content := handler.buildMDContent("Content.")

	// Multiple globs should be formatted as YAML array with paths:
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

func TestRuleHandler_BuildMDContent_RuleDescription(t *testing.T) {
	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name:        "custom-desc",
			Description: "Generic asset description",
		},
		Rule: &metadata.RuleConfig{
			Description: "Rule-specific description",
		},
	}

	handler := NewRuleHandler(meta)
	content := handler.buildMDContent("Content.")

	// Should use rule-level description
	if !strings.Contains(content, "description: Rule-specific description") {
		t.Errorf("Expected rule-specific description to override asset description, got: %s", content)
	}
	if strings.Contains(content, "Generic asset description") {
		t.Errorf("Should not contain generic asset description")
	}
}

func TestRuleHandler_BuildMDContent_FallbackToAssetDescription(t *testing.T) {
	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name:        "my-rule",
			Description: "Asset level description",
		},
	}

	handler := NewRuleHandler(meta)
	content := handler.buildMDContent("Content.")

	// Should use asset description when no rule description
	if !strings.Contains(content, "description: Asset level description") {
		t.Errorf("Expected asset description as fallback, got: %s", content)
	}
}

func TestRuleHandler_DetermineRulesDir_Global(t *testing.T) {
	home, _ := os.UserHomeDir()
	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "test"},
	}

	handler := NewRuleHandler(meta)

	// Global scope: targetBase is ~/.cline
	targetBase := filepath.Join(home, ConfigDir)
	rulesDir, err := handler.determineRulesDir(targetBase)
	if err != nil {
		t.Fatalf("determineRulesDir failed: %v", err)
	}

	// Always use ~/.cline/rules/ (not ~/Documents/Cline/Rules)
	expected := filepath.Join(home, ConfigDir, DirRules)
	if rulesDir != expected {
		t.Errorf("Expected %s, got %s", expected, rulesDir)
	}
}

func TestRuleHandler_DetermineRulesDir_Repo(t *testing.T) {
	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "test"},
	}

	handler := NewRuleHandler(meta)

	// Repo scope: targetBase is {repo}/.cline
	targetBase := "/path/to/repo/.cline"
	rulesDir, err := handler.determineRulesDir(targetBase)
	if err != nil {
		t.Fatalf("determineRulesDir failed: %v", err)
	}

	expected := "/path/to/repo/.clinerules"
	if rulesDir != expected {
		t.Errorf("Expected %s, got %s", expected, rulesDir)
	}
}

func TestRuleHandler_DetermineRulesDir_Path(t *testing.T) {
	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "test"},
	}

	handler := NewRuleHandler(meta)

	// Path scope: targetBase is {repo}/{path}/.cline
	targetBase := "/path/to/repo/services/api/.cline"
	rulesDir, err := handler.determineRulesDir(targetBase)
	if err != nil {
		t.Fatalf("determineRulesDir failed: %v", err)
	}

	expected := "/path/to/repo/services/api/.clinerules"
	if rulesDir != expected {
		t.Errorf("Expected %s, got %s", expected, rulesDir)
	}
}

func TestRuleHandler_Install_CreatesFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a fake .cline directory structure
	targetBase := filepath.Join(tmpDir, ".cline")
	if err := os.MkdirAll(targetBase, 0755); err != nil {
		t.Fatalf("Failed to create target dir: %v", err)
	}

	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name:        "test-rule",
			Description: "Test description",
		},
		Rule: &metadata.RuleConfig{
			PromptFile: "RULE.md",
			Globs:      []string{"src/**/*.ts"},
		},
	}

	handler := NewRuleHandler(meta)

	// Create a test zip with rule content
	zipData := createTestRuleZip(t, "Test rule content.")

	if err := handler.Install(context.TODO(), zipData, targetBase); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	// Verify file was created in .clinerules/ (sibling to .cline/)
	rulesDir := filepath.Join(tmpDir, ".clinerules")
	filePath := filepath.Join(rulesDir, "test-rule.md")
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Errorf("Expected rule file to exist at %s", filePath)
	}

	// Verify content
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	contentStr := string(content)
	if !strings.Contains(contentStr, "Test rule content") {
		t.Errorf("File should contain rule content, got: %s", contentStr)
	}
	if !strings.Contains(contentStr, "paths:") {
		t.Errorf("File should contain paths: frontmatter, got: %s", contentStr)
	}
	if !strings.Contains(contentStr, "src/**/*.ts") {
		t.Errorf("File should contain glob pattern, got: %s", contentStr)
	}
}

func TestRuleHandler_Remove(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a fake .cline directory
	targetBase := filepath.Join(tmpDir, ".cline")
	if err := os.MkdirAll(targetBase, 0755); err != nil {
		t.Fatalf("Failed to create target dir: %v", err)
	}

	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "remove-test"},
	}

	handler := NewRuleHandler(meta)

	// Create the file first in .clinerules/
	rulesDir := filepath.Join(tmpDir, ".clinerules")
	if err := os.MkdirAll(rulesDir, 0755); err != nil {
		t.Fatalf("Failed to create rules dir: %v", err)
	}
	filePath := filepath.Join(rulesDir, "remove-test.md")
	if err := os.WriteFile(filePath, []byte("content"), 0644); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

	// Remove it
	if err := handler.Remove(context.TODO(), targetBase); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	// Verify file was removed
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Errorf("File should have been removed")
	}
}

func TestRuleHandler_VerifyInstalled(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a fake .cline directory
	targetBase := filepath.Join(tmpDir, ".cline")
	if err := os.MkdirAll(targetBase, 0755); err != nil {
		t.Fatalf("Failed to create target dir: %v", err)
	}

	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "verify-test"},
	}

	handler := NewRuleHandler(meta)

	// Should not be installed initially
	installed, _ := handler.VerifyInstalled(targetBase)
	if installed {
		t.Error("Should not be installed initially")
	}

	// Create the file in .clinerules/
	rulesDir := filepath.Join(tmpDir, ".clinerules")
	if err := os.MkdirAll(rulesDir, 0755); err != nil {
		t.Fatalf("Failed to create rules dir: %v", err)
	}
	filePath := filepath.Join(rulesDir, "verify-test.md")
	if err := os.WriteFile(filePath, []byte("content"), 0644); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

	// Should be installed now
	installed, msg := handler.VerifyInstalled(targetBase)
	if !installed {
		t.Errorf("Should be installed after creating file, msg: %s", msg)
	}
}

func TestRuleHandler_GetGlobs(t *testing.T) {
	tests := []struct {
		name     string
		meta     *metadata.Metadata
		expected []string
	}{
		{
			name: "returns rule globs",
			meta: &metadata.Metadata{
				Asset: metadata.Asset{Name: "test"},
				Rule: &metadata.RuleConfig{
					Globs: []string{"**/*.go", "**/*.ts"},
				},
			},
			expected: []string{"**/*.go", "**/*.ts"},
		},
		{
			name: "returns nil when no rule config",
			meta: &metadata.Metadata{
				Asset: metadata.Asset{Name: "test"},
			},
			expected: nil,
		},
		{
			name: "returns nil when empty globs",
			meta: &metadata.Metadata{
				Asset: metadata.Asset{Name: "test"},
				Rule:  &metadata.RuleConfig{},
			},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewRuleHandler(tt.meta)
			got := handler.getGlobs()

			if tt.expected == nil {
				if got != nil {
					t.Errorf("Expected nil, got %v", got)
				}
			} else {
				if len(got) != len(tt.expected) {
					t.Errorf("Expected %v, got %v", tt.expected, got)
				}
				for i, v := range tt.expected {
					if got[i] != v {
						t.Errorf("Expected %v at index %d, got %v", v, i, got[i])
					}
				}
			}
		})
	}
}

func TestRuleHandler_GetDescription(t *testing.T) {
	tests := []struct {
		name     string
		meta     *metadata.Metadata
		expected string
	}{
		{
			name: "uses rule description",
			meta: &metadata.Metadata{
				Asset: metadata.Asset{
					Name:        "test",
					Description: "Asset desc",
				},
				Rule: &metadata.RuleConfig{
					Description: "Rule desc",
				},
			},
			expected: "Rule desc",
		},
		{
			name: "falls back to asset description",
			meta: &metadata.Metadata{
				Asset: metadata.Asset{
					Name:        "test",
					Description: "Asset desc",
				},
			},
			expected: "Asset desc",
		},
		{
			name: "empty when no description",
			meta: &metadata.Metadata{
				Asset: metadata.Asset{Name: "test"},
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewRuleHandler(tt.meta)
			got := handler.getDescription()
			if got != tt.expected {
				t.Errorf("getDescription() = %v, want %v", got, tt.expected)
			}
		})
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
