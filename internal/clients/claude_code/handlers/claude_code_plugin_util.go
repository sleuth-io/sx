package handlers

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/sleuth-io/sx/internal/utils"
)

// sshGitURLPattern matches SSH git URLs like git@gitlab.com:org/repo.git
var sshGitURLPattern = regexp.MustCompile(`^git@[^:]+:(.+?)(?:\.git)?(?:#.*)?$`)

// httpsGitURLPattern matches HTTPS git URLs like https://gitlab.com/org/repo.git
var httpsGitURLPattern = regexp.MustCompile(`^https?://[^/]+/(.+?)(?:\.git)?(?:#.*)?$`)

// extractRepoIdentifier extracts org/repo from various marketplace identifier formats.
// Returns the org/repo string, or empty if the identifier is already a plain name.
func extractRepoIdentifier(identifier string) string {
	// SSH git URL: git@host:org/repo.git
	if matches := sshGitURLPattern.FindStringSubmatch(identifier); matches != nil {
		return matches[1]
	}

	// HTTPS git URL: https://host/org/repo.git
	if matches := httpsGitURLPattern.FindStringSubmatch(identifier); matches != nil {
		return matches[1]
	}

	// org/repo format (contains /)
	if strings.Contains(identifier, "/") {
		return identifier
	}

	// Already a plain name
	return ""
}

// knownMarketplaceEntry represents an entry in known_marketplaces.json
type knownMarketplaceEntry struct {
	Source struct {
		Source string `json:"source"`
		Repo   string `json:"repo"`
	} `json:"source"`
	InstallLocation string `json:"installLocation"`
}

// ResolveMarketplaceName resolves a marketplace identifier to its registered name
// in ~/.claude/plugins/known_marketplaces.json. Supports:
//   - Plain names: direct key match (e.g., "claude-code-plugins")
//   - org/repo: matched against source.repo field (e.g., "anthropics/claude-code" → "claude-code-plugins")
//   - Git URLs: org/repo extracted and matched (e.g., "https://github.com/anthropics/claude-code.git")
func ResolveMarketplaceName(marketplaceIdentifier string) (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	knownMarketsPath := filepath.Join(homeDir, ".claude", "plugins", "known_marketplaces.json")
	return ResolveMarketplaceNameFromFile(knownMarketsPath, marketplaceIdentifier)
}

// ResolveMarketplaceNameFromFile resolves a marketplace identifier using a specific
// known_marketplaces.json file path.
func ResolveMarketplaceNameFromFile(knownMarketsPath, identifier string) (string, error) {
	data, err := os.ReadFile(knownMarketsPath)
	if err != nil {
		return "", fmt.Errorf("failed to read known_marketplaces.json: %w", err)
	}

	var marketplaces map[string]knownMarketplaceEntry
	if err := json.Unmarshal(data, &marketplaces); err != nil {
		return "", fmt.Errorf("failed to parse known_marketplaces.json: %w", err)
	}

	// Direct key match
	if _, ok := marketplaces[identifier]; ok {
		return identifier, nil
	}

	// Extract org/repo from git URLs or use as-is for org/repo format
	repo := extractRepoIdentifier(identifier)
	if repo != "" {
		// Search by source.repo
		for name, m := range marketplaces {
			if m.Source.Repo == repo {
				return name, nil
			}
		}
	}

	var available []string
	for name := range marketplaces {
		available = append(available, name)
	}
	return "", fmt.Errorf("marketplace %q not found. Available: %s", identifier, strings.Join(available, ", "))
}

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
			return fmt.Errorf("failed to read %s: %w", registryPath, err)
		}
		if err := json.Unmarshal(data, &registry); err != nil {
			return fmt.Errorf("failed to parse %s: %w", registryPath, err)
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
		return fmt.Errorf("failed to read %s: %w", registryPath, err)
	}

	var registry InstalledPluginsRegistry
	if err := json.Unmarshal(data, &registry); err != nil {
		return fmt.Errorf("failed to parse %s: %w", registryPath, err)
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
			return fmt.Errorf("failed to read %s: %w", settingsPath, err)
		}
		if err := json.Unmarshal(data, &settings); err != nil {
			return fmt.Errorf("failed to parse %s: %w", settingsPath, err)
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

	if err := os.WriteFile(settingsPath, data, 0600); err != nil {
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
		return fmt.Errorf("failed to read %s: %w", settingsPath, err)
	}

	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		return fmt.Errorf("failed to parse %s: %w", settingsPath, err)
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

	if err := os.WriteFile(settingsPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write settings.json: %w", err)
	}

	return nil
}

