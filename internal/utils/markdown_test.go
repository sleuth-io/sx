package utils

import (
	"testing"
)

func TestParseMarkdownSections(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected []MarkdownSection
	}{
		{
			name: "single section",
			content: `## Coding Standards

Follow these coding standards.`,
			expected: []MarkdownSection{
				{Heading: "Coding Standards", Content: "Follow these coding standards.", Level: 2},
			},
		},
		{
			name: "multiple sections",
			content: `## First Section

First content.

## Second Section

Second content.`,
			expected: []MarkdownSection{
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
			expected: []MarkdownSection{
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

Content here.`,
			expected: []MarkdownSection{
				{Heading: "First Section", Content: "Content here.", Level: 2},
			},
		},
		{
			name:     "empty content",
			content:  ``,
			expected: []MarkdownSection{},
		},
		{
			name:     "no sections",
			content:  "Just some text without any headings.",
			expected: []MarkdownSection{},
		},
		{
			name: "h3 only (no h2)",
			content: `### Not a top-level section

Content here.`,
			expected: []MarkdownSection{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sections := ParseMarkdownSections(tt.content)

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

func TestRemoveMarkdownSections(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		remove   []string
		expected string
	}{
		{
			name: "remove single section",
			content: `# Title

Intro.

## Keep This

Keep content.

## Remove This

Remove content.

## Also Keep

More content.
`,
			remove: []string{"Remove This"},
			expected: `# Title

Intro.

## Keep This

Keep content.

## Also Keep

More content.
`,
		},
		{
			name: "remove multiple sections",
			content: `## First

Content 1.

## Second

Content 2.

## Third

Content 3.
`,
			remove: []string{"First", "Third"},
			expected: `## Second

Content 2.
`,
		},
		{
			name: "remove section with sub-headings",
			content: `## Staying

This stays.

## Removing

This goes away.

### Sub heading

Sub content.

## Another Staying

This also stays.
`,
			remove: []string{"Removing"},
			expected: `## Staying

This stays.

## Another Staying

This also stays.
`,
		},
		{
			name: "remove non-existent section",
			content: `## Section

Content.
`,
			remove: []string{"Non Existent"},
			expected: `## Section

Content.
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RemoveMarkdownSections(tt.content, tt.remove)
			if got != tt.expected {
				t.Errorf("RemoveMarkdownSections() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestCleanupMarkdownSpacing(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected string
	}{
		{
			name:     "no change needed",
			content:  "Line 1\n\nLine 2\n",
			expected: "Line 1\n\nLine 2\n",
		},
		{
			name:     "three newlines to two",
			content:  "Line 1\n\n\nLine 2",
			expected: "Line 1\n\nLine 2\n",
		},
		{
			name:     "many newlines to two",
			content:  "Line 1\n\n\n\n\nLine 2",
			expected: "Line 1\n\nLine 2\n",
		},
		{
			name:     "trim trailing space",
			content:  "Content\n\n\n",
			expected: "Content\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CleanupMarkdownSpacing(tt.content)
			if got != tt.expected {
				t.Errorf("CleanupMarkdownSpacing() = %q, want %q", got, tt.expected)
			}
		})
	}
}
