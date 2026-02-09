package handlers

import (
	"strings"
	"testing"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/metadata"
	"github.com/sleuth-io/sx/internal/utils"
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

func TestClaudeCodePluginHandler_IsMarketplaceSource(t *testing.T) {
	tests := []struct {
		name     string
		metadata *metadata.Metadata
		expected bool
	}{
		{
			name:     "nil ClaudeCodePlugin config",
			metadata: &metadata.Metadata{},
			expected: false,
		},
		{
			name: "empty source",
			metadata: &metadata.Metadata{
				ClaudeCodePlugin: &metadata.ClaudeCodePluginConfig{},
			},
			expected: false,
		},
		{
			name: "source = local",
			metadata: &metadata.Metadata{
				ClaudeCodePlugin: &metadata.ClaudeCodePluginConfig{
					Source: "local",
				},
			},
			expected: false,
		},
		{
			name: "source = marketplace",
			metadata: &metadata.Metadata{
				ClaudeCodePlugin: &metadata.ClaudeCodePluginConfig{
					Source: "marketplace",
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewClaudeCodePluginHandler(tt.metadata)
			result := handler.isMarketplaceSource()
			if result != tt.expected {
				t.Errorf("isMarketplaceSource() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestClaudeCodePluginHandler_Validate_MarketplaceSource(t *testing.T) {
	tests := []struct {
		name        string
		metadata    string
		extraFiles  map[string]string
		expectError bool
		errorSubstr string
	}{
		{
			name: "marketplace source with marketplace field is valid",
			metadata: `metadata-version = "1.0"

[asset]
name = "test-plugin"
version = "1.0.0"
type = "claude-code-plugin"

[claude-code-plugin]
marketplace = "my-market"
source = "marketplace"
`,
			expectError: false,
		},
		{
			name: "marketplace source without marketplace field is invalid",
			metadata: `metadata-version = "1.0"

[asset]
name = "test-plugin"
version = "1.0.0"
type = "claude-code-plugin"

[claude-code-plugin]
source = "marketplace"
`,
			expectError: true,
			errorSubstr: "marketplace source requires marketplace field",
		},
		{
			name: "local source without plugin.json is invalid",
			metadata: `metadata-version = "1.0"

[asset]
name = "test-plugin"
version = "1.0.0"
type = "claude-code-plugin"

[claude-code-plugin]
source = "local"
marketplace = "my-market"
`,
			expectError: true,
			errorSubstr: "plugin manifest not found",
		},
		{
			name: "local source with plugin.json is valid",
			metadata: `metadata-version = "1.0"

[asset]
name = "test-plugin"
version = "1.0.0"
type = "claude-code-plugin"

[claude-code-plugin]
manifest-file = ".claude-plugin/plugin.json"
source = "local"
marketplace = "my-market"
`,
			extraFiles: map[string]string{
				".claude-plugin/plugin.json": `{"name":"test-plugin"}`,
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewClaudeCodePluginHandler(&metadata.Metadata{})

			// Build zip with metadata.toml and any extra files
			zipData := createPluginTestZip(t, tt.metadata, tt.extraFiles)

			err := handler.Validate(zipData)
			if tt.expectError {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errorSubstr != "" && !strings.Contains(err.Error(), tt.errorSubstr) {
					t.Errorf("expected error to contain %q, got %q", tt.errorSubstr, err.Error())
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestClaudeCodePluginHandler_Remove_MarketplaceSource(t *testing.T) {
	// Test that marketplace source remove only unregisters+disables, no file deletion
	ctx := t.Context()
	targetBase := t.TempDir()

	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name:    "test-plugin",
			Version: "1.0.0",
			Type:    asset.TypeClaudeCodePlugin,
		},
		ClaudeCodePlugin: &metadata.ClaudeCodePluginConfig{
			Marketplace: "my-market",
			Source:      "marketplace",
		},
	}

	// Register the plugin first
	if err := RegisterPlugin(targetBase, "test-plugin", "my-market", "1.0.0", "/some/marketplace/path"); err != nil {
		t.Fatalf("failed to register: %v", err)
	}
	if err := EnablePlugin(targetBase, "test-plugin", "my-market", "/some/marketplace/path"); err != nil {
		t.Fatalf("failed to enable: %v", err)
	}

	// Verify registered
	if !IsPluginRegistered(targetBase, "test-plugin", "my-market") {
		t.Fatal("expected plugin to be registered before remove")
	}

	handler := NewClaudeCodePluginHandler(meta)
	if err := handler.Remove(ctx, targetBase); err != nil {
		t.Fatalf("Remove() failed: %v", err)
	}

	// Verify unregistered
	if IsPluginRegistered(targetBase, "test-plugin", "my-market") {
		t.Error("expected plugin to be unregistered after remove")
	}
}

// createPluginTestZip creates a zip with metadata.toml and optional extra files
func createPluginTestZip(t *testing.T, metadataContent string, extraFiles map[string]string) []byte {
	t.Helper()
	// Start with metadata.toml
	zipData, err := utils.CreateZipFromContent("metadata.toml", []byte(metadataContent))
	if err != nil {
		t.Fatalf("failed to create zip: %v", err)
	}

	// Add extra files
	for name, content := range extraFiles {
		zipData, err = utils.AddFileToZip(zipData, name, []byte(content))
		if err != nil {
			t.Fatalf("failed to add %s to zip: %v", name, err)
		}
	}

	return zipData
}

// Helper function to create bool pointer
func boolPtr(b bool) *bool {
	return &b
}
