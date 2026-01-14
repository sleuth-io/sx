package handlers

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/sleuth-io/sx/internal/utils"
)

// installedPluginsFile is the filename for the installed plugins registry
const installedPluginsFile = "plugins/installed_plugins.json"

// InstalledPluginsRegistry represents the installed_plugins.json structure
type InstalledPluginsRegistry struct {
	Version int                              `json:"version"`
	Plugins map[string][]InstalledPluginInfo `json:"plugins"`
}

// InstalledPluginInfo represents a single plugin installation entry
type InstalledPluginInfo struct {
	Scope       string `json:"scope"`
	InstallPath string `json:"installPath"`
	Version     string `json:"version"`
	InstalledAt string `json:"installedAt"`
	LastUpdated string `json:"lastUpdated"`
	IsLocal     bool   `json:"isLocal"`
}

// BuildPluginKey creates the plugin key for installed_plugins.json and enabledPlugins
// Format: plugin-name@marketplace (or just plugin-name if no marketplace)
func BuildPluginKey(pluginName, marketplace string) string {
	if marketplace != "" {
		return pluginName + "@" + marketplace
	}
	return pluginName
}

// RegisterPlugin adds a plugin to installed_plugins.json
func RegisterPlugin(targetBase, pluginName, marketplace, version, installPath string) error {
	registryPath := filepath.Join(targetBase, installedPluginsFile)

	// Read existing registry or create new
	var registry InstalledPluginsRegistry
	if utils.FileExists(registryPath) {
		data, err := os.ReadFile(registryPath)
		if err != nil {
			return fmt.Errorf("failed to read installed_plugins.json: %w", err)
		}
		if err := json.Unmarshal(data, &registry); err != nil {
			return fmt.Errorf("failed to parse installed_plugins.json: %w", err)
		}
	} else {
		registry = InstalledPluginsRegistry{
			Version: 2,
			Plugins: make(map[string][]InstalledPluginInfo),
		}
	}

	// Ensure plugins map exists
	if registry.Plugins == nil {
		registry.Plugins = make(map[string][]InstalledPluginInfo)
	}

	// Create plugin key using marketplace if available
	pluginKey := BuildPluginKey(pluginName, marketplace)
	now := time.Now().UTC().Format(time.RFC3339Nano)

	// Create or update plugin entry
	registry.Plugins[pluginKey] = []InstalledPluginInfo{
		{
			Scope:       "user",
			InstallPath: installPath,
			Version:     version,
			InstalledAt: now,
			LastUpdated: now,
			IsLocal:     marketplace == "", // isLocal only if no marketplace
		},
	}

	// Write updated registry
	data, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal installed_plugins.json: %w", err)
	}

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(registryPath), 0755); err != nil {
		return fmt.Errorf("failed to create directory for installed_plugins.json: %w", err)
	}

	if err := os.WriteFile(registryPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write installed_plugins.json: %w", err)
	}

	return nil
}

// UnregisterPlugin removes a plugin from installed_plugins.json
func UnregisterPlugin(targetBase, pluginName, marketplace string) error {
	registryPath := filepath.Join(targetBase, installedPluginsFile)

	if !utils.FileExists(registryPath) {
		return nil // Nothing to remove
	}

	// Read registry
	data, err := os.ReadFile(registryPath)
	if err != nil {
		return fmt.Errorf("failed to read installed_plugins.json: %w", err)
	}

	var registry InstalledPluginsRegistry
	if err := json.Unmarshal(data, &registry); err != nil {
		return fmt.Errorf("failed to parse installed_plugins.json: %w", err)
	}

	if registry.Plugins == nil {
		return nil
	}

	// Remove plugin entry
	pluginKey := BuildPluginKey(pluginName, marketplace)
	delete(registry.Plugins, pluginKey)

	// Write updated registry
	data, err = json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal installed_plugins.json: %w", err)
	}

	if err := os.WriteFile(registryPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write installed_plugins.json: %w", err)
	}

	return nil
}

// EnablePlugin enables a plugin in settings.json
func EnablePlugin(targetBase, pluginName, marketplace, installPath string) error {
	settingsPath := filepath.Join(targetBase, "settings.json")

	// Read existing settings or create new
	var settings map[string]any
	if utils.FileExists(settingsPath) {
		data, err := os.ReadFile(settingsPath)
		if err != nil {
			return fmt.Errorf("failed to read settings.json: %w", err)
		}
		if err := json.Unmarshal(data, &settings); err != nil {
			return fmt.Errorf("failed to parse settings.json: %w", err)
		}
	} else {
		settings = make(map[string]any)
	}

	// Ensure enabledPlugins section exists
	if settings["enabledPlugins"] == nil {
		settings["enabledPlugins"] = make(map[string]any)
	}
	enabledPlugins, ok := settings["enabledPlugins"].(map[string]any)
	if !ok {
		// enabledPlugins exists but is wrong type, recreate it
		enabledPlugins = make(map[string]any)
		settings["enabledPlugins"] = enabledPlugins
	}

	// Add plugin entry with marketplace key
	pluginKey := BuildPluginKey(pluginName, marketplace)
	enabledPlugins[pluginKey] = true

	// Write updated settings
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal settings: %w", err)
	}

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0755); err != nil {
		return fmt.Errorf("failed to create directory for settings.json: %w", err)
	}

	if err := os.WriteFile(settingsPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write settings.json: %w", err)
	}

	return nil
}

// DisablePlugin removes a plugin from the enabledPlugins in settings.json
func DisablePlugin(targetBase, pluginName, marketplace string) error {
	settingsPath := filepath.Join(targetBase, "settings.json")

	if !utils.FileExists(settingsPath) {
		return nil // Nothing to remove
	}

	// Read settings
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return fmt.Errorf("failed to read settings.json: %w", err)
	}

	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		return fmt.Errorf("failed to parse settings.json: %w", err)
	}

	// Check if enabledPlugins section exists
	if settings["enabledPlugins"] == nil {
		return nil
	}
	enabledPlugins, ok := settings["enabledPlugins"].(map[string]any)
	if !ok {
		return nil
	}

	// Remove this plugin
	pluginKey := BuildPluginKey(pluginName, marketplace)
	delete(enabledPlugins, pluginKey)

	// Write updated settings
	data, err = json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal settings: %w", err)
	}

	if err := os.WriteFile(settingsPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write settings.json: %w", err)
	}

	return nil
}
