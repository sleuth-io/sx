package utils

import "testing"

func TestSlugify(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple lowercase",
			input:    "hello",
			expected: "hello",
		},
		{
			name:     "uppercase to lowercase",
			input:    "Hello World",
			expected: "hello-world",
		},
		{
			name:     "spaces to hyphens",
			input:    "my asset name",
			expected: "my-asset-name",
		},
		{
			name:     "underscores to hyphens",
			input:    "my_asset_name",
			expected: "my-asset-name",
		},
		{
			name:     "removes special characters",
			input:    "Hello! World? @#$%",
			expected: "hello-world",
		},
		{
			name:     "collapses multiple hyphens",
			input:    "hello---world",
			expected: "hello-world",
		},
		{
			name:     "trims leading hyphens",
			input:    "---hello",
			expected: "hello",
		},
		{
			name:     "trims trailing hyphens",
			input:    "hello---",
			expected: "hello",
		},
		{
			name:     "preserves numbers",
			input:    "version 2.0",
			expected: "version-20",
		},
		{
			name:     "complex example",
			input:    "Go Coding Standards (v2)",
			expected: "go-coding-standards-v2",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "only special characters",
			input:    "@#$%^&*()",
			expected: "",
		},
		{
			name:     "mixed case with numbers",
			input:    "API v2 Guidelines",
			expected: "api-v2-guidelines",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Slugify(tt.input)
			if got != tt.expected {
				t.Errorf("Slugify(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}
