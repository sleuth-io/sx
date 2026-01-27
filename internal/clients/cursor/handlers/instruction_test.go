package handlers

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sleuth-io/sx/internal/metadata"
)

func TestInstructionHandler_BuildMDCContent_AlwaysApply(t *testing.T) {
	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name:        "coding-standards",
			Description: "Follow these coding standards",
		},
		Instruction: &metadata.InstructionConfig{
			Title: "Coding Standards",
		},
	}

	handler := NewInstructionHandler(meta, "")
	content := handler.buildMDCContent("Always follow these rules.")

	// Should have alwaysApply since no path scope and no explicit globs
	if !strings.Contains(content, "alwaysApply: true") {
		t.Errorf("Expected alwaysApply: true for repo-wide instruction without globs")
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
		t.Errorf("Expected instruction content")
	}

	// Verify frontmatter structure
	if !strings.HasPrefix(content, "---\n") {
		t.Errorf("Expected content to start with frontmatter")
	}
	if !strings.Contains(content, "\n---\n\n#") {
		t.Errorf("Expected frontmatter to end before title")
	}
}

func TestInstructionHandler_BuildMDCContent_WithPathScope(t *testing.T) {
	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name:        "backend-rules",
			Description: "Backend specific rules",
		},
	}

	handler := NewInstructionHandler(meta, "backend/")
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

func TestInstructionHandler_BuildMDCContent_ExplicitGlobs(t *testing.T) {
	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name:        "test-rules",
			Description: "Test file rules",
		},
		Instruction: &metadata.InstructionConfig{
			Cursor: &metadata.CursorInstructionConfig{
				Globs: []string{"**/*_test.go"},
			},
		},
	}

	handler := NewInstructionHandler(meta, "")
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

func TestInstructionHandler_BuildMDCContent_MultipleGlobs(t *testing.T) {
	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name: "multi-glob-rules",
		},
		Instruction: &metadata.InstructionConfig{
			Cursor: &metadata.CursorInstructionConfig{
				Globs: []string{"**/*.go", "**/*.mod", "**/*.sum"},
			},
		},
	}

	handler := NewInstructionHandler(meta, "")
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

func TestInstructionHandler_BuildMDCContent_CursorDescription(t *testing.T) {
	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name:        "custom-desc",
			Description: "Generic asset description",
		},
		Instruction: &metadata.InstructionConfig{
			Cursor: &metadata.CursorInstructionConfig{
				Description: "Cursor-specific description",
				AlwaysApply: true,
			},
		},
	}

	handler := NewInstructionHandler(meta, "")
	content := handler.buildMDCContent("Content.")

	// Should use Cursor-specific description
	if !strings.Contains(content, "description: Cursor-specific description") {
		t.Errorf("Expected Cursor-specific description to override asset description")
	}
	if strings.Contains(content, "Generic asset description") {
		t.Errorf("Should not contain generic asset description")
	}
}

func TestInstructionHandler_BuildMDCContent_FallbackToAssetName(t *testing.T) {
	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name: "my-instruction",
		},
	}

	handler := NewInstructionHandler(meta, "")
	content := handler.buildMDCContent("Content.")

	// Should use asset name as title
	if !strings.Contains(content, "# my-instruction") {
		t.Errorf("Expected asset name as title when no instruction title set")
	}
}

