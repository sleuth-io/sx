package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestClaudeCodePluginIntegration tests the full workflow for claude-code-plugin assets
func TestClaudeCodePluginIntegration(t *testing.T) {
	env := NewTestEnv(t)

	vaultDir := env.SetupPathVault()
	env.AddPluginToVault(vaultDir, "test-plugin", "1.0.0")

	// Create lock file with the plugin asset
	lockFile := `lock-version = "1"
version = "1.0.0"
created-by = "test"

[[assets]]
name = "test-plugin"
version = "1.0.0"
type = "claude-code-plugin"

[assets.source-path]
path = "assets/test-plugin/1.0.0"
`
	env.WriteLockFile(vaultDir, lockFile)

	// Set up git repo (needed for install context)
	projectDir := env.SetupGitRepo("project", "https://github.com/testorg/testrepo")
	env.Chdir(projectDir)

	// Step 1: Install the plugin
	t.Log("Step 1: Install plugin from repository")
	installCmd := NewInstallCommand()
	if err := installCmd.Execute(); err != nil {
		t.Fatalf("Failed to install: %v", err)
	}

	// Step 2: Verify plugin was installed to ~/.claude/plugins/test-plugin
	t.Log("Step 2: Verify plugin installation")
	pluginDir := filepath.Join(env.GlobalClaudeDir(), "plugins", "test-plugin")
	env.AssertFileExists(pluginDir)

	// Verify metadata.toml exists
	env.AssertFileExists(filepath.Join(pluginDir, "metadata.toml"))

	// Verify plugin.json exists in .claude-plugin subdirectory
	env.AssertFileExists(filepath.Join(pluginDir, ".claude-plugin", "plugin.json"))

	// Verify README.md exists
	env.AssertFileExists(filepath.Join(pluginDir, "README.md"))

	// Step 3: Verify plugin was enabled in settings.json
	t.Log("Step 3: Verify plugin enablement in settings.json")
	settingsPath := filepath.Join(env.GlobalClaudeDir(), "settings.json")
	settingsData, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("Failed to read settings.json: %v", err)
	}

	var settings map[string]any
	if err := json.Unmarshal(settingsData, &settings); err != nil {
		t.Fatalf("Failed to parse settings.json: %v", err)
	}

	enabledPlugins, ok := settings["enabledPlugins"].(map[string]any)
	if !ok {
		t.Fatalf("enabledPlugins not found or invalid in settings.json")
	}

	if enabled, exists := enabledPlugins["test-plugin"]; !exists || enabled != true {
		t.Errorf("Plugin not enabled in settings.json. enabledPlugins: %v", enabledPlugins)
	}

	// Step 4: Verify plugin was registered in installed_plugins.json
	t.Log("Step 4: Verify plugin registration in installed_plugins.json")
	registryPath := filepath.Join(env.GlobalClaudeDir(), "plugins", "installed_plugins.json")
	registryData, err := os.ReadFile(registryPath)
	if err != nil {
		t.Fatalf("Failed to read installed_plugins.json: %v", err)
	}

	var registry map[string]any
	if err := json.Unmarshal(registryData, &registry); err != nil {
		t.Fatalf("Failed to parse installed_plugins.json: %v", err)
	}

	plugins, ok := registry["plugins"].(map[string]any)
	if !ok {
		t.Fatalf("plugins not found or invalid in installed_plugins.json")
	}

	// Plugin key is just the name when no marketplace is set
	if _, exists := plugins["test-plugin"]; !exists {
		t.Errorf("Plugin not registered in installed_plugins.json. plugins: %v", plugins)
	}

	t.Log("Plugin integration test passed!")
}

