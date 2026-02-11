package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sleuth-io/sx/internal/utils"
)

func TestIsMarketplaceReference(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		// Valid marketplace references
		{"plugin@marketplace", true},
		{"my-plugin@my-marketplace", true},
		{"plugin123@marketplace456", true},

		// Invalid: empty plugin name
		{"@marketplace", false},

		// Invalid: empty marketplace name
		{"plugin@", false},

		// Invalid: no @ sign
		{"plugin", false},
		{"marketplace", false},

		// Invalid: multiple @ signs (treated as valid, second @ is part of marketplace name)
		{"plugin@@marketplace", true},

		// Invalid: contains path separators in plugin name
		{"path/to/plugin@marketplace", false},
		{"path\\to\\plugin@marketplace", false},

		// Valid: path separators only in marketplace (validated separately)
		{"plugin@path/to/marketplace", true},

		// Invalid: URLs should not match
		{"https://github.com/user@domain.com/repo", false},

		// Invalid: "git" as plugin name rejected to avoid conflict with git SSH URLs
		{"git@github.com:user/repo", false},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := IsMarketplaceReference(tc.input)
			if result != tc.expected {
				t.Errorf("IsMarketplaceReference(%q) = %v, want %v", tc.input, result, tc.expected)
			}
		})
	}
}

func TestParseMarketplaceReference(t *testing.T) {
	tests := []struct {
		input               string
		expectedPlugin      string
		expectedMarketplace string
	}{
		{"plugin@marketplace", "plugin", "marketplace"},
		{"my-plugin@my-marketplace", "my-plugin", "my-marketplace"},
		{"plugin@", "plugin", ""},
		{"@marketplace", "", "marketplace"},
		{"plugin", "plugin", ""},
		{"plugin@market@place", "plugin", "market@place"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			ref := ParseMarketplaceReference(tc.input)
			if ref.PluginName != tc.expectedPlugin {
				t.Errorf("ParseMarketplaceReference(%q).PluginName = %q, want %q", tc.input, ref.PluginName, tc.expectedPlugin)
			}
			if ref.Marketplace != tc.expectedMarketplace {
				t.Errorf("ParseMarketplaceReference(%q).Marketplace = %q, want %q", tc.input, ref.Marketplace, tc.expectedMarketplace)
			}
		})
	}
}

func TestValidateMarketplaceReference(t *testing.T) {
	tests := []struct {
		name        string
		ref         MarketplaceReference
		expectError bool
	}{
		{
			name:        "valid reference",
			ref:         MarketplaceReference{PluginName: "plugin", Marketplace: "marketplace"},
			expectError: false,
		},
		{
			name:        "valid reference with hyphens",
			ref:         MarketplaceReference{PluginName: "my-plugin", Marketplace: "my-marketplace"},
			expectError: false,
		},
		{
			name:        "marketplace with path traversal",
			ref:         MarketplaceReference{PluginName: "plugin", Marketplace: "../evil"},
			expectError: true,
		},
		{
			name:        "marketplace with forward slash (org/repo format)",
			ref:         MarketplaceReference{PluginName: "plugin", Marketplace: "org/repo"},
			expectError: false,
		},
		{
			name:        "marketplace with backslash",
			ref:         MarketplaceReference{PluginName: "plugin", Marketplace: "path\\to\\marketplace"},
			expectError: true,
		},
		{
			name:        "plugin name with path traversal",
			ref:         MarketplaceReference{PluginName: "../evil", Marketplace: "marketplace"},
			expectError: true,
		},
		{
			name:        "empty marketplace is valid",
			ref:         MarketplaceReference{PluginName: "plugin", Marketplace: ""},
			expectError: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateMarketplaceReference(tc.ref)
			if tc.expectError && err == nil {
				t.Errorf("ValidateMarketplaceReference(%+v) expected error, got nil", tc.ref)
			}
			if !tc.expectError && err != nil {
				t.Errorf("ValidateMarketplaceReference(%+v) unexpected error: %v", tc.ref, err)
			}
		})
	}
}

