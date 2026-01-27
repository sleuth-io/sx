package handlers

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/metadata"
)

func TestShouldUseAgentsMd_NoAgentsFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Create only CLAUDE.md, no AGENTS.md
	claudePath := filepath.Join(tmpDir, "CLAUDE.md")
	if err := os.WriteFile(claudePath, []byte("# Project\n"), 0644); err != nil {
		t.Fatalf("Failed to create CLAUDE.md: %v", err)
	}

	// Should return false when AGENTS.md doesn't exist
	if shouldUseAgentsMd(tmpDir) {
		t.Error("shouldUseAgentsMd should return false when AGENTS.md doesn't exist")
	}
}

func TestShouldUseAgentsMd_SymlinkToAgents(t *testing.T) {
	tmpDir := t.TempDir()

	// Create AGENTS.md
	agentsPath := filepath.Join(tmpDir, "AGENTS.md")
	if err := os.WriteFile(agentsPath, []byte("# Agents\n"), 0644); err != nil {
		t.Fatalf("Failed to create AGENTS.md: %v", err)
	}

	// Create CLAUDE.md as symlink to AGENTS.md
	claudePath := filepath.Join(tmpDir, "CLAUDE.md")
	if err := os.Symlink("AGENTS.md", claudePath); err != nil {
		t.Fatalf("Failed to create symlink: %v", err)
	}

	// Should return true when CLAUDE.md is symlink to AGENTS.md
	if !shouldUseAgentsMd(tmpDir) {
		t.Error("shouldUseAgentsMd should return true when CLAUDE.md is symlink to AGENTS.md")
	}
}

func TestShouldUseAgentsMd_SymlinkAbsolutePath(t *testing.T) {
	tmpDir := t.TempDir()

	// Create AGENTS.md
	agentsPath := filepath.Join(tmpDir, "AGENTS.md")
	if err := os.WriteFile(agentsPath, []byte("# Agents\n"), 0644); err != nil {
		t.Fatalf("Failed to create AGENTS.md: %v", err)
	}

	// Create CLAUDE.md as symlink with absolute path
	claudePath := filepath.Join(tmpDir, "CLAUDE.md")
	if err := os.Symlink(agentsPath, claudePath); err != nil {
		t.Fatalf("Failed to create symlink: %v", err)
	}

	// Should return true when CLAUDE.md is symlink to AGENTS.md (absolute path)
	if !shouldUseAgentsMd(tmpDir) {
		t.Error("shouldUseAgentsMd should return true for absolute path symlink")
	}
}

func TestShouldUseAgentsMd_ContainsReference(t *testing.T) {
	tmpDir := t.TempDir()

	// Create AGENTS.md
	agentsPath := filepath.Join(tmpDir, "AGENTS.md")
	if err := os.WriteFile(agentsPath, []byte("# Agents\n"), 0644); err != nil {
		t.Fatalf("Failed to create AGENTS.md: %v", err)
	}

	// Create CLAUDE.md with @AGENTS.md reference
	claudePath := filepath.Join(tmpDir, "CLAUDE.md")
	content := `# Project

@AGENTS.md

Some other content.
`
	if err := os.WriteFile(claudePath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create CLAUDE.md: %v", err)
	}

	// Should return true when CLAUDE.md contains @AGENTS.md
	if !shouldUseAgentsMd(tmpDir) {
		t.Error("shouldUseAgentsMd should return true when CLAUDE.md contains @AGENTS.md reference")
	}
}

func TestShouldUseAgentsMd_ReferenceWithWhitespace(t *testing.T) {
	tmpDir := t.TempDir()

	// Create AGENTS.md
	agentsPath := filepath.Join(tmpDir, "AGENTS.md")
	if err := os.WriteFile(agentsPath, []byte("# Agents\n"), 0644); err != nil {
		t.Fatalf("Failed to create AGENTS.md: %v", err)
	}

	// Create CLAUDE.md with @AGENTS.md reference with leading whitespace
	claudePath := filepath.Join(tmpDir, "CLAUDE.md")
	content := `# Project

  @AGENTS.md

Some other content.
`
	if err := os.WriteFile(claudePath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create CLAUDE.md: %v", err)
	}

	// Should return true - pattern allows whitespace
	if !shouldUseAgentsMd(tmpDir) {
		t.Error("shouldUseAgentsMd should return true with whitespace around @AGENTS.md")
	}
}

