package claude_code

import (
	"strings"
	"testing"

	"github.com/sleuth-io/sx/internal/metadata"
)

func TestMatchesPath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{
			name:     "matches claude rules path",
			path:     ".claude/rules/my-rule.md",
			expected: true,
		},
		{
			name:     "matches nested claude rules path",
			path:     "backend/.claude/rules/go-standards.md",
			expected: true,
		},
		{
			name:     "does not match cursor rules",
			path:     ".cursor/rules/my-rule.mdc",
			expected: false,
		},
		{
			name:     "does not match non-md file",
			path:     ".claude/rules/my-rule.txt",
			expected: false,
		},
		{
			name:     "does not match CLAUDE.md",
			path:     "CLAUDE.md",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
			name: "matches content with paths",
			path: "rule.md",
			content: `---
paths:
  - **/*.go
---

# Rule`,
			expected: true,
		},
		{
			name:     "does not match content without paths",
			path:     "rule.md",
			content:  "# Just a heading\n\nSome content.",
			expected: false,
		},
		{
			name: "does not match cursor globs",
			path: "rule.md",
			content: `---
globs:
  - **/*.go
---

# Rule`,
			expected: false,
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

func TestParseRuleFile(t *testing.T) {
	tests := []struct {
		name            string
		content         string
		expectedGlobs   []string
		expectedDesc    string
		expectedContent string
	}{
		{
			name: "parses full frontmatter",
			content: `---
description: Go coding standards
paths:
  - "**/*.go"
  - "**/*.mod"
---

# Go Standards

Follow these rules.`,
			expectedGlobs:   []string{"**/*.go", "**/*.mod"},
			expectedDesc:    "Go coding standards",
			expectedContent: "# Go Standards\n\nFollow these rules.",
		},
		{
			name: "parses single path",
			content: `---
paths:
  - "**/*.go"
---

Content here.`,
			expectedGlobs:   []string{"**/*.go"},
			expectedContent: "Content here.",
		},
		{
			name:            "handles no frontmatter",
			content:         "# Just content\n\nNo frontmatter.",
			expectedGlobs:   nil,
			expectedContent: "# Just content\n\nNo frontmatter.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseRuleFile([]byte(tt.content))
			if err != nil {
				t.Fatalf("parseRuleFile() error = %v", err)
			}

			if result.ClientName != "claude-code" {
				t.Errorf("ClientName = %q, want %q", result.ClientName, "claude-code")
			}

			if len(result.Globs) != len(tt.expectedGlobs) {
				t.Errorf("Globs = %v, want %v", result.Globs, tt.expectedGlobs)
			} else {
				for i, g := range result.Globs {
					if g != tt.expectedGlobs[i] {
						t.Errorf("Globs[%d] = %q, want %q", i, g, tt.expectedGlobs[i])
					}
				}
			}

			if result.Description != tt.expectedDesc {
				t.Errorf("Description = %q, want %q", result.Description, tt.expectedDesc)
			}

			if strings.TrimSpace(result.Content) != strings.TrimSpace(tt.expectedContent) {
				t.Errorf("Content = %q, want %q", result.Content, tt.expectedContent)
			}
		})
	}
}

func TestGenerateRuleFile(t *testing.T) {
	tests := []struct {
		name        string
		cfg         *metadata.RuleConfig
		body        string
		contains    []string
		notContains []string
	}{
		{
			name: "generates with globs and description",
			cfg: &metadata.RuleConfig{
				Description: "Go standards",
				Globs:       []string{"**/*.go"},
			},
			body: "# Go\n\nContent here.",
			contains: []string{
				"---\n",
				"description: Go standards",
				"paths:",
				"**/*.go",
				"# Go\n\nContent here.",
			},
		},
		{
			name: "generates with multiple globs",
			cfg: &metadata.RuleConfig{
				Globs: []string{"**/*.go", "**/*.mod"},
			},
			body: "Content.",
			contains: []string{
				"paths:",
				"  - **/*.go",
				"  - **/*.mod",
			},
		},
		{
			name: "generates without frontmatter when no config",
			cfg:  nil,
			body: "# Just content",
			notContains: []string{
				"---",
				"paths:",
				"description:",
			},
			contains: []string{
				"# Just content",
			},
		},
		{
			name: "generates without frontmatter when empty config",
			cfg:  &metadata.RuleConfig{},
			body: "# Just content",
			notContains: []string{
				"---",
				"paths:",
				"description:",
			},
			contains: []string{
				"# Just content",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := generateRuleFile(tt.cfg, tt.body)
			content := string(result)

			for _, s := range tt.contains {
				if !strings.Contains(content, s) {
					t.Errorf("generated content missing %q\nGot:\n%s", s, content)
				}
			}

			for _, s := range tt.notContains {
				if strings.Contains(content, s) {
					t.Errorf("generated content should not contain %q\nGot:\n%s", s, content)
				}
			}
		})
	}
}

func TestRuleCapabilities(t *testing.T) {
	caps := RuleCapabilities()

	if caps.ClientName != "claude-code" {
		t.Errorf("ClientName = %q, want %q", caps.ClientName, "claude-code")
	}

	if caps.RulesDirectory != ".claude/rules" {
		t.Errorf("RulesDirectory = %q, want %q", caps.RulesDirectory, ".claude/rules")
	}

	if caps.FileExtension != ".md" {
		t.Errorf("FileExtension = %q, want %q", caps.FileExtension, ".md")
	}

	if len(caps.InstructionFiles) != 2 {
		t.Errorf("InstructionFiles = %v, want 2 items", caps.InstructionFiles)
	}

	if caps.MatchesPath == nil {
		t.Error("MatchesPath is nil")
	}

	if caps.MatchesContent == nil {
		t.Error("MatchesContent is nil")
	}

	if caps.ParseRuleFile == nil {
		t.Error("ParseRuleFile is nil")
	}

	if caps.GenerateRuleFile == nil {
		t.Error("GenerateRuleFile is nil")
	}
}
