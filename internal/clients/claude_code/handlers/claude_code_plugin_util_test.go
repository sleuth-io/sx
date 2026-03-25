package handlers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractRepoIdentifier(t *testing.T) {
	tests := []struct {
		name       string
		identifier string
		expected   string
	}{
		// HTTPS URLs
		{name: "HTTPS URL", identifier: "https://github.com/org/repo", expected: "org/repo"},
		{name: "HTTPS URL with .git", identifier: "https://github.com/org/repo.git", expected: "org/repo"},
		{name: "HTTPS URL with extra path segments", identifier: "https://github.com/anthropics/claude-plugins-official/tree/main/plugins/typescript-lsp", expected: "anthropics/claude-plugins-official"},
		{name: "HTTPS URL with fragment", identifier: "https://github.com/org/repo.git#main", expected: "org/repo"},
		// SSH URLs
		{name: "SSH URL", identifier: "git@github.com:org/repo.git", expected: "org/repo"},
		{name: "SSH URL without .git", identifier: "git@github.com:org/repo", expected: "org/repo"},
		// org/repo format
		{name: "org/repo", identifier: "anthropics/claude-code", expected: "anthropics/claude-code"},
		// Plain names
		{name: "plain name", identifier: "claude-plugins-official", expected: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := extractRepoIdentifier(tc.identifier)
			if result != tc.expected {
				t.Errorf("extractRepoIdentifier(%q) = %q, want %q", tc.identifier, result, tc.expected)
			}
		})
	}
}

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

	t.Run("found plugin in external_plugins", func(t *testing.T) {
		extPluginDir := filepath.Join(marketplaceDir, "external_plugins", "ext-plugin", ".claude-plugin")
		if err := os.MkdirAll(extPluginDir, 0755); err != nil {
			t.Fatalf("failed to create external plugin dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(extPluginDir, "plugin.json"), []byte(`{}`), 0644); err != nil {
			t.Fatalf("failed to write plugin.json: %v", err)
		}

		path, err := ResolveMarketplacePluginPathFromFile(knownMarketsPath, "my-market", "ext-plugin")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := filepath.Join(marketplaceDir, "external_plugins", "ext-plugin")
		if path != expected {
			t.Errorf("expected %q, got %q", expected, path)
		}
	})

	t.Run("found plugin without manifest (directory only)", func(t *testing.T) {
		// LSP-type plugins may not have .claude-plugin/plugin.json
		lspDir := filepath.Join(marketplaceDir, "plugins", "typescript-lsp")
		if err := os.MkdirAll(lspDir, 0755); err != nil {
			t.Fatalf("failed to create lsp dir: %v", err)
		}
		// Only a README, no .claude-plugin/plugin.json
		if err := os.WriteFile(filepath.Join(lspDir, "README.md"), []byte("# LSP Plugin"), 0644); err != nil {
			t.Fatalf("failed to write README: %v", err)
		}

		path, err := ResolveMarketplacePluginPathFromFile(knownMarketsPath, "my-market", "typescript-lsp")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := filepath.Join(marketplaceDir, "plugins", "typescript-lsp")
		if path != expected {
			t.Errorf("expected %q, got %q", expected, path)
		}
	})

	t.Run("found plugin via marketplace.json source", func(t *testing.T) {
		// Set up a marketplace where the repo root IS the plugin (source: "./")
		singlePluginMarketDir := filepath.Join(tmpDir, "single-plugin-market")
		claudePluginDir := filepath.Join(singlePluginMarketDir, ".claude-plugin")
		if err := os.MkdirAll(claudePluginDir, 0755); err != nil {
			t.Fatalf("failed to create .claude-plugin dir: %v", err)
		}
		marketplaceJSON := `{
			"name": "single-plugin-market",
			"plugins": [
				{"name": "my-single-plugin", "source": "./"}
			]
		}`
		if err := os.WriteFile(filepath.Join(claudePluginDir, "marketplace.json"), []byte(marketplaceJSON), 0644); err != nil {
			t.Fatalf("failed to write marketplace.json: %v", err)
		}
		if err := os.WriteFile(filepath.Join(claudePluginDir, "plugin.json"), []byte(`{}`), 0644); err != nil {
			t.Fatalf("failed to write plugin.json: %v", err)
		}

		// Update known_marketplaces.json to include this marketplace
		markets2 := map[string]any{
			"my-market": map[string]any{
				"installLocation": marketplaceDir,
			},
			"single-plugin-market": map[string]any{
				"installLocation": singlePluginMarketDir,
			},
		}
		data2, _ := json.Marshal(markets2)
		if err := os.WriteFile(knownMarketsPath, data2, 0644); err != nil {
			t.Fatalf("failed to write known_marketplaces.json: %v", err)
		}

		path, err := ResolveMarketplacePluginPathFromFile(knownMarketsPath, "single-plugin-market", "my-single-plugin")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// source "./" resolves to the marketplace root
		if path != singlePluginMarketDir {
			t.Errorf("expected %q, got %q", singlePluginMarketDir, path)
		}
	})

	t.Run("plugins dir takes precedence over external_plugins", func(t *testing.T) {
		// Create plugin in both plugins/ and external_plugins/
		for _, subdir := range []string{"plugins", "external_plugins"} {
			dir := filepath.Join(marketplaceDir, subdir, "dual-plugin", ".claude-plugin")
			if err := os.MkdirAll(dir, 0755); err != nil {
				t.Fatalf("failed to create dir: %v", err)
			}
			if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(`{}`), 0644); err != nil {
				t.Fatalf("failed to write plugin.json: %v", err)
			}
		}

		path, err := ResolveMarketplacePluginPathFromFile(knownMarketsPath, "my-market", "dual-plugin")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := filepath.Join(marketplaceDir, "plugins", "dual-plugin")
		if path != expected {
			t.Errorf("expected plugins/ to win, got %q", path)
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
		"git-sourced-market": map[string]any{
			"source": map[string]any{
				"source": "git",
				"url":    "https://github.com/someuser/agents.git",
			},
			"installLocation": "/home/user/.claude/plugins/marketplaces/git-sourced-market",
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

		// Git-sourced marketplace (source.url matching)
		{
			name:       "HTTPS URL matches git-sourced marketplace",
			identifier: "https://github.com/someuser/agents",
			expected:   "git-sourced-market",
		},
		{
			name:       "HTTPS URL with .git matches git-sourced marketplace",
			identifier: "https://github.com/someuser/agents.git",
			expected:   "git-sourced-market",
		},
		{
			name:       "org/repo matches git-sourced marketplace",
			identifier: "someuser/agents",
			expected:   "git-sourced-market",
		},
		{
			name:       "HTTPS URL with extra path matches git-sourced marketplace",
			identifier: "https://github.com/someuser/agents/tree/main/plugins/foo",
			expected:   "git-sourced-market",
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

func TestEnsureMarketplaceInstalledFromFile(t *testing.T) {
	tmpDir := t.TempDir()
	knownMarketsPath := filepath.Join(tmpDir, "known_marketplaces.json")

	// Set up known_marketplaces.json with one existing marketplace
	markets := map[string]any{
		"my-market": map[string]any{
			"source": map[string]any{
				"source": "github",
				"repo":   "myorg/my-market",
			},
			"installLocation": filepath.Join(tmpDir, "my-market"),
		},
	}
	data, _ := json.Marshal(markets)
	if err := os.WriteFile(knownMarketsPath, data, 0644); err != nil {
		t.Fatalf("failed to write known_marketplaces.json: %v", err)
	}

	t.Run("already installed by name", func(t *testing.T) {
		name, err := EnsureMarketplaceInstalledFromFile(t.Context(), knownMarketsPath, "my-market")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if name != "my-market" {
			t.Errorf("expected %q, got %q", "my-market", name)
		}
	})

	t.Run("already installed by URL", func(t *testing.T) {
		name, err := EnsureMarketplaceInstalledFromFile(t.Context(), knownMarketsPath, "https://github.com/myorg/my-market")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if name != "my-market" {
			t.Errorf("expected %q, got %q", "my-market", name)
		}
	})

	t.Run("plain name not found cannot auto-install", func(t *testing.T) {
		_, err := EnsureMarketplaceInstalledFromFile(t.Context(), knownMarketsPath, "nonexistent")
		if err == nil {
			t.Fatal("expected error for plain name that can't be auto-installed")
		}
		if !strings.Contains(err.Error(), "cannot be auto-installed") {
			t.Errorf("expected 'cannot be auto-installed' error, got: %v", err)
		}
	})
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