func TestShouldUseAgentsMd_NoReference(t *testing.T) {
	tmpDir := t.TempDir()

	// Create AGENTS.md
	agentsPath := filepath.Join(tmpDir, "AGENTS.md")
	if err := os.WriteFile(agentsPath, []byte("# Agents\n"), 0644); err != nil {
		t.Fatalf("Failed to create AGENTS.md: %v", err)
	}

	// Create CLAUDE.md without @AGENTS.md reference
	claudePath := filepath.Join(tmpDir, "CLAUDE.md")
	content := `# Project

This file mentions AGENTS.md but doesn't import it.
`
	if err := os.WriteFile(claudePath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create CLAUDE.md: %v", err)
	}

	// Should return false - no @AGENTS.md reference
	if shouldUseAgentsMd(tmpDir) {
		t.Error("shouldUseAgentsMd should return false without @AGENTS.md reference")
	}
}

func TestShouldUseAgentsMd_ReferenceInline(t *testing.T) {
	tmpDir := t.TempDir()

	// Create AGENTS.md
	agentsPath := filepath.Join(tmpDir, "AGENTS.md")
	if err := os.WriteFile(agentsPath, []byte("# Agents\n"), 0644); err != nil {
		t.Fatalf("Failed to create AGENTS.md: %v", err)
	}

	// Create CLAUDE.md with @AGENTS.md inline (not on its own line)
	claudePath := filepath.Join(tmpDir, "CLAUDE.md")
	content := `# Project

See @AGENTS.md for more info.
`
	if err := os.WriteFile(claudePath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create CLAUDE.md: %v", err)
	}

	// Should return false - reference must be on its own line
	if shouldUseAgentsMd(tmpDir) {
		t.Error("shouldUseAgentsMd should return false when @AGENTS.md is inline")
	}
}

func TestShouldUseAgentsMd_NeitherFileExists(t *testing.T) {
	tmpDir := t.TempDir()

	// Neither file exists
	if shouldUseAgentsMd(tmpDir) {
		t.Error("shouldUseAgentsMd should return false when neither file exists")
	}
}

func TestShouldUseAgentsMd_OnlyAgentsExists(t *testing.T) {
	tmpDir := t.TempDir()

	// Create only AGENTS.md
	agentsPath := filepath.Join(tmpDir, "AGENTS.md")
	if err := os.WriteFile(agentsPath, []byte("# Agents\n"), 0644); err != nil {
		t.Fatalf("Failed to create AGENTS.md: %v", err)
	}

	// Should return false - CLAUDE.md doesn't exist to check for symlink/reference
	if shouldUseAgentsMd(tmpDir) {
		t.Error("shouldUseAgentsMd should return false when only AGENTS.md exists")
	}
}

func TestIsSymlinkTo_NotASymlink(t *testing.T) {
	tmpDir := t.TempDir()

	source := filepath.Join(tmpDir, "source.md")
	target := filepath.Join(tmpDir, "target.md")

	// Create regular files
	if err := os.WriteFile(source, []byte("source"), 0644); err != nil {
		t.Fatalf("Failed to create source: %v", err)
	}
	if err := os.WriteFile(target, []byte("target"), 0644); err != nil {
		t.Fatalf("Failed to create target: %v", err)
	}

	if isSymlinkTo(source, target) {
		t.Error("isSymlinkTo should return false for regular file")
	}
}

func TestIsSymlinkTo_SymlinkToDifferentFile(t *testing.T) {
	tmpDir := t.TempDir()

	target := filepath.Join(tmpDir, "target.md")
	other := filepath.Join(tmpDir, "other.md")
	source := filepath.Join(tmpDir, "source.md")

	// Create target files
	if err := os.WriteFile(target, []byte("target"), 0644); err != nil {
		t.Fatalf("Failed to create target: %v", err)
	}
	if err := os.WriteFile(other, []byte("other"), 0644); err != nil {
		t.Fatalf("Failed to create other: %v", err)
	}

	// Create symlink to other file
	if err := os.Symlink("other.md", source); err != nil {
		t.Fatalf("Failed to create symlink: %v", err)
	}

	if isSymlinkTo(source, target) {
		t.Error("isSymlinkTo should return false when symlink points to different file")
	}
}

func TestIsSymlinkTo_SourceDoesNotExist(t *testing.T) {
	tmpDir := t.TempDir()

	source := filepath.Join(tmpDir, "nonexistent.md")
	target := filepath.Join(tmpDir, "target.md")

	if isSymlinkTo(source, target) {
		t.Error("isSymlinkTo should return false when source doesn't exist")
	}
}

