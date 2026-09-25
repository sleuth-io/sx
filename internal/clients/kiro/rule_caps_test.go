package kiro

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/sleuth-io/sx/v2/internal/asset"
	"github.com/sleuth-io/sx/v2/internal/clients"
	"github.com/sleuth-io/sx/v2/internal/metadata"
)

func TestMatchesPath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{
			name:     "matches kiro steering path",
			path:     ".kiro/steering/my-rule.md",
			expected: true,
		},
		{
			name:     "matches nested kiro steering path",
			path:     "backend/.kiro/steering/go-standards.md",
			expected: true,
		},
		{
			name:     "does not match cursor rules",
			path:     ".cursor/rules/my-rule.mdc",
			expected: false,
		},
		{
			name:     "does not match claude rules",
			path:     ".claude/rules/my-rule.md",
			expected: false,
		},
		{
			name:     "does not match non-.md file in kiro steering",
			path:     ".kiro/steering/my-rule.txt",
			expected: false,
		},
		{
			name:     "does not match kiro skills directory",
			path:     ".kiro/skills/my-skill.md",
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
			name: "matches content with inclusion",
			path: "rule.md",
			content: `---
inclusion: always
---

# Rule`,
			expected: true,
		},
		{
			name: "matches content with fileMatchPattern",
			path: "rule.md",
			content: `---
fileMatchPattern: "**/*.go"
---

# Rule`,
			expected: true,
		},
		{
			name: "matches content with both",
			path: "rule.md",
			content: `---
inclusion: fileMatch
fileMatchPattern:
  - "**/*.go"
  - "**/*.ts"
---

# Rule`,
			expected: true,
		},
		{
			name: "does not match content with globs (cursor format)",
			path: "rule.md",
			content: `---
globs:
  - "**/*.go"
---

# Rule`,
			expected: false,
		},
		{
			name: "does not match content with paths (claude format)",
			path: "rule.md",
			content: `---
paths:
  - "**/*.go"
---

# Rule`,
			expected: false,
		},
		{
			name:     "does not match plain md without kiro fields",
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
		inclusion       string
	}{
		{
			name: "parses full frontmatter with fileMatch",
			content: `---
description: "Go coding standards"
inclusion: fileMatch
fileMatchPattern:
  - "**/*.go"
  - "**/*.mod"
---

# Go Standards

Follow these rules.`,
			expectedGlobs:   []string{"**/*.go", "**/*.mod"},
			expectedDesc:    "Go coding standards",
			expectedContent: "# Go Standards\n\nFollow these rules.",
			inclusion:       "fileMatch",
		},
		{
			name: "parses always inclusion",
			content: `---
description: "Global rules"
inclusion: always
---

Content here.`,
			expectedDesc:    "Global rules",
			expectedContent: "Content here.",
			inclusion:       "always",
		},
		{
			name: "parses single fileMatchPattern",
			content: `---
inclusion: fileMatch
fileMatchPattern: "**/*.go"
---

Content here.`,
			expectedGlobs:   []string{"**/*.go"},
			expectedContent: "Content here.",
			inclusion:       "fileMatch",
		},
		{
			name:            "handles no frontmatter",
			content:         "# Just content\n\nNo frontmatter.",
			expectedGlobs:   nil,
			expectedContent: "# Just content\n\nNo frontmatter.",
		},
		{
			name: "parses manual inclusion with name",
			content: `---
inclusion: manual
name: my-rule
description: "A manual rule"
---

Content.`,
			expectedDesc:    "A manual rule",
			expectedContent: "Content.",
			inclusion:       "manual",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseRuleFile([]byte(tt.content))
			if err != nil {
				t.Fatalf("parseRuleFile() error = %v", err)
			}

			if result.ClientName != clients.ClientIDKiro {
				t.Errorf("ClientName = %q, want %q", result.ClientName, clients.ClientIDKiro)
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

			if tt.inclusion != "" {
				if val, ok := result.ClientFields["inclusion"].(string); !ok || val != tt.inclusion {
					t.Errorf("inclusion = %v, want %q", result.ClientFields["inclusion"], tt.inclusion)
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
				`description: "Go standards"`,
				"inclusion: fileMatch",
				`fileMatchPattern: "**/*.go"`,
				"# Go\n\nContent here.",
			},
			notContains: []string{
				"inclusion: always",
			},
		},
		{
			name: "generates with multiple globs",
			cfg: &metadata.RuleConfig{
				Globs: []string{"**/*.go", "**/*.mod"},
			},
			body: "Content.",
			contains: []string{
				"inclusion: fileMatch",
				"fileMatchPattern:",
				`"**/*.go"`,
				`"**/*.mod"`,
			},
			notContains: []string{
				"inclusion: always",
			},
		},
		{
			name: "generates always inclusion when no globs",
			cfg:  &metadata.RuleConfig{},
			body: "# Just content",
			contains: []string{
				"---\n",
				"inclusion: always",
			},
			notContains: []string{
				"fileMatchPattern",
				"inclusion: fileMatch",
			},
		},
		{
			name: "generates custom inclusion from kiro config",
			cfg: &metadata.RuleConfig{
				Kiro: map[string]any{
					"inclusion": "manual",
				},
			},
			body: "# Content",
			contains: []string{
				"inclusion: manual",
			},
		},
		{
			name: "nil config generates always inclusion",
			cfg:  nil,
			body: "# Just content",
			contains: []string{
				"---\n",
				"inclusion: always",
			},
			notContains: []string{
				"fileMatchPattern",
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

func TestGenerateRuleFileExactOutput(t *testing.T) {
	// Test exact output for specific cases
	tests := []struct {
		name     string
		cfg      *metadata.RuleConfig
		body     string
		expected string
	}{
		{
			name: "single glob exact output",
			cfg: &metadata.RuleConfig{
				Description: "Test rule",
				Globs:       []string{"**/*.go"},
			},
			body: "Content here.",
			expected: `---
description: "Test rule"
inclusion: fileMatch
fileMatchPattern: "**/*.go"
---

Content here.`,
		},
		{
			name: "always apply exact output",
			cfg: &metadata.RuleConfig{
				Description: "Global rule",
			},
			body: "Global content.",
			expected: `---
description: "Global rule"
inclusion: always
---

Global content.`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := generateRuleFile(tt.cfg, tt.body)
			got := string(result)

			if got != tt.expected {
				t.Errorf("generateRuleFile() mismatch\nGot:\n%s\n\nWant:\n%s", got, tt.expected)
			}
		})
	}
}

func TestRuleCapabilities(t *testing.T) {
	caps := RuleCapabilities()

	// Test all fields are set correctly
	tests := []struct {
		name     string
		got      string
		expected string
	}{
		{"ClientName", caps.ClientName, clients.ClientIDKiro},
		{"RulesDirectory", caps.RulesDirectory, ".kiro/steering"},
		{"FileExtension", caps.FileExtension, ".md"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.expected)
			}
		})
	}

	// Test InstructionFiles contains AGENTS.md
	expectedInstructionFiles := []string{"AGENTS.md"}
	if len(caps.InstructionFiles) != len(expectedInstructionFiles) {
		t.Errorf("InstructionFiles = %v, want %v", caps.InstructionFiles, expectedInstructionFiles)
	} else {
		for i, f := range caps.InstructionFiles {
			if f != expectedInstructionFiles[i] {
				t.Errorf("InstructionFiles[%d] = %q, want %q", i, f, expectedInstructionFiles[i])
			}
		}
	}

	// Test function pointers are not nil
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
	if caps.DetectAssetType == nil {
		t.Error("DetectAssetType is nil")
	}
}

func TestDetectAssetType(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected *asset.Type
	}{
		{
			name:     "detects .kiro/steering/ with .md as rule",
			path:     ".kiro/steering/my-rule.md",
			expected: &asset.TypeRule,
		},
		{
			name:     "detects nested .kiro/steering/ as rule",
			path:     "project/.kiro/steering/go-standards.md",
			expected: &asset.TypeRule,
		},
		{
			name:     "detects .kiro/skills/ as skill",
			path:     ".kiro/skills/my-skill/SKILL.md",
			expected: &asset.TypeSkill,
		},
		{
			name:     "detects nested .kiro/skills/ as skill",
			path:     "project/.kiro/skills/code-review/metadata.toml",
			expected: &asset.TypeSkill,
		},
		{
			name:     "does not detect non-.kiro paths",
			path:     "foo/steering/my-rule.md",
			expected: nil,
		},
		{
			name:     "does not detect .cursor paths",
			path:     ".cursor/rules/my-rule.mdc",
			expected: nil,
		},
		{
			name:     "does not detect .claude paths",
			path:     ".claude/rules/my-rule.md",
			expected: nil,
		},
		{
			name:     "does not detect .kiro/settings paths",
			path:     ".kiro/settings/mcp.json",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectAssetType(tt.path, nil)
			if tt.expected == nil {
				if got != nil {
					t.Errorf("detectAssetType(%q) = %v, want nil", tt.path, got)
				}
			} else {
				if got == nil {
					t.Errorf("detectAssetType(%q) = nil, want %v", tt.path, *tt.expected)
				} else if *got != *tt.expected {
					t.Errorf("detectAssetType(%q) = %v, want %v", tt.path, *got, *tt.expected)
				}
			}
		})
	}
}