func TestInstructionHandler_GetGlobs_Priority(t *testing.T) {
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
				Instruction: &metadata.InstructionConfig{
					Cursor: &metadata.CursorInstructionConfig{
						Globs: []string{"explicit/**/*"},
					},
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
			handler := NewInstructionHandler(tt.meta, tt.pathScope)
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

func TestInstructionHandler_ShouldAlwaysApply(t *testing.T) {
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
				Instruction: &metadata.InstructionConfig{
					Cursor: &metadata.CursorInstructionConfig{
						Globs: []string{"**/*.go"},
					},
				},
			},
			pathScope: "",
			expected:  false,
		},
		{
			name: "explicit always apply overrides",
			meta: &metadata.Metadata{
				Asset: metadata.Asset{Name: "test"},
				Instruction: &metadata.InstructionConfig{
					Cursor: &metadata.CursorInstructionConfig{
						AlwaysApply: true,
					},
				},
			},
			pathScope: "backend/", // Would normally NOT always apply
			expected:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewInstructionHandler(tt.meta, tt.pathScope)
			got := handler.shouldAlwaysApply()
			if got != tt.expected {
				t.Errorf("shouldAlwaysApply() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestInstructionHandler_Install_CreatesFile(t *testing.T) {
	tmpDir := t.TempDir()

	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name:        "test-rule",
			Description: "Test description",
		},
		Instruction: &metadata.InstructionConfig{
			Title:      "Test Rule",
			PromptFile: "INSTRUCTION.md",
		},
	}

	handler := NewInstructionHandler(meta, "")

	// Create a test zip with instruction content
	zipData := createTestInstructionZip(t, "Test instruction content.")

	if err := handler.Install(nil, zipData, tmpDir); err != nil {
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

	if !strings.Contains(string(content), "Test instruction content") {
		t.Errorf("File should contain instruction content")
	}
	if !strings.Contains(string(content), "# Test Rule") {
		t.Errorf("File should contain title")
	}
}

func TestInstructionHandler_Remove(t *testing.T) {
	tmpDir := t.TempDir()

	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "remove-test"},
	}

	handler := NewInstructionHandler(meta, "")

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
	if err := handler.Remove(nil, tmpDir); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	// Verify file was removed
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Errorf("File should have been removed")
	}
}

func TestInstructionHandler_VerifyInstalled(t *testing.T) {
	tmpDir := t.TempDir()

	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "verify-test"},
	}

	handler := NewInstructionHandler(meta, "")

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

func TestInstructionHandler_GetTitle(t *testing.T) {
	tests := []struct {
		name     string
		meta     *metadata.Metadata
		expected string
	}{
		{
			name: "uses instruction title",
			meta: &metadata.Metadata{
				Asset: metadata.Asset{Name: "asset-name"},
				Instruction: &metadata.InstructionConfig{
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
			handler := NewInstructionHandler(tt.meta, "")
			got := handler.getTitle()
			if got != tt.expected {
				t.Errorf("getTitle() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// createTestInstructionZip creates a minimal zip file with instruction content
func createTestInstructionZip(t *testing.T, content string) []byte {
	t.Helper()

	tmpDir := t.TempDir()

	// Create metadata.toml
	metadataContent := `[asset]
name = "test"
type = "instruction"
version = "1.0.0"

[instruction]
prompt-file = "INSTRUCTION.md"
`
	if err := os.WriteFile(filepath.Join(tmpDir, "metadata.toml"), []byte(metadataContent), 0644); err != nil {
		t.Fatalf("Failed to write metadata: %v", err)
	}

	// Create INSTRUCTION.md
	if err := os.WriteFile(filepath.Join(tmpDir, "INSTRUCTION.md"), []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write instruction: %v", err)
	}

	// Create zip
	zipPath := filepath.Join(tmpDir, "test.zip")
	cmd := "cd " + tmpDir + " && zip -q " + zipPath + " metadata.toml INSTRUCTION.md"
	if err := os.WriteFile(filepath.Join(tmpDir, "run.sh"), []byte(cmd), 0755); err != nil {
		t.Fatalf("Failed to write script: %v", err)
	}

	// Use a simpler approach - just create a zip manually
	// For testing, we can use the internal zip utility or just test the buildMDCContent directly
	// For now, let's read the files directly since Install() calls readInstructionContent which uses zip

	// Actually for this test, we need a real zip. Let's use archive/zip
	return createZipFromFiles(t, map[string]string{
		"metadata.toml": metadataContent,
		"INSTRUCTION.md": content,
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