func TestContainsAgentsReference_ValidPatterns(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{
			name:    "simple reference",
			content: "@AGENTS.md\n",
			want:    true,
		},
		{
			name:    "reference with leading spaces",
			content: "  @AGENTS.md\n",
			want:    true,
		},
		{
			name:    "reference with trailing spaces",
			content: "@AGENTS.md  \n",
			want:    true,
		},
		{
			name:    "reference with tabs",
			content: "\t@AGENTS.md\t\n",
			want:    true,
		},
		{
			name:    "reference in middle of file",
			content: "# Header\n\n@AGENTS.md\n\nMore content\n",
			want:    true,
		},
		{
			name:    "inline reference - should not match",
			content: "See @AGENTS.md for details\n",
			want:    false,
		},
		{
			name:    "partial match - should not match",
			content: "@AGENTS.md.bak\n",
			want:    false,
		},
		{
			name:    "lowercase - should not match",
			content: "@agents.md\n",
			want:    false,
		},
		{
			name:    "no reference",
			content: "# Project\nSome content\n",
			want:    false,
		},
		{
			name:    "empty file",
			content: "",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			path := filepath.Join(tmpDir, "test.md")

			if err := os.WriteFile(path, []byte(tt.content), 0644); err != nil {
				t.Fatalf("Failed to write file: %v", err)
			}

			got := containsAgentsReference(path)
			if got != tt.want {
				t.Errorf("containsAgentsReference() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestContainsAgentsReference_FileNotFound(t *testing.T) {
	if containsAgentsReference("/nonexistent/path/file.md") {
		t.Error("containsAgentsReference should return false for nonexistent file")
	}
}

func TestInstructionHandler_GetTargetFile(t *testing.T) {
	tmpDir := t.TempDir()

	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name: "test-instruction",
		},
	}

	lockConfig := &lockfile.InstructionInstallConfig{
		Heading:   "## Test",
		EndMarker: "---",
	}

	handler := NewInstructionHandler(meta, lockConfig)

	// Test with no AGENTS.md - should use CLAUDE.md
	targetFile := handler.getTargetFile(tmpDir)
	expected := filepath.Join(tmpDir, "CLAUDE.md")
	if targetFile != expected {
		t.Errorf("getTargetFile() = %v, want %v", targetFile, expected)
	}

	// Create AGENTS.md and CLAUDE.md with @AGENTS.md reference
	agentsPath := filepath.Join(tmpDir, "AGENTS.md")
	if err := os.WriteFile(agentsPath, []byte("# Agents\n"), 0644); err != nil {
		t.Fatalf("Failed to create AGENTS.md: %v", err)
	}

	claudePath := filepath.Join(tmpDir, "CLAUDE.md")
	if err := os.WriteFile(claudePath, []byte("@AGENTS.md\n"), 0644); err != nil {
		t.Fatalf("Failed to create CLAUDE.md: %v", err)
	}

	// Now should use AGENTS.md
	targetFile = handler.getTargetFile(tmpDir)
	expected = filepath.Join(tmpDir, "AGENTS.md")
	if targetFile != expected {
		t.Errorf("getTargetFile() = %v, want %v (should use AGENTS.md)", targetFile, expected)
	}
}

func TestInstructionHandler_GetTitle(t *testing.T) {
	tests := []struct {
		name     string
		meta     *metadata.Metadata
		expected string
	}{
		{
			name: "uses instruction title when set",
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
		{
			name: "falls back when title is empty",
			meta: &metadata.Metadata{
				Asset: metadata.Asset{Name: "asset-name"},
				Instruction: &metadata.InstructionConfig{
					Title: "",
				},
			},
			expected: "asset-name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewInstructionHandler(tt.meta, nil)
			got := handler.getTitle()
			if got != tt.expected {
				t.Errorf("getTitle() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestInstructionHandler_DefaultConfig(t *testing.T) {
	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "test"},
	}

	// Pass nil config - should use defaults
	handler := NewInstructionHandler(meta, nil)

	if handler.lockConfig.Heading != lockfile.DefaultInstructionHeading {
		t.Errorf("Expected default heading %q, got %q", lockfile.DefaultInstructionHeading, handler.lockConfig.Heading)
	}

	if handler.lockConfig.EndMarker != lockfile.DefaultInstructionEndMarker {
		t.Errorf("Expected default end marker %q, got %q", lockfile.DefaultInstructionEndMarker, handler.lockConfig.EndMarker)
	}
}
