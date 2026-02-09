package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/sx/internal/clients/claude_code/handlers"
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
	if strings.Contains(ref.Marketplace, "..") || strings.Contains(ref.Marketplace, "\\") {
		return fmt.Errorf("invalid marketplace name: %q", ref.Marketplace)
	}
	if strings.Contains(ref.PluginName, "..") {
		return fmt.Errorf("invalid plugin name: %q", ref.PluginName)
	}
	return nil
}

// addFromMarketplace handles adding a plugin from a Claude Code marketplace
func addFromMarketplace(ctx context.Context, cmd *cobra.Command, out *outputHelper, status *components.Status, input string, promptInstall bool, opts addOptions) error {
	ref := ParseMarketplaceReference(input)

	if err := ValidateMarketplaceReference(ref); err != nil {
		return err
	}

	status.Start("Looking up marketplace")

	// Resolve marketplace identifier (e.g., "anthropics/claude-code" → "claude-code-plugins")
	resolvedName, err := handlers.ResolveMarketplaceName(ref.Marketplace)
	if err != nil {
		status.Fail("Marketplace lookup failed")
		return err
	}
	ref.Marketplace = resolvedName

	pluginPath, err := handlers.ResolveMarketplacePluginPath(ref.Marketplace, ref.PluginName)
	if err != nil {
		status.Fail("Marketplace lookup failed")
		return err
	}
	status.Done("")

	out.printf("Found plugin: %s in marketplace %s\n", ref.PluginName, ref.Marketplace)

	// Determine import vs vendor mode
	useImport, err := promptImportOrVendor(cmd, out, opts)
	if err != nil {
		return err
	}

	var zipData []byte
	if useImport {
		// Import mode: config-only zip with source = "marketplace"
		status.Start("Creating config-only package")
		zipData, err = createConfigOnlyPluginZip(pluginPath, ref)
		if err != nil {
			status.Fail("Failed to create package")
			return fmt.Errorf("failed to create config-only zip: %w", err)
		}
		status.Done("")
	} else {
		// Vendor mode: full zip with source = "local"
		status.Start("Creating zip from plugin directory")
		zipData, err = utils.CreateZip(pluginPath)
		if err != nil {
			status.Fail("Failed to create zip")
			return fmt.Errorf("failed to create zip from plugin directory: %w", err)
		}
		status.Done("")

		zipData, err = injectMarketplaceMetadata(zipData, ref, "local")
		if err != nil {
			return fmt.Errorf("failed to inject marketplace metadata: %w", err)
		}
	}

	return runAddWithZipData(ctx, cmd, out, status, pluginPath, zipData, promptInstall, opts)
}

// promptImportOrVendor asks the user whether to import (marketplace-owned) or vendor (sx-owned).
// With --yes, defaults to import.
func promptImportOrVendor(cmd *cobra.Command, out *outputHelper, opts addOptions) (bool, error) {
	if opts.Yes {
		return true, nil // Default to import with --yes
	}

	options := []components.Option{
		{
			Label:       "Import (Recommended)",
			Value:       "import",
			Description: "Marketplace retains ownership, auto-updates continue",
		},
		{
			Label:       "Vendor",
			Value:       "vendor",
			Description: "Snapshot plugin into vault, sx owns the files",
		},
	}

	out.println()
	selected, err := components.SelectWithIO("How should this plugin be managed?", options, cmd.InOrStdin(), cmd.OutOrStdout())
	if err != nil {
		return false, fmt.Errorf("failed to select mode: %w", err)
	}

	return selected.Value == "import", nil
}

// createConfigOnlyPluginZip creates a zip containing only metadata.toml with source = "marketplace".
// It reads plugin.json from the marketplace plugin directory for name/version/description.
func createConfigOnlyPluginZip(pluginPath string, ref MarketplaceReference) ([]byte, error) {
	manifestPath := filepath.Join(pluginPath, ".claude-plugin", "plugin.json")
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read plugin.json: %w", err)
	}

	var pluginManifest struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Version     string `json:"version"`
	}
	if err := json.Unmarshal(manifestData, &pluginManifest); err != nil {
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
marketplace = %q
source = "marketplace"
`, ref.PluginName, version, pluginManifest.Description, ref.Marketplace)

	return utils.CreateZipFromContent("metadata.toml", []byte(metadataContent))
}

// injectMarketplaceMetadata adds or updates metadata.toml with marketplace info
func injectMarketplaceMetadata(zipData []byte, ref MarketplaceReference, source string) ([]byte, error) {
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

	sourceLine := ""
	if source != "" {
		sourceLine = fmt.Sprintf("source = %q\n", source)
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
%s`, ref.PluginName, version, pluginManifest.Description, ref.Marketplace, sourceLine)

	_, err = utils.ReadZipFile(zipData, "metadata.toml")
	if err == nil {
		return utils.ReplaceFileInZip(zipData, "metadata.toml", []byte(metadataContent))
	}

	return utils.AddFileToZip(zipData, "metadata.toml", []byte(metadataContent))
}

// runAddWithZipData continues the add flow with pre-loaded zip data
func runAddWithZipData(ctx context.Context, cmd *cobra.Command, out *outputHelper, status *components.Status, sourcePath string, zipData []byte, promptInstall bool, opts addOptions) error {
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
