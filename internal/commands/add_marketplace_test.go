package commands

import (
	"testing"
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
			name:        "marketplace with forward slash",
			ref:         MarketplaceReference{PluginName: "plugin", Marketplace: "path/to/marketplace"},
			expectError: true,
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