// ResolveMarketplacePluginPath looks up a plugin in a Claude Code marketplace.
// It reads ~/.claude/plugins/known_marketplaces.json and resolves the plugin path.
// The marketplace name is normalized (e.g., "org/repo" → "org-repo") before lookup.
func ResolveMarketplacePluginPath(marketplaceName, pluginName string) (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	knownMarketsPath := filepath.Join(homeDir, ".claude", "plugins", "known_marketplaces.json")
	return ResolveMarketplacePluginPathFromFile(knownMarketsPath, marketplaceName, pluginName)
}

// ResolveMarketplacePluginPathFromFile looks up a plugin using a specific known_marketplaces.json path.
// The marketplaceName must be the resolved name (as registered in known_marketplaces.json).
func ResolveMarketplacePluginPathFromFile(knownMarketsPath, marketplaceName, pluginName string) (string, error) {
	data, err := os.ReadFile(knownMarketsPath)
	if err != nil {
		return "", fmt.Errorf("failed to read known_marketplaces.json: %w", err)
	}

	var marketplaces map[string]struct {
		InstallLocation string `json:"installLocation"`
	}
	if err := json.Unmarshal(data, &marketplaces); err != nil {
		return "", fmt.Errorf("failed to parse known_marketplaces.json: %w", err)
	}

	marketplace, ok := marketplaces[marketplaceName]
	if !ok {
		var available []string
		for name := range marketplaces {
			available = append(available, name)
		}
		return "", fmt.Errorf("marketplace %q not found. Available: %s", marketplaceName, strings.Join(available, ", "))
	}

	pluginPaths := []string{
		filepath.Join(marketplace.InstallLocation, "plugins", pluginName),
		filepath.Join(marketplace.InstallLocation, pluginName),
	}

	for _, path := range pluginPaths {
		manifestPath := filepath.Join(path, ".claude-plugin", "plugin.json")
		if utils.FileExists(manifestPath) {
			return path, nil
		}
	}

	pluginsDir := filepath.Join(marketplace.InstallLocation, "plugins")
	if utils.IsDirectory(pluginsDir) {
		entries, _ := os.ReadDir(pluginsDir)
		var available []string
		for _, entry := range entries {
			if entry.IsDir() && entry.Name() != "." && entry.Name() != ".." {
				if utils.FileExists(filepath.Join(pluginsDir, entry.Name(), ".claude-plugin", "plugin.json")) {
					available = append(available, entry.Name())
				}
			}
		}
		if len(available) > 0 {
			return "", fmt.Errorf("plugin %q not found in marketplace %q. Available plugins: %s", pluginName, marketplaceName, strings.Join(available, ", "))
		}
	}

	return "", fmt.Errorf("plugin %q not found in marketplace %q", pluginName, marketplaceName)
}

// IsPluginRegistered checks if a plugin is registered in installed_plugins.json
func IsPluginRegistered(targetBase, pluginName, marketplace string) bool {
	registryPath := filepath.Join(targetBase, installedPluginsFile)
	if !utils.FileExists(registryPath) {
		return false
	}

	data, err := os.ReadFile(registryPath)
	if err != nil {
		return false
	}

	var registry InstalledPluginsRegistry
	if err := json.Unmarshal(data, &registry); err != nil {
		return false
	}

	if registry.Plugins == nil {
		return false
	}

	pluginKey := BuildPluginKey(pluginName, marketplace)
	_, exists := registry.Plugins[pluginKey]
	return exists
}
