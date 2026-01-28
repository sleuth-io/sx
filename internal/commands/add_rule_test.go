package commands

import (
	"testing"

	"github.com/sleuth-io/sx/internal/utils"
)

func TestParseMarkdownSections(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected []utils.MarkdownSection
	}{
		{
			name: "single section",
			content: `## Coding Standards

Follow these coding standards.`,
			expected: []utils.MarkdownSection{
				{Heading: "Coding Standards", Content: "Follow these coding standards.", Level: 2},
			},
		},
		{
			name: "multiple sections",
			content: `## First Section

First content.

## Second Section

Second content.`,
			expected: []utils.MarkdownSection{
				{Heading: "First Section", Content: "First content.", Level: 2},
				{Heading: "Second Section", Content: "Second content.", Level: 2},
			},
		},
		{
			name: "section with sub-headings",
			content: `## Main Section

Intro text.

### Sub Section

Sub content.

### Another Sub

More content.`,
			expected: []utils.MarkdownSection{
				{
					Heading: "Main Section",
					Content: "Intro text.\n\n### Sub Section\n\nSub content.\n\n### Another Sub\n\nMore content.",
					Level:   2,
				},
			},
		},
		{
			name: "content before first section is ignored",
			content: `# Title

Some intro text.

## First Section

The content here.`,
			expected: []utils.MarkdownSection{
				{Heading: "First Section", Content: "The content here.", Level: 2},
			},
		},
		{
			name:     "empty content",
			content:  ``,
			expected: []utils.MarkdownSection{},
		},
		{
			name:     "no sections",
			content:  "Just some text without any headings.",
			expected: []utils.MarkdownSection{},
		},
		{
			name: "h3 only (no h2)",
			content: `### Not a top-level section

Content here.`,
			expected: []utils.MarkdownSection{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sections := utils.ParseMarkdownSections(tt.content)

			if len(sections) != len(tt.expected) {
				t.Fatalf("got %d sections, want %d", len(sections), len(tt.expected))
			}

			for i, got := range sections {
				want := tt.expected[i]
				if got.Heading != want.Heading {
					t.Errorf("section[%d].Heading = %q, want %q", i, got.Heading, want.Heading)
				}
				if got.Content != want.Content {
					t.Errorf("section[%d].Content = %q, want %q", i, got.Content, want.Content)
				}
				if got.Level != want.Level {
					t.Errorf("section[%d].Level = %d, want %d", i, got.Level, want.Level)
				}
			}
		})
	}
}

func TestIsImportableRuleFile(t *testing.T) {
	// Note: These tests depend on clients being registered.
	// In production, clients register in init(). For these tests,
	// we're testing the basic logic without mocking the registry.

	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{
			name:     "CLAUDE.md is importable",
			path:     "CLAUDE.md",
			expected: true,
		},
		{
			name:     "AGENTS.md is importable",
			path:     "AGENTS.md",
			expected: true,
		},
		{
			name:     "nested CLAUDE.md is importable",
			path:     "some/path/CLAUDE.md",
			expected: true,
		},
		{
			name:     "README.md is not importable",
			path:     "README.md",
			expected: false,
		},
		{
			name:     "random file is not importable",
			path:     "some-file.txt",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isImportableRuleFile(tt.path)
			if got != tt.expected {
				t.Errorf("isImportableRuleFile(%q) = %v, want %v", tt.path, got, tt.expected)
			}
		})
	}
}
