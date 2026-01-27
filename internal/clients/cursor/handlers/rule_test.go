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

func TestRuleHandler_BuildMDCContent_AlwaysApply(t *testing.T) {
	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name:        "coding-standards",
			Description: "Follow these coding standards",
		},
		Rule: &metadata.RuleConfig{
			Title: "Coding Standards",
		},
	}

	handler := NewRuleHandler(meta, "")
	content := handler.buildMDCContent("Always follow these rules.")

	// Should have alwaysApply since no path scope and no explicit globs
	if !strings.Contains(content, "alwaysApply: true") {
		t.Errorf("Expected alwaysApply: true for repo-wide rule without globs")
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
	if !strings.Contains(content, "\n---\n\n#") {
		t.Errorf("Expected frontmatter to end before title")
	}
}

func TestRuleHandler_BuildMDCContent_WithPathScope(t *testing.T) {
	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name:        "backend-rules",
			Description: "Backend specific rules",
		},
	}

	handler := NewRuleHandler(meta, "backend/")
	content := handler.buildMDCContent("Backend content.")

	// Should NOT have alwaysApply since it has path scope
	if strings.Contains(content, "alwaysApply") {
		t.Errorf("Should not have alwaysApply when path scoped")
	}

	// Should have auto-generated glob from path
	if !strings.Contains(content, "globs: backend/**/*") {
		t.Errorf("Expected auto-generated glob from path scope, got: %s", content)
	}
}

func TestRuleHandler_BuildMDCContent_ExplicitGlobs(t *testing.T) {
	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name:        "test-rules",
			Description: "Test file rules",
		},
		Rule: &metadata.RuleConfig{
			Globs: []string{"**/*_test.go"},
		},
	}

	handler := NewRuleHandler(meta, "")
	content := handler.buildMDCContent("Test content.")

	// Should have explicit glob
	if !strings.Contains(content, "globs: **/*_test.go") {
		t.Errorf("Expected explicit glob, got: %s", content)
	}

	// Should NOT have alwaysApply when explicit globs provided
	if strings.Contains(content, "alwaysApply") {
		t.Errorf("Should not have alwaysApply with explicit globs")
	}
}

func TestRuleHandler_BuildMDCContent_MultipleGlobs(t *testing.T) {
	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name: "multi-glob-rules",
		},
		Rule: &metadata.RuleConfig{
			Globs: []string{"**/*.go", "**/*.mod", "**/*.sum"},
		},
	}

	handler := NewRuleHandler(meta, "")
	content := handler.buildMDCContent("Content.")

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

func TestRuleHandler_BuildMDCContent_RuleDescription(t *testing.T) {
	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name:        "custom-desc",
			Description: "Generic asset description",
		},
		Rule: &metadata.RuleConfig{
			Description: "Rule-specific description",
			Cursor: map[string]any{
				"always-apply": true,
			},
		},
	}

	handler := NewRuleHandler(meta, "")
	content := handler.buildMDCContent("Content.")

	// Should use rule-level description
	if !strings.Contains(content, "description: Rule-specific description") {
		t.Errorf("Expected rule-specific description to override asset description")
	}
	if strings.Contains(content, "Generic asset description") {
		t.Errorf("Should not contain generic asset description")
	}
}

func TestRuleHandler_BuildMDCContent_FallbackToAssetName(t *testing.T) {
	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name: "my-rule",
		},
	}

	handler := NewRuleHandler(meta, "")
	content := handler.buildMDCContent("Content.")

	// Should use asset name as title
	if !strings.Contains(content, "# my-rule") {
		t.Errorf("Expected asset name as title when no rule title set")
	}
}

func TestRuleHandler_GetGlobs_Priority(t *testing.T) {
	tests := []struct {
		name         string
		meta         *metadata.Metadata
		pathScope    string
		expectedGlob string
	}{
		{
			name: "explicit globs take priority over path scope",
			meta: &metadata.Metadata{
				Asset: metadata.Asset{Name: "test"},
				Rule: &metadata.RuleConfig{
					Globs: []string{"explicit/**/*"},
				},
			},
			pathScope:    "ignored/",
			expectedGlob: "explicit/**/*",
		},
		{
			name: "path scope generates glob when no explicit",
			meta: &metadata.Metadata{
				Asset: metadata.Asset{Name: "test"},
			},
			pathScope:    "services/api/",
			expectedGlob: "services/api/**/*",
		},
		{
			name: "no globs when no scope and no explicit",
			meta: &metadata.Metadata{
				Asset: metadata.Asset{Name: "test"},
			},
			pathScope:    "",
			expectedGlob: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewRuleHandler(tt.meta, tt.pathScope)
			globs := handler.getGlobs()

			if tt.expectedGlob == "" {
				if len(globs) != 0 {
					t.Errorf("Expected no globs, got %v", globs)
				}
			} else {
				if len(globs) == 0 || globs[0] != tt.expectedGlob {
					t.Errorf("Expected glob %q, got %v", tt.expectedGlob, globs)
				}
			}
		})
	}
}

func TestRuleHandler_ShouldAlwaysApply(t *testing.T) {
	tests := []struct {
		name      string
		meta      *metadata.Metadata
		pathScope string
		expected  bool
	}{
		{
			name: "always apply when no scope and no globs",
			meta: &metadata.Metadata{
				Asset: metadata.Asset{Name: "test"},
			},
			pathScope: "",
			expected:  true,
		},
		{
			name: "not always apply when path scoped",
			meta: &metadata.Metadata{
				Asset: metadata.Asset{Name: "test"},
			},
			pathScope: "backend/",
			expected:  false,
		},
		{
			name: "not always apply when explicit globs",
			meta: &metadata.Metadata{
				Asset: metadata.Asset{Name: "test"},
				Rule: &metadata.RuleConfig{
					Globs: []string{"**/*.go"},
				},
			},
			pathScope: "",
			expected:  false,
		},
		{
			name: "explicit always apply overrides",
			meta: &metadata.Metadata{
				Asset: metadata.Asset{Name: "test"},
				Rule: &metadata.RuleConfig{
					Cursor: map[string]any{
						"always-apply": true,
					},
				},
			},
			pathScope: "backend/", // Would normally NOT always apply
			expected:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewRuleHandler(tt.meta, tt.pathScope)
			got := handler.shouldAlwaysApply()
			if got != tt.expected {
				t.Errorf("shouldAlwaysApply() = %v, want %v", got, tt.expected)
			}
		})
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

	handler := NewRuleHandler(meta, "")

	// Create a test zip with rule content
	zipData := createTestRuleZip(t, "Test rule content.")

	if err := handler.Install(context.TODO(), zipData, tmpDir); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	// Verify file was created
	filePath := filepath.Join(tmpDir, "rules", "test-rule.mdc")
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

func TestRuleHandler_GetTitle(t *testing.T) {
	tests := []struct {
		name     string
		meta     *metadata.Metadata
		expected string
	}{
		{
			name: "uses rule title",
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewRuleHandler(tt.meta, "")
			got := handler.getTitle()
			if got != tt.expected {
				t.Errorf("getTitle() = %v, want %v", got, tt.expected)
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
