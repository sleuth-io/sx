package cline

import (
	"strings"
	"testing"

	"github.com/sleuth-io/sx/internal/metadata"
)

func TestRuleCapabilities_BasicProperties(t *testing.T) {
	caps := RuleCapabilities()

	if caps.ClientName != "cline" {
		t.Errorf("Expected ClientName 'cline', got %s", caps.ClientName)
	}

	if caps.RulesDirectory != ".clinerules" {
		t.Errorf("Expected RulesDirectory '.clinerules', got %s", caps.RulesDirectory)
	}

	if caps.FileExtension != ".md" {
		t.Errorf("Expected FileExtension '.md', got %s", caps.FileExtension)
	}

	// Verify instruction files include common formats
	foundCursor := false
	foundWindsurf := false
	foundAgents := false
	for _, f := range caps.InstructionFiles {
		switch f {
		case ".cursorrules":
			foundCursor = true
		case ".windsurfrules":
			foundWindsurf = true
		case "AGENTS.md":
			foundAgents = true
		}
	}
	if !foundCursor || !foundWindsurf || !foundAgents {
		t.Errorf("Missing expected instruction files, got: %v", caps.InstructionFiles)
	}
}

func TestMatchesPath(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{".clinerules/coding-style.md", true},
		{"/path/to/repo/.clinerules/test.md", true},
		{".clinerules/nested/rule.md", true},
		{".cursor/rules/test.mdc", false},
		{".claude/rules/test.md", false},
		{"README.md", false},
		{".clinerules/test.txt", false}, // Wrong extension
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := matchesPath(tt.path)
			if got != tt.expected {
				t.Errorf("matchesPath(%q) = %v, want %v", tt.path, got, tt.expected)
			}
		})
	}
}

func TestMatchesContent(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		content  string
		expected bool
	}{
		{
			name: "has paths frontmatter in clinerules",
			path: ".clinerules/test.md",
			content: `---
paths:
  - src/**/*.ts
---
# Rule content`,
			expected: true,
		},
		{
			name: "has paths inline in cline dir",
			path: ".cline/rules/test.md",
			content: `---
description: Test rule
paths: ["**/*.go"]
---
Content here`,
			expected: true,
		},
		{
			name:     "no paths",
			path:     ".clinerules/test.md",
			content:  "# Just markdown\nNo frontmatter",
			expected: false,
		},
		{
			name: "uses globs instead of paths",
			path: ".clinerules/test.md",
			content: `---
globs: ["**/*.ts"]
---
Content`,
			expected: false, // Cline uses paths, not globs
		},
		{
			name: "claude code file with paths - should not match",
			path: ".claude/rules/test.md",
			content: `---
paths:
  - src/**/*.ts
---
# Rule content`,
			expected: false, // Claude Code file, not Cline
		},
		{
			name: "unrelated path - should not match",
			path: "random/test.md",
			content: `---
paths:
  - src/**/*.ts
---
# Rule content`,
			expected: false, // Not in a Cline-specific location
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesContent(tt.path, []byte(tt.content))
			if got != tt.expected {
				t.Errorf("matchesContent(%q, ...) = %v, want %v", tt.path, got, tt.expected)
			}
		})
	}
}

func TestParseRuleFile_WithFrontmatter(t *testing.T) {
	content := `---
description: TypeScript rules
paths:
  - src/**/*.ts
  - src/**/*.tsx
---
# TypeScript Best Practices

Always use strict mode.`

	parsed, err := parseRuleFile([]byte(content))
	if err != nil {
		t.Fatalf("parseRuleFile failed: %v", err)
	}

	if parsed.ClientName != "cline" {
		t.Errorf("Expected ClientName 'cline', got %s", parsed.ClientName)
	}

	if parsed.Description != "TypeScript rules" {
		t.Errorf("Expected description 'TypeScript rules', got %s", parsed.Description)
	}

	if len(parsed.Globs) != 2 {
		t.Errorf("Expected 2 globs, got %d", len(parsed.Globs))
	}

	if parsed.Globs[0] != "src/**/*.ts" {
		t.Errorf("Expected first glob 'src/**/*.ts', got %s", parsed.Globs[0])
	}

	if !strings.Contains(parsed.Content, "TypeScript Best Practices") {
		t.Errorf("Content should contain rule body")
	}
}

func TestParseRuleFile_NoFrontmatter(t *testing.T) {
	content := `# Simple Rule

Just some content without frontmatter.`

	parsed, err := parseRuleFile([]byte(content))
	if err != nil {
		t.Fatalf("parseRuleFile failed: %v", err)
	}

	if parsed.ClientName != "cline" {
		t.Errorf("Expected ClientName 'cline', got %s", parsed.ClientName)
	}

	// Should have the full content
	if !strings.Contains(parsed.Content, "Simple Rule") {
		t.Errorf("Content should contain the full content")
	}

	// No globs or description
	if len(parsed.Globs) != 0 {
		t.Errorf("Expected no globs, got %v", parsed.Globs)
	}
}

