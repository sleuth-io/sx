package handlers

import (
	"testing"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/metadata"
)

func TestClaudeCodePluginHandler_DetectType(t *testing.T) {
	handler := NewClaudeCodePluginHandler(&metadata.Metadata{})

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
			name:     "does not detect without manifest",
			files:    []string{"SKILL.md", "README.md"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := handler.DetectType(tt.files)
			if result != tt.expected {
				t.Errorf("DetectType() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestClaudeCodePluginHandler_GetType(t *testing.T) {
	handler := NewClaudeCodePluginHandler(&metadata.Metadata{})
	expected := "claude-code-plugin"

	result := handler.GetType()
	if result != expected {
		t.Errorf("GetType() = %v, expected %v", result, expected)
	}
}

func TestClaudeCodePluginHandler_GetInstallPath(t *testing.T) {
	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name: "my-plugin",
		},
	}
	handler := NewClaudeCodePluginHandler(meta)

	expected := "plugins/my-plugin"
	result := handler.GetInstallPath()
	if result != expected {
		t.Errorf("GetInstallPath() = %v, expected %v", result, expected)
	}
}

func TestClaudeCodePluginHandler_CreateDefaultMetadata(t *testing.T) {
	handler := NewClaudeCodePluginHandler(&metadata.Metadata{})

	name := "test-plugin"
	version := "1.0.0"

	meta := handler.CreateDefaultMetadata(name, version)

	if meta.Asset.Name != name {
		t.Errorf("Expected name %s, got %s", name, meta.Asset.Name)
	}

	if meta.Asset.Version != version {
		t.Errorf("Expected version %s, got %s", version, meta.Asset.Version)
	}

	if meta.Asset.Type != asset.TypeClaudeCodePlugin {
		t.Errorf("Expected type %s, got %s", asset.TypeClaudeCodePlugin.Key, meta.Asset.Type.Key)
	}
}

func TestClaudeCodePluginHandler_ShouldAutoEnable(t *testing.T) {
	tests := []struct {
		name     string
		metadata *metadata.Metadata
		expected bool
	}{
		{
			name:     "nil ClaudeCodePlugin config",
			metadata: &metadata.Metadata{},
			expected: true,
		},
		{
			name: "nil AutoEnable",
			metadata: &metadata.Metadata{
				ClaudeCodePlugin: &metadata.ClaudeCodePluginConfig{},
			},
			expected: true,
		},
		{
			name: "AutoEnable true",
			metadata: &metadata.Metadata{
				ClaudeCodePlugin: &metadata.ClaudeCodePluginConfig{
					AutoEnable: boolPtr(true),
				},
			},
			expected: true,
		},
		{
			name: "AutoEnable false",
			metadata: &metadata.Metadata{
				ClaudeCodePlugin: &metadata.ClaudeCodePluginConfig{
					AutoEnable: boolPtr(false),
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewClaudeCodePluginHandler(tt.metadata)
			result := handler.shouldAutoEnable()
			if result != tt.expected {
				t.Errorf("shouldAutoEnable() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestClaudeCodePluginHandler_DetectUsageFromToolCall(t *testing.T) {
	handler := NewClaudeCodePluginHandler(&metadata.Metadata{})

	// Plugins are not directly invoked
	name, detected := handler.DetectUsageFromToolCall("Skill", map[string]any{"skill": "test"})
	if detected {
		t.Error("Expected detected = false for plugins")
	}
	if name != "" {
		t.Errorf("Expected empty name, got %s", name)
	}
}

func TestClaudeCodePluginHandler_CanDetectInstalledState(t *testing.T) {
	handler := NewClaudeCodePluginHandler(&metadata.Metadata{})

	if !handler.CanDetectInstalledState() {
		t.Error("Expected CanDetectInstalledState() = true")
	}
}

// Helper function to create bool pointer
func boolPtr(b bool) *bool {
	return &b
}