// TestDetectAssetTypeRelocatedHome covers a Kiro config root that is not named
// .kiro. KIRO_HOME *is* the config root, so KIRO_HOME=/opt/profile holds skills
// at /opt/profile/skills and steering at /opt/profile/steering, with no .kiro
// segment anywhere — against the repo-relative literals alone every such path
// was classified as "not a Kiro asset". Hermetic: t.TempDir()/t.Setenv only.
func TestDetectAssetTypeRelocatedHome(t *testing.T) {
	t.Run("skills under a relocated root are detected", func(t *testing.T) {
		kiroHome := t.TempDir()
		t.Setenv("KIRO_HOME", kiroHome)

		path := filepath.Join(kiroHome, "skills", "my-skill", "SKILL.md")
		got := detectAssetType(path, nil)

		if got == nil || *got != asset.TypeSkill {
			t.Errorf("detectAssetType(%q) = %v, want %v", path, got, asset.TypeSkill)
		}
	})

	t.Run("steering under a relocated root is detected", func(t *testing.T) {
		kiroHome := t.TempDir()
		t.Setenv("KIRO_HOME", kiroHome)

		path := filepath.Join(kiroHome, "steering", "my-rule.md")
		got := detectAssetType(path, nil)

		if got == nil || *got != asset.TypeRule {
			t.Errorf("detectAssetType(%q) = %v, want %v", path, got, asset.TypeRule)
		}
	})

	t.Run("matchesPath accepts steering under a relocated root", func(t *testing.T) {
		kiroHome := t.TempDir()
		t.Setenv("KIRO_HOME", kiroHome)

		path := filepath.Join(kiroHome, "steering", "my-rule.md")
		if !matchesPath(path) {
			t.Errorf("matchesPath(%q) = false, want true", path)
		}

		notMarkdown := filepath.Join(kiroHome, "steering", "my-rule.txt")
		if matchesPath(notMarkdown) {
			t.Errorf("matchesPath(%q) = true, want false", notMarkdown)
		}
	})

	t.Run("repo-relative paths still work while KIRO_HOME is set", func(t *testing.T) {
		t.Setenv("KIRO_HOME", t.TempDir())

		got := detectAssetType(".kiro/steering/my-rule.md", nil)
		if got == nil || *got != asset.TypeRule {
			t.Errorf("detectAssetType(.kiro/steering/my-rule.md) = %v, want %v", got, asset.TypeRule)
		}

		if !matchesPath(".kiro/steering/my-rule.md") {
			t.Error("matchesPath(.kiro/steering/my-rule.md) = false, want true")
		}
	})

	t.Run("another client's skills directory is not claimed", func(t *testing.T) {
		// clients.DetectAssetType takes the first client that claims a path, so
		// Kiro must stay scoped to its own roots rather than keying on a bare
		// "skills/" segment shared by every harness.
		t.Setenv("KIRO_HOME", t.TempDir())

		for _, path := range []string{
			".claude/skills/my-skill/SKILL.md",
			".copilot/skills/my-skill/SKILL.md",
			"foo/steering/my-rule.md",
		} {
			if got := detectAssetType(path, nil); got != nil {
				t.Errorf("detectAssetType(%q) = %v, want nil", path, *got)
			}
		}
	})
}

