package commands

import (
	"testing"

	"github.com/sleuth-io/sx/internal/asset"
)

func TestIsSingleFileAsset(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{"my-agent.md", true},
		{"my-agent.MD", true},
		{"path/to/agent.md", true},
		{"my-skill.zip", false},
		{"my-skill", false},
		{"README.md", true}, // Any .md file is considered
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			result := isSingleFileAsset(tc.path)
			if result != tc.expected {
				t.Errorf("isSingleFileAsset(%q) = %v, want %v", tc.path, result, tc.expected)
			}
		})
	}
}

func TestDetectSingleFileAssetType(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		content  string
		expected asset.Type
	}{
		{
			name:     "path contains agents",
			path:     "/home/user/.claude/agents/my-agent.md",
			content:  "Just some content",
			expected: asset.TypeAgent,
		},
		{
			name:     "path contains commands",
			path:     "/home/user/.claude/commands/my-command.md",
			content:  "Just some content",
			expected: asset.TypeCommand,
		},
		{
			name: "agent frontmatter with tools",
			path: "/some/path/file.md",
			content: `---
name: my-agent
description: An agent
tools: Read, Write
---

Agent prompt here.`,
			expected: asset.TypeAgent,
		},
		{
			name: "agent frontmatter with model",
			path: "/some/path/file.md",
			content: `---
name: my-agent
model: sonnet
---

Agent prompt here.`,
			expected: asset.TypeAgent,
		},
		{
			name: "command - no frontmatter",
			path: "/some/path/file.md",
			content: `# My Command

This is a slash command prompt.`,
			expected: asset.TypeCommand,
		},
		{
			name: "command - frontmatter without agent fields",
			path: "/some/path/file.md",
			content: `---
name: my-command
description: A command
---

Command prompt here.`,
			expected: asset.TypeCommand,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := detectSingleFileAssetType(tc.path, []byte(tc.content))
			if result != tc.expected {
				t.Errorf("detectSingleFileAssetType() = %v, want %v", result, tc.expected)
			}
		})
	}
}
