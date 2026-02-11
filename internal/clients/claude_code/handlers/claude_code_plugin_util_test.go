package handlers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveMarketplacePluginPathFromFile(t *testing.T) {
	// Set up a fake marketplace
	tmpDir := t.TempDir()
	marketplaceDir := filepath.Join(tmpDir, "marketplace")
	pluginDir := filepath.Join(marketplaceDir, "plugins", "test-plugin", ".claude-plugin")
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatalf("failed to create plugin dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(`{}`), 0644); err != nil {
		t.Fatalf("failed to write plugin.json: %v", err)
	}

	// Write known_marketplaces.json
	knownMarketsPath := filepath.Join(tmpDir, "known_marketplaces.json")
	markets := map[string]any{
		"my-market": map[string]any{
			"installLocation": marketplaceDir,
		},
	}
	data, _ := json.Marshal(markets)
	if err := os.WriteFile(knownMarketsPath, data, 0644); err != nil {
		t.Fatalf("failed to write known_marketplaces.json: %v", err)
	}

	t.Run("found plugin", func(t *testing.T) {
		path, err := ResolveMarketplacePluginPathFromFile(knownMarketsPath, "my-market", "test-plugin")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := filepath.Join(marketplaceDir, "plugins", "test-plugin")
		if path != expected {
			t.Errorf("expected %q, got %q", expected, path)
		}
	})

	t.Run("marketplace not found", func(t *testing.T) {
		_, err := ResolveMarketplacePluginPathFromFile(knownMarketsPath, "nonexistent", "test-plugin")
		if err == nil {
			t.Fatal("expected error for nonexistent marketplace")
		}
	})

	t.Run("plugin not found", func(t *testing.T) {
		_, err := ResolveMarketplacePluginPathFromFile(knownMarketsPath, "my-market", "nonexistent")
		if err == nil {
			t.Fatal("expected error for nonexistent plugin")
		}
	})
}

func TestResolveMarketplaceNameFromFile(t *testing.T) {
	tmpDir := t.TempDir()
	knownMarketsPath := filepath.Join(tmpDir, "known_marketplaces.json")

	// Set up known_marketplaces.json with realistic entries
	markets := map[string]any{
		"claude-code-plugins": map[string]any{
			"source": map[string]any{
				"source": "github",
				"repo":   "anthropics/claude-code",
			},
			"installLocation": "/home/user/.claude/plugins/marketplaces/claude-code-plugins",
		},
		"anthropic-agent-skills": map[string]any{
			"source": map[string]any{
				"source": "github",
				"repo":   "anthropics/skills",
			},
			"installLocation": "/home/user/.claude/plugins/marketplaces/anthropic-agent-skills",
		},
		"my-local-market": map[string]any{
			"source": map[string]any{
				"source": "directory",
				"path":   "/some/local/path",
			},
			"installLocation": "/some/local/path",
		},
	}
	data, _ := json.Marshal(markets)
	if err := os.WriteFile(knownMarketsPath, data, 0644); err != nil {
		t.Fatalf("failed to write known_marketplaces.json: %v", err)
	}

	tests := []struct {
		name        string
		identifier  string
		expected    string
		expectError bool
	}{
		// Direct key match
		{
			name:       "plain name direct match",
			identifier: "claude-code-plugins",
			expected:   "claude-code-plugins",
		},
		{
			name:       "local market direct match",
			identifier: "my-local-market",
			expected:   "my-local-market",
		},

		// org/repo format → search by source.repo
		{
			name:       "org/repo resolves to marketplace name",
			identifier: "anthropics/claude-code",
			expected:   "claude-code-plugins",
		},
		{
			name:       "org/repo for skills",
			identifier: "anthropics/skills",
			expected:   "anthropic-agent-skills",
		},

		// HTTPS git URLs → extract org/repo → search by source.repo
		{
			name:       "HTTPS git URL",
			identifier: "https://github.com/anthropics/claude-code.git",
			expected:   "claude-code-plugins",
		},
		{
			name:       "HTTPS URL without .git suffix",
			identifier: "https://github.com/anthropics/claude-code",
			expected:   "claude-code-plugins",
		},
		{
			name:       "HTTPS URL with fragment",
			identifier: "https://github.com/anthropics/skills.git#main",
			expected:   "anthropic-agent-skills",
		},

		// SSH git URLs → extract org/repo → search by source.repo
		{
			name:       "SSH git URL",
			identifier: "git@github.com:anthropics/claude-code.git",
			expected:   "claude-code-plugins",
		},
		{
			name:       "SSH URL without .git suffix",
			identifier: "git@github.com:anthropics/skills",
			expected:   "anthropic-agent-skills",
		},

		// Not found
		{
			name:        "nonexistent plain name",
			identifier:  "nonexistent",
			expectError: true,
		},
		{
			name:        "nonexistent org/repo",
			identifier:  "unknown/repo",
			expectError: true,
		},
		{
			name:        "nonexistent git URL",
			identifier:  "https://github.com/unknown/repo.git",
			expectError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := ResolveMarketplaceNameFromFile(knownMarketsPath, tc.identifier)
			if tc.expectError {
				if err == nil {
					t.Errorf("ResolveMarketplaceNameFromFile(%q) expected error, got %q", tc.identifier, result)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveMarketplaceNameFromFile(%q) unexpected error: %v", tc.identifier, err)
			}
			if result != tc.expected {
				t.Errorf("ResolveMarketplaceNameFromFile(%q) = %q, want %q", tc.identifier, result, tc.expected)
			}
		})
	}
}

func TestIsPluginRegistered(t *testing.T) {
	targetBase := t.TempDir()

	// Before registration
	if IsPluginRegistered(targetBase, "test-plugin", "my-market") {
		t.Error("expected false before registration")
	}

	// Register
	if err := RegisterPlugin(targetBase, "test-plugin", "my-market", "1.0.0", "/path/to/plugin"); err != nil {
		t.Fatalf("failed to register: %v", err)
	}

	// After registration
	if !IsPluginRegistered(targetBase, "test-plugin", "my-market") {
		t.Error("expected true after registration")
	}

	// Different marketplace
	if IsPluginRegistered(targetBase, "test-plugin", "other-market") {
		t.Error("expected false for different marketplace")
	}

	// Unregister
	if err := UnregisterPlugin(targetBase, "test-plugin", "my-market"); err != nil {
		t.Fatalf("failed to unregister: %v", err)
	}

	// After unregistration
	if IsPluginRegistered(targetBase, "test-plugin", "my-market") {
		t.Error("expected false after unregistration")
	}
}