func TestExtractYAMLFrontmatter(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		expectFM    bool
		expectError bool
		fmKeys      []string
		bodyPrefix  string
	}{
		{
			name: "extracts valid frontmatter",
			content: `---
key: value
---

Body content.`,
			expectFM:   true,
			fmKeys:     []string{"key"},
			bodyPrefix: "Body content.",
		},
		{
			name:        "returns error for no frontmatter",
			content:     "No frontmatter here.",
			expectError: true,
		},
		{
			name: "returns error for unclosed frontmatter",
			content: `---
key: value
no closing`,
			expectError: true,
		},
		{
			name:       "handles CRLF line endings",
			content:    "---\r\nkey: value\r\n---\r\n\r\nBody.",
			expectFM:   true,
			fmKeys:     []string{"key"},
			bodyPrefix: "Body.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, body, err := extractYAMLFrontmatter([]byte(tt.content))

			if tt.expectError {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.expectFM {
				if fm == nil {
					t.Error("expected frontmatter, got nil")
				}
				for _, key := range tt.fmKeys {
					if _, ok := fm[key]; !ok {
						t.Errorf("frontmatter missing key %q", key)
					}
				}
			}

			if tt.bodyPrefix != "" && !strings.HasPrefix(strings.TrimSpace(body), tt.bodyPrefix) {
				t.Errorf("body = %q, want prefix %q", body, tt.bodyPrefix)
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
			name:     "converts string slice",
			input:    []string{"a", "b", "c"},
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "converts any slice",
			input:    []any{"a", "b", "c"},
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "converts single string",
			input:    "single",
			expected: []string{"single"},
		},
		{
			name:     "returns nil for nil input",
			input:    nil,
			expected: nil,
		},
		{
			name:     "returns nil for unsupported type",
			input:    123,
			expected: nil,
		},
		{
			name:     "filters non-strings from any slice",
			input:    []any{"a", 123, "b"},
			expected: []string{"a", "b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toStringSlice(tt.input)

			if tt.expected == nil {
				if got != nil {
					t.Errorf("toStringSlice() = %v, want nil", got)
				}
				return
			}

			if len(got) != len(tt.expected) {
				t.Errorf("toStringSlice() = %v, want %v", got, tt.expected)
				return
			}

			for i, v := range got {
				if v != tt.expected[i] {
					t.Errorf("toStringSlice()[%d] = %q, want %q", i, v, tt.expected[i])
				}
			}
		})
	}
}

func TestClientIDConstant(t *testing.T) {
	// Verify the client uses the constant from clients package
	client := NewClient()
	if client.ID() != clients.ClientIDKiro {
		t.Errorf("client.ID() = %q, want %q", client.ID(), clients.ClientIDKiro)
	}
}
