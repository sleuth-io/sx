package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/sx/internal/github"
	"github.com/sleuth-io/sx/internal/ui/components"
	"github.com/sleuth-io/sx/internal/utils"
)

// MarketplaceReference represents a parsed plugin@marketplace reference
type MarketplaceReference struct {
	PluginName  string
	Marketplace string
}

// IsMarketplaceReference checks if input matches the plugin@marketplace pattern
func IsMarketplaceReference(input string) bool {
	if !strings.Contains(input, "@") {
		return false
	}
	if isURL(input) {
		return false
	}
	if github.IsTreeURL(input) {
		return false
	}
	parts := strings.SplitN(input, "@", 2)
	if len(parts) != 2 {
		return false
	}
	pluginName := parts[0]
	marketplace := parts[1]
	// Reject "git" as plugin name to avoid conflict with git SSH URLs (git@github.com:user/repo)
	if pluginName == "git" {
		return false
	}
	return pluginName != "" && marketplace != "" && !strings.Contains(pluginName, "/") && !strings.Contains(pluginName, "\\")
}

// ParseMarketplaceReference splits plugin@marketplace into its parts
func ParseMarketplaceReference(input string) MarketplaceReference {
	parts := strings.SplitN(input, "@", 2)
	if len(parts) == 2 {
		return MarketplaceReference{
			PluginName:  parts[0],
			Marketplace: parts[1],
		}
	}
	return MarketplaceReference{PluginName: input}
}

// ValidateMarketplaceReference validates that a marketplace reference doesn't contain path traversal characters
func ValidateMarketplaceReference(ref MarketplaceReference) error {
	if strings.Contains(ref.Marketplace, "..") || strings.Contains(ref.Marketplace, "/") || strings.Contains(ref.Marketplace, "\\") {
		return fmt.Errorf("invalid marketplace name: %q", ref.Marketplace)
	}
	if strings.Contains(ref.PluginName, "..") {
		return fmt.Errorf("invalid plugin name: %q", ref.PluginName)
	}
	return nil
}

// addFromMarketplace handles adding a plugin from a Claude Code marketplace
func addFromMarketplace(ctx context.Context, cmd *cobra.Command, out *outputHelper, status *components.Status, input string, promptInstall bool) error {
	ref := ParseMarketplaceReference(input)

	if err := ValidateMarketplaceReference(ref); err != nil {
		return err
	}

	status.Start("Looking up marketplace")

	pluginPath, err := findMarketplacePluginPath(ref.Marketplace, ref.PluginName)
	if err != nil {
		status.Fail("Marketplace lookup failed")
		return err
	}
	status.Done("")

	out.printf("Found plugin: %s in marketplace %s\n", ref.PluginName, ref.Marketplace)

	status.Start("Creating zip from plugin directory")
	zipData, err := utils.CreateZip(pluginPath)
	if err != nil {
		status.Fail("Failed to create zip")
		return fmt.Errorf("failed to create zip from plugin directory: %w", err)
	}
	status.Done("")

	zipData, err = injectMarketplaceMetadata(zipData, ref)
	if err != nil {
		return fmt.Errorf("failed to inject marketplace metadata: %w", err)
	}

	return runAddWithZipData(ctx, cmd, out, status, pluginPath, zipData, promptInstall)
}

// findMarketplacePluginPath looks up a plugin in a Claude Code marketplace
func findMarketplacePluginPath(marketplaceName, pluginName string) (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	knownMarketsPath := filepath.Join(homeDir, ".claude", "plugins", "known_marketplaces.json")
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

// injectMarketplaceMetadata adds or updates metadata.toml with marketplace info
func injectMarketplaceMetadata(zipData []byte, ref MarketplaceReference) ([]byte, error) {
	pluginJSON, err := utils.ReadZipFile(zipData, ".claude-plugin/plugin.json")
	if err != nil {
		return nil, fmt.Errorf("failed to read plugin.json: %w", err)
	}

	var pluginManifest struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Version     string `json:"version"`
	}
	if err := json.Unmarshal(pluginJSON, &pluginManifest); err != nil {
		return nil, fmt.Errorf("failed to parse plugin.json: %w", err)
	}

	version := pluginManifest.Version
	if version == "" {
		version = "1.0.0"
	}

	metadataContent := fmt.Sprintf(`metadata-version = "1.0"

[asset]
name = %q
version = %q
type = "claude-code-plugin"
description = %q

[claude-code-plugin]
manifest-file = ".claude-plugin/plugin.json"
marketplace = %q
`, ref.PluginName, version, pluginManifest.Description, ref.Marketplace)

	_, err = utils.ReadZipFile(zipData, "metadata.toml")
	if err == nil {
		return utils.ReplaceFileInZip(zipData, "metadata.toml", []byte(metadataContent))
	}

	return utils.AddFileToZip(zipData, "metadata.toml", []byte(metadataContent))
}

// runAddWithZipData continues the add flow with pre-loaded zip data
func runAddWithZipData(ctx context.Context, cmd *cobra.Command, out *outputHelper, status *components.Status, sourcePath string, zipData []byte, promptInstall bool) error {
	// Marketplace flow is always interactive, so use empty options
	opts := addOptions{}

	name, assetType, metadataExists, err := detectAssetInfo(out, status, sourcePath, zipData, opts)
	if err != nil {
		return err
	}

	vault, err := createVault()
	if err != nil {
		return err
	}

	version, contentsIdentical, err := checkVersionAndContents(ctx, status, vault, name, zipData)
	if err != nil {
		return err
	}

	var addErr error
	if contentsIdentical {
		addErr = handleIdenticalAsset(ctx, out, status, vault, name, version, assetType, opts)
	} else {
		addErr = addNewAsset(ctx, out, status, vault, name, assetType, version, sourcePath, zipData, metadataExists, opts)
	}

	if addErr != nil {
		return addErr
	}

	if promptInstall {
		promptRunInstall(cmd, ctx, out)
	}

	return nil
}
