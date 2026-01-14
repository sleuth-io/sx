package detectors

import (
	"testing"

	"github.com/sleuth-io/sx/internal/asset"
)

func TestClaudeCodePluginDetector_DetectType(t *testing.T) {
	detector := &ClaudeCodePluginDetector{}

	tests := []struct {
		name     string
		files    []string
		expected bool
	}{
		{
			name:     "detects plugin with manifest",
			files:    []string{".claude-plugin/plugin.json", "README.md"},
			expected: true,
		},
		{
			name:     "detects plugin manifest only",
			files:    []string{".claude-plugin/plugin.json"},
			expected: true,
		},
		{
			name:     "does not detect without manifest",
			files:    []string{"SKILL.md", "README.md"},
			expected: false,
		},
		{
			name:     "does not detect empty file list",
			files:    []string{},
			expected: false,
		},
		{
			name:     "does not detect similar but wrong path",
			files:    []string{"plugin.json", ".claude/plugin.json"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detector.DetectType(tt.files)
			if result != tt.expected {
				t.Errorf("DetectType() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestClaudeCodePluginDetector_GetType(t *testing.T) {
	detector := &ClaudeCodePluginDetector{}
	expected := "claude-code-plugin"

	result := detector.GetType()
	if result != expected {
		t.Errorf("GetType() = %v, expected %v", result, expected)
	}
}

func TestClaudeCodePluginDetector_CreateDefaultMetadata(t *testing.T) {
	detector := &ClaudeCodePluginDetector{}

	name := "test-plugin"
	version := "1.0.0"

	meta := detector.CreateDefaultMetadata(name, version)

	if meta.Asset.Name != name {
		t.Errorf("Expected name %s, got %s", name, meta.Asset.Name)
	}

	if meta.Asset.Version != version {
		t.Errorf("Expected version %s, got %s", version, meta.Asset.Version)
	}

	if meta.Asset.Type != asset.TypeClaudeCodePlugin {
		t.Errorf("Expected type %s, got %s", asset.TypeClaudeCodePlugin.Key, meta.Asset.Type.Key)
	}

	if meta.ClaudeCodePlugin == nil {
		t.Fatal("Expected ClaudeCodePlugin config to be set")
	}

	expectedManifest := ".claude-plugin/plugin.json"
	if meta.ClaudeCodePlugin.ManifestFile != expectedManifest {
		t.Errorf("Expected manifest file %s, got %s", expectedManifest, meta.ClaudeCodePlugin.ManifestFile)
	}
}

func TestClaudeCodePluginDetector_DetectUsageFromToolCall(t *testing.T) {
	detector := &ClaudeCodePluginDetector{}

	// Plugins are not directly invoked, so this should always return empty/false
	tests := []struct {
		name      string
		toolName  string
		toolInput map[string]any
	}{
		{
			name:      "skill tool",
			toolName:  "Skill",
			toolInput: map[string]any{"skill": "my-plugin:some-skill"},
		},
		{
			name:      "other tool",
			toolName:  "Read",
			toolInput: map[string]any{"path": "/some/file"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, detected := detector.DetectUsageFromToolCall(tt.toolName, tt.toolInput)
			if detected {
				t.Errorf("DetectUsageFromToolCall() detected = true, expected false")
			}
			if name != "" {
				t.Errorf("DetectUsageFromToolCall() name = %s, expected empty", name)
			}
		})
	}
}
