package cursor

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
			name:     "matches cursor rules path",
			path:     ".cursor/rules/my-rule.mdc",
			expected: true,
		},
		{
			name:     "matches nested cursor rules path",
			path:     "backend/.cursor/rules/go-standards.mdc",
			expected: true,
		},
		{
			name:     "does not match claude rules",
			path:     ".claude/rules/my-rule.md",
			expected: false,
		},
		{
			name:     "does not match .md file in cursor rules",
			path:     ".cursor/rules/my-rule.md",
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
			name: "matches content with globs",
			path: "rule.md",
			content: `---
globs:
  - **/*.go
---

# Rule`,
			expected: true,
		},
		{
			name:     "matches .mdc file even without globs",
			path:     "rule.mdc",
			content:  "# Just a heading\n\nSome content.",
			expected: true,
		},
		{
			name: "does not match content with paths (claude format)",
			path: "rule.md",
			content: `---
paths:
  - **/*.go
---

# Rule`,
			expected: false,
		},
		{
			name:     "does not match plain md without globs",
			path:     "rule.md",
			content:  "# Just content",
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
		alwaysApply     bool
	}{
		{
			name: "parses full frontmatter",
			content: `---
description: Go coding standards
globs:
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
			name: "parses alwaysApply",
			content: `---
description: Global rules
alwaysApply: true
---

Content here.`,
			expectedDesc:    "Global rules",
			expectedContent: "Content here.",
			alwaysApply:     true,
		},
		{
			name: "parses single glob",
			content: `---
globs:
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

			if result.ClientName != "cursor" {
				t.Errorf("ClientName = %q, want %q", result.ClientName, "cursor")
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

			if tt.alwaysApply {
				if val, ok := result.ClientFields["alwaysApply"].(bool); !ok || !val {
					t.Errorf("alwaysApply = %v, want true", result.ClientFields["alwaysApply"])
				}
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
				"globs: **/*.go",
				"# Go\n\nContent here.",
			},
			notContains: []string{
				"alwaysApply",
			},
		},
		{
			name: "generates with multiple globs",
			cfg: &metadata.RuleConfig{
				Globs: []string{"**/*.go", "**/*.mod"},
			},
			body: "Content.",
			contains: []string{
				"globs:",
				"  - **/*.go",
				"  - **/*.mod",
			},
			notContains: []string{
				"alwaysApply",
			},
		},
		{
			name: "generates alwaysApply when no globs",
			cfg:  &metadata.RuleConfig{},
			body: "# Just content",
			contains: []string{
				"---\n",
				"alwaysApply: true",
			},
			notContains: []string{
				"globs:",
			},
		},
		{
			name: "generates alwaysApply from cursor config",
			cfg: &metadata.RuleConfig{
				Cursor: map[string]any{
					"always-apply": true,
				},
			},
			body: "# Content",
			contains: []string{
				"alwaysApply: true",
			},
		},
		{
			name: "nil config generates alwaysApply",
			cfg:  nil,
			body: "# Just content",
			contains: []string{
				"---\n",
				"alwaysApply: true",
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

	if caps.ClientName != "cursor" {
		t.Errorf("ClientName = %q, want %q", caps.ClientName, "cursor")
	}

	if caps.RulesDirectory != ".cursor/rules" {
		t.Errorf("RulesDirectory = %q, want %q", caps.RulesDirectory, ".cursor/rules")
	}

	if caps.FileExtension != ".mdc" {
		t.Errorf("FileExtension = %q, want %q", caps.FileExtension, ".mdc")
	}

	if len(caps.InstructionFiles) != 0 {
		t.Errorf("InstructionFiles = %v, want empty", caps.InstructionFiles)
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