func TestCreateConfigOnlyPluginZip(t *testing.T) {
	// Set up a fake marketplace plugin directory
	pluginDir := t.TempDir()
	claudePluginDir := filepath.Join(pluginDir, ".claude-plugin")
	if err := os.MkdirAll(claudePluginDir, 0755); err != nil {
		t.Fatalf("failed to create .claude-plugin dir: %v", err)
	}

	pluginJSON := `{
  "name": "test-plugin",
  "description": "A test plugin",
  "version": "2.0.0"
}`
	if err := os.WriteFile(filepath.Join(claudePluginDir, "plugin.json"), []byte(pluginJSON), 0644); err != nil {
		t.Fatalf("failed to write plugin.json: %v", err)
	}

	ref := MarketplaceReference{
		PluginName:  "test-plugin",
		Marketplace: "my-market",
	}

	zipData, err := createConfigOnlyPluginZip(pluginDir, ref)
	if err != nil {
		t.Fatalf("createConfigOnlyPluginZip() error: %v", err)
	}

	// Verify zip only contains metadata.toml
	files, err := utils.ListZipFiles(zipData)
	if err != nil {
		t.Fatalf("failed to list zip files: %v", err)
	}
	if len(files) != 1 {
		t.Errorf("expected 1 file in zip, got %d: %v", len(files), files)
	}
	if files[0] != "metadata.toml" {
		t.Errorf("expected metadata.toml, got %s", files[0])
	}

	// Read and verify metadata.toml content
	metadataBytes, err := utils.ReadZipFile(zipData, "metadata.toml")
	if err != nil {
		t.Fatalf("failed to read metadata.toml: %v", err)
	}

	content := string(metadataBytes)
	if !strings.Contains(content, `source = "marketplace"`) {
		t.Errorf("metadata should contain source = marketplace, got:\n%s", content)
	}
	if !strings.Contains(content, `marketplace = "my-market"`) {
		t.Errorf("metadata should contain marketplace = my-market, got:\n%s", content)
	}
	if !strings.Contains(content, `name = "test-plugin"`) {
		t.Errorf("metadata should contain name = test-plugin, got:\n%s", content)
	}
	if !strings.Contains(content, `version = "2.0.0"`) {
		t.Errorf("metadata should contain version = 2.0.0, got:\n%s", content)
	}
	if !strings.Contains(content, `type = "claude-code-plugin"`) {
		t.Errorf("metadata should contain type = claude-code-plugin, got:\n%s", content)
	}
}

func TestCreateConfigOnlyPluginZip_DefaultVersion(t *testing.T) {
	// Set up a fake marketplace plugin directory with no version
	pluginDir := t.TempDir()
	claudePluginDir := filepath.Join(pluginDir, ".claude-plugin")
	if err := os.MkdirAll(claudePluginDir, 0755); err != nil {
		t.Fatalf("failed to create .claude-plugin dir: %v", err)
	}

	pluginJSON := `{
  "name": "test-plugin",
  "description": "A test plugin"
}`
	if err := os.WriteFile(filepath.Join(claudePluginDir, "plugin.json"), []byte(pluginJSON), 0644); err != nil {
		t.Fatalf("failed to write plugin.json: %v", err)
	}

	ref := MarketplaceReference{
		PluginName:  "test-plugin",
		Marketplace: "my-market",
	}

	zipData, err := createConfigOnlyPluginZip(pluginDir, ref)
	if err != nil {
		t.Fatalf("createConfigOnlyPluginZip() error: %v", err)
	}

	metadataBytes, err := utils.ReadZipFile(zipData, "metadata.toml")
	if err != nil {
		t.Fatalf("failed to read metadata.toml: %v", err)
	}

	content := string(metadataBytes)
	if !strings.Contains(content, `version = "1.0.0"`) {
		t.Errorf("metadata should default to version 1.0.0, got:\n%s", content)
	}
}