func TestGenerateRuleFile_WithGlobs(t *testing.T) {
	cfg := &metadata.RuleConfig{
		Description: "Test rule",
		Globs:       []string{"src/**/*.go", "pkg/**/*.go"},
	}

	result := generateRuleFile(cfg, "# Rule Body\n\nContent here.")

	resultStr := string(result)

	// Should have frontmatter
	if !strings.HasPrefix(resultStr, "---\n") {
		t.Errorf("Expected frontmatter, got: %s", resultStr)
	}

	// Should have description
	if !strings.Contains(resultStr, "description: Test rule") {
		t.Errorf("Expected description in frontmatter")
	}

	// Should use 'paths:' not 'globs:'
	if !strings.Contains(resultStr, "paths:") {
		t.Errorf("Expected paths: in frontmatter (Cline format)")
	}

	// Should have both globs
	if !strings.Contains(resultStr, "src/**/*.go") {
		t.Errorf("Expected first glob pattern")
	}
	if !strings.Contains(resultStr, "pkg/**/*.go") {
		t.Errorf("Expected second glob pattern")
	}

	// Should have body
	if !strings.Contains(resultStr, "# Rule Body") {
		t.Errorf("Expected rule body")
	}
}

func TestGenerateRuleFile_NoGlobs(t *testing.T) {
	cfg := &metadata.RuleConfig{
		Description: "Global rule",
	}

	result := generateRuleFile(cfg, "Always apply this rule.")

	resultStr := string(result)

	// Should have frontmatter with just description
	if !strings.HasPrefix(resultStr, "---\n") {
		t.Errorf("Expected frontmatter, got: %s", resultStr)
	}

	if !strings.Contains(resultStr, "description: Global rule") {
		t.Errorf("Expected description")
	}

	// Should NOT have paths since no globs
	if strings.Contains(resultStr, "paths:") {
		t.Errorf("Should not have paths when no globs specified")
	}

	// Should have body
	if !strings.Contains(resultStr, "Always apply this rule.") {
		t.Errorf("Expected rule body")
	}
}

func TestGenerateRuleFile_NilConfig(t *testing.T) {
	result := generateRuleFile(nil, "Content only.")

	resultStr := string(result)

	// Should just have content, no frontmatter
	if strings.Contains(resultStr, "---") {
		t.Errorf("Should not have frontmatter with nil config, got: %s", resultStr)
	}

	if !strings.Contains(resultStr, "Content only.") {
		t.Errorf("Expected content")
	}
}

func TestDetectAssetType(t *testing.T) {
	tests := []struct {
		path         string
		expectedType string // empty for nil
	}{
		{".clinerules/coding.md", "rule"},
		{"/repo/.clinerules/test.md", "rule"},
		{".cline/skills/my-skill/SKILL.md", "skill"},
		{"/home/user/.cline/skills/test/SKILL.md", "skill"},
		{".cursor/rules/test.mdc", ""}, // Not Cline
		{".claude/skills/test.md", ""}, // Not Cline
		{"README.md", ""},              // Not a Cline asset
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := detectAssetType(tt.path, nil)

			if tt.expectedType == "" {
				if result != nil {
					t.Errorf("Expected nil for %q, got %v", tt.path, result)
				}
			} else {
				if result == nil {
					t.Errorf("Expected %s for %q, got nil", tt.expectedType, tt.path)
				} else if result.Key != tt.expectedType {
					t.Errorf("Expected %s for %q, got %s", tt.expectedType, tt.path, result.Key)
				}
			}
		})
	}
}

func TestToStringSlice(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected []string
	}{
		{
			name:     "string slice",
			input:    []string{"a", "b"},
			expected: []string{"a", "b"},
		},
		{
			name:     "interface slice",
			input:    []any{"x", "y", "z"},
			expected: []string{"x", "y", "z"},
		},
		{
			name:     "single string",
			input:    "single",
			expected: []string{"single"},
		},
		{
			name:     "nil",
			input:    nil,
			expected: nil,
		},
		{
			name:     "int (unsupported)",
			input:    123,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toStringSlice(tt.input)

			if tt.expected == nil {
				if got != nil {
					t.Errorf("Expected nil, got %v", got)
				}
				return
			}

			if len(got) != len(tt.expected) {
				t.Errorf("Expected %v, got %v", tt.expected, got)
				return
			}

			for i, v := range tt.expected {
				if got[i] != v {
					t.Errorf("Expected %v at index %d, got %v", v, i, got[i])
				}
			}
		})
	}
}
