package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/assets/detectors"
	"github.com/sleuth-io/sx/internal/metadata"
	"github.com/sleuth-io/sx/internal/utils"
)

func TestIsSingleFileAsset(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{"my-agent.md", true},
		{"my-agent.MD", true},
		{"path/to/agent.md", true},
		{"path/to/.codex/agents/security-reviewer.toml", true},
		{"path/to/agents/security-reviewer.toml", false},
		{"path/to/config.toml", false},
		{"my-skill.zip", false},
		{"my-skill", false},
		{"README.md", true}, // Any .md file is considered via fallback
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

func TestDetectAssetTypeFromPath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		content  string
		expected asset.Type
	}{
		{
			name:     "path contains agents",
			path:     "/home/user/agents/my-agent.md",
			content:  "Just some content",
			expected: asset.TypeAgent,
		},
		{
			name:     "path contains commands",
			path:     "/home/user/commands/my-command.md",
			content:  "Just some content",
			expected: asset.TypeCommand,
		},
		{
			name:     "path contains rules",
			path:     "/home/user/rules/my-rule.md",
			content:  "Just some content",
			expected: asset.TypeRule,
		},
		{
			name:     "path contains skills",
			path:     "/home/user/skills/my-skill.md",
			content:  "Just some content",
			expected: asset.TypeSkill,
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
			result := detectors.DetectAssetTypeFromPath(tc.path, []byte(tc.content))
			if result == nil {
				t.Fatalf("DetectAssetTypeFromPath() returned nil, want %v", tc.expected)
			}
			if *result != tc.expected {
				t.Errorf("DetectAssetTypeFromPath() = %v, want %v", *result, tc.expected)
			}
		})
	}
}

func TestDetectAssetTypeFromPath_IgnoresTOML(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		content string
	}{
		{
			name:    "codex agent toml belongs to codex client detection",
			path:    "/home/user/.codex/agents/security-reviewer.toml",
			content: `name = "security_reviewer"`,
		},
		{
			name: "content-shaped toml is not generic",
			path: "/some/path/security-reviewer.toml",
			content: `name = "security_reviewer"
description = "Security reviewer"
developer_instructions = "Review security risks."
`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := detectors.DetectAssetTypeFromPath(tc.path, []byte(tc.content)); got != nil {
				t.Fatalf("DetectAssetTypeFromPath() = %v, want nil", got)
			}
		})
	}
}