// TestClaudeCodePluginUninstall tests that plugins are properly removed
func TestClaudeCodePluginUninstall(t *testing.T) {
	env := NewTestEnv(t)

	vaultDir := env.SetupPathVault()
	env.AddPluginToVault(vaultDir, "test-plugin", "1.0.0")
	env.AddSkillToVault(vaultDir, "test-skill", "1.0.0") // Keep one asset so cleanup runs

	// Lock file WITH plugin
	lockFileWithPlugin := `lock-version = "1"
version = "1.0.0"
created-by = "test"

[[assets]]
name = "test-plugin"
version = "1.0.0"
type = "claude-code-plugin"

[assets.source-path]
path = "assets/test-plugin/1.0.0"

[[assets]]
name = "test-skill"
version = "1.0.0"
type = "skill"

[assets.source-path]
path = "assets/test-skill/1.0.0"
`
	env.WriteLockFile(vaultDir, lockFileWithPlugin)

	projectDir := env.SetupGitRepo("project", "https://github.com/testorg/testrepo")
	env.Chdir(projectDir)

	// Step 1: Install both assets
	t.Log("Step 1: Install plugin and skill")
	installCmd := NewInstallCommand()
	if err := installCmd.Execute(); err != nil {
		t.Fatalf("Failed to install: %v", err)
	}

	pluginDir := filepath.Join(env.GlobalClaudeDir(), "plugins", "test-plugin")
	env.AssertFileExists(pluginDir)
	t.Log("Plugin installed successfully")

	// Step 2: Remove plugin from lock file (simulating sx uninstall)
	lockFileWithoutPlugin := `lock-version = "1"
version = "1.0.0"
created-by = "test"

[[assets]]
name = "test-skill"
version = "1.0.0"
type = "skill"

[assets.source-path]
path = "assets/test-skill/1.0.0"
`
	env.WriteLockFile(vaultDir, lockFileWithoutPlugin)

	// Step 3: Run install again to trigger cleanup
	t.Log("Step 2: Remove plugin and verify cleanup")
	installCmd2 := NewInstallCommand()
	if err := installCmd2.Execute(); err != nil {
		t.Fatalf("Second install failed: %v", err)
	}

	// Step 4: Verify plugin was removed
	env.AssertFileNotExists(pluginDir)

	// Step 5: Verify plugin was disabled in settings.json
	settingsPath := filepath.Join(env.GlobalClaudeDir(), "settings.json")
	settingsData, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("Failed to read settings.json: %v", err)
	}

	var settings map[string]any
	if err := json.Unmarshal(settingsData, &settings); err != nil {
		t.Fatalf("Failed to parse settings.json: %v", err)
	}

	if enabledPlugins, ok := settings["enabledPlugins"].(map[string]any); ok {
		if _, exists := enabledPlugins["test-plugin"]; exists {
			t.Errorf("Plugin should have been removed from enabledPlugins")
		}
	}

	t.Log("Plugin uninstall test passed!")
}

// TestClaudeCodePluginAutoEnableFalse tests that plugins with auto-enable=false
// are not automatically enabled in settings.json
func TestClaudeCodePluginAutoEnableFalse(t *testing.T) {
	env := NewTestEnv(t)

	vaultDir := env.SetupPathVault()

	// Create plugin with auto-enable = false
	pluginDir := env.MkdirAll(filepath.Join(vaultDir, "assets", "manual-plugin", "1.0.0"))

	metadata := `[asset]
name = "manual-plugin"
type = "claude-code-plugin"
version = "1.0.0"
description = "Plugin that requires manual enablement"

[claude-code-plugin]
manifest-file = ".claude-plugin/plugin.json"
auto-enable = false
`

	pluginJSON := `{
  "name": "manual-plugin",
  "description": "Plugin that requires manual enablement",
  "version": "1.0.0",
  "author": { "name": "Test Author" }
}`

	env.WriteFile(filepath.Join(pluginDir, "metadata.toml"), metadata)
	env.MkdirAll(filepath.Join(pluginDir, ".claude-plugin"))
	env.WriteFile(filepath.Join(pluginDir, ".claude-plugin", "plugin.json"), pluginJSON)

	lockFile := `lock-version = "1"
version = "1.0.0"
created-by = "test"

[[assets]]
name = "manual-plugin"
version = "1.0.0"
type = "claude-code-plugin"

[assets.source-path]
path = "assets/manual-plugin/1.0.0"
`
	env.WriteLockFile(vaultDir, lockFile)

	projectDir := env.SetupGitRepo("project", "https://github.com/testorg/testrepo")
	env.Chdir(projectDir)

	// Install the plugin
	installCmd := NewInstallCommand()
	if err := installCmd.Execute(); err != nil {
		t.Fatalf("Failed to install: %v", err)
	}

	// Verify plugin was installed
	installedDir := filepath.Join(env.GlobalClaudeDir(), "plugins", "manual-plugin")
	env.AssertFileExists(installedDir)

	// Verify plugin was NOT enabled in settings.json
	settingsPath := filepath.Join(env.GlobalClaudeDir(), "settings.json")
	settingsData, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("Failed to read settings.json: %v", err)
	}

	var settings map[string]any
	if err := json.Unmarshal(settingsData, &settings); err != nil {
		t.Fatalf("Failed to parse settings.json: %v", err)
	}

	if enabledPlugins, ok := settings["enabledPlugins"].(map[string]any); ok {
		if _, exists := enabledPlugins["manual-plugin"]; exists {
			t.Errorf("Plugin with auto-enable=false should not be in enabledPlugins")
		}
	}

	t.Log("Auto-enable=false test passed!")
}