func TestCreateZipFromSingleFile(t *testing.T) {
	tests := []struct {
		name              string
		filename          string
		dirPath           string // path hint for type detection (e.g., "agents" or "commands")
		content           string
		expectedType      asset.Type
		expectedPrompt    string
		expectedAssetName string
	}{
		{
			name:              "agent file keeps original name",
			filename:          "my-agent.md",
			dirPath:           "agents",
			content:           "Agent prompt content",
			expectedType:      asset.TypeAgent,
			expectedPrompt:    "my-agent.md",
			expectedAssetName: "my-agent",
		},
		{
			name:              "command file keeps original name",
			filename:          "review-pr.md",
			dirPath:           "commands",
			content:           "Command prompt content",
			expectedType:      asset.TypeCommand,
			expectedPrompt:    "review-pr.md",
			expectedAssetName: "review-pr",
		},
		{
			name:     "codex agent toml uses name from file",
			filename: "security-reviewer.toml",
			dirPath:  ".codex/agents",
			content: `name = "security_reviewer"
description = "Security reviewer"
developer_instructions = "Review security risks."
model = "gpt-5.4"
`,
			expectedType:      asset.TypeAgent,
			expectedPrompt:    "security_reviewer.toml",
			expectedAssetName: "security_reviewer",
		},
		{
			name:     "agent detected from frontmatter",
			filename: "custom-name.md",
			dirPath:  "other",
			content: `---
tools: Read, Write
---
Agent with tools`,
			expectedType:      asset.TypeAgent,
			expectedPrompt:    "custom-name.md",
			expectedAssetName: "custom-name",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Create temp directory structure
			tmpDir := t.TempDir()
			assetDir := filepath.Join(tmpDir, tc.dirPath)
			if err := os.MkdirAll(assetDir, 0755); err != nil {
				t.Fatalf("Failed to create dir: %v", err)
			}

			// Write test file
			filePath := filepath.Join(assetDir, tc.filename)
			if err := os.WriteFile(filePath, []byte(tc.content), 0644); err != nil {
				t.Fatalf("Failed to write file: %v", err)
			}

			// Create zip
			zipData, err := createZipFromSingleFile(filePath)
			if err != nil {
				t.Fatalf("createZipFromSingleFile failed: %v", err)
			}

			// Verify original filename is in zip
			files, err := utils.ListZipFiles(zipData)
			if err != nil {
				t.Fatalf("Failed to list zip files: %v", err)
			}

			foundPromptFile := false
			foundMetadata := false
			for _, f := range files {
				if f == tc.expectedPrompt {
					foundPromptFile = true
				}
				if f == "metadata.toml" {
					foundMetadata = true
				}
			}

			if !foundPromptFile {
				t.Errorf("Expected prompt file %q not found in zip. Files: %v", tc.expectedPrompt, files)
			}
			if !foundMetadata {
				t.Errorf("metadata.toml not found in zip. Files: %v", files)
			}

			// Read and verify metadata
			metaBytes, err := utils.ReadZipFile(zipData, "metadata.toml")
			if err != nil {
				t.Fatalf("Failed to read metadata.toml: %v", err)
			}

			meta, err := metadata.Parse(metaBytes)
			if err != nil {
				t.Fatalf("Failed to parse metadata: %v", err)
			}

			// Verify asset name
			if meta.Asset.Name != tc.expectedAssetName {
				t.Errorf("Asset name = %q, want %q", meta.Asset.Name, tc.expectedAssetName)
			}

			// Verify asset type
			if meta.Asset.Type != tc.expectedType {
				t.Errorf("Asset type = %v, want %v", meta.Asset.Type, tc.expectedType)
			}

			// Verify PromptFile in type-specific config
			if tc.expectedType == asset.TypeAgent {
				if meta.Agent == nil {
					t.Fatal("Agent config is nil")
				}
				if meta.Agent.PromptFile != tc.expectedPrompt {
					t.Errorf("Agent.PromptFile = %q, want %q", meta.Agent.PromptFile, tc.expectedPrompt)
				}
				if filepath.Ext(tc.expectedPrompt) == ".toml" {
					if meta.Asset.Description != "Security reviewer" {
						t.Errorf("Asset.Description = %q, want %q", meta.Asset.Description, "Security reviewer")
					}
					if len(meta.Asset.Clients) != 1 || meta.Asset.Clients[0] != "codex" {
						t.Errorf("Asset.Clients = %v, want [codex]", meta.Asset.Clients)
					}
				}
			} else {
				if meta.Command == nil {
					t.Fatal("Command config is nil")
				}
				if meta.Command.PromptFile != tc.expectedPrompt {
					t.Errorf("Command.PromptFile = %q, want %q", meta.Command.PromptFile, tc.expectedPrompt)
				}
			}
		})
	}
}

func TestCreateZipFromSingleFile_CodexAgentTOMLMissingRequiredField(t *testing.T) {
	tmpDir := t.TempDir()
	agentsDir := filepath.Join(tmpDir, ".codex", "agents")
	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		t.Fatalf("Failed to create dir: %v", err)
	}

	filePath := filepath.Join(agentsDir, "security-reviewer.toml")
	if err := os.WriteFile(filePath, []byte(`name = "security_reviewer"`), 0644); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

	if _, err := createZipFromSingleFile(filePath); err == nil {
		t.Fatal("createZipFromSingleFile succeeded, want missing required field error")
	}
}

func TestCreateZipFromSingleFile_CodexAgentTOMLInvalidName(t *testing.T) {
	tests := []struct {
		name          string
		agentNameTOML string
	}{
		{name: "slash", agentNameTOML: `"security/reviewer"`},
		{name: "backslash", agentNameTOML: `'security\reviewer'`},
		{name: "dotdot", agentNameTOML: `"security..reviewer"`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			agentsDir := filepath.Join(tmpDir, ".codex", "agents")
			if err := os.MkdirAll(agentsDir, 0755); err != nil {
				t.Fatalf("Failed to create dir: %v", err)
			}

			filePath := filepath.Join(agentsDir, "security-reviewer.toml")
			content := []byte(`name = ` + tc.agentNameTOML + `
description = "Security reviewer"
developer_instructions = "Review security risks."
`)
			if err := os.WriteFile(filePath, content, 0644); err != nil {
				t.Fatalf("Failed to write file: %v", err)
			}

			if _, err := createZipFromSingleFile(filePath); err == nil {
				t.Fatal("createZipFromSingleFile succeeded, want invalid name error")
			}
		})
	}
}
