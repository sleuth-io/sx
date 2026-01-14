package handlers

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/handlers/dirasset"
	"github.com/sleuth-io/sx/internal/metadata"
	"github.com/sleuth-io/sx/internal/utils"
)

var claudeCodePluginOps = dirasset.NewOperations("plugins", &asset.TypeClaudeCodePlugin)

// ClaudeCodePluginHandler handles Claude Code plugin asset installation
type ClaudeCodePluginHandler struct {
	metadata *metadata.Metadata
}

// NewClaudeCodePluginHandler creates a new plugin handler
func NewClaudeCodePluginHandler(meta *metadata.Metadata) *ClaudeCodePluginHandler {
	return &ClaudeCodePluginHandler{
		metadata: meta,
	}
}

// DetectType returns true if files indicate this is a Claude Code plugin asset
func (h *ClaudeCodePluginHandler) DetectType(files []string) bool {
	return slices.Contains(files, ".claude-plugin/plugin.json")
}

// GetType returns the asset type string
func (h *ClaudeCodePluginHandler) GetType() string {
	return "claude-code-plugin"
}

// CreateDefaultMetadata creates default metadata for a plugin
func (h *ClaudeCodePluginHandler) CreateDefaultMetadata(name, version string) *metadata.Metadata {
	return &metadata.Metadata{
		MetadataVersion: metadata.CurrentMetadataVersion,
		Asset: metadata.Asset{
			Name:    name,
			Version: version,
			Type:    asset.TypeClaudeCodePlugin,
		},
		ClaudeCodePlugin: &metadata.ClaudeCodePluginConfig{
			ManifestFile: ".claude-plugin/plugin.json",
		},
	}
}

// GetPromptFile returns empty for plugins (not applicable at top level)
func (h *ClaudeCodePluginHandler) GetPromptFile(meta *metadata.Metadata) string {
	return ""
}

// GetScriptFile returns empty for plugins (not applicable)
func (h *ClaudeCodePluginHandler) GetScriptFile(meta *metadata.Metadata) string {
	return ""
}

// ValidateMetadata validates plugin-specific metadata
func (h *ClaudeCodePluginHandler) ValidateMetadata(meta *metadata.Metadata) error {
	// ClaudeCodePlugin section is optional - all fields have defaults
	return nil
}

// DetectUsageFromToolCall detects plugin usage from tool calls
// Plugins are not directly invoked - their contents (commands, skills, etc.) are
func (h *ClaudeCodePluginHandler) DetectUsageFromToolCall(toolName string, toolInput map[string]any) (string, bool) {
	return "", false
}

// Install extracts and installs the plugin asset
func (h *ClaudeCodePluginHandler) Install(ctx context.Context, zipData []byte, targetBase string) error {
	// Validate zip structure
	if err := h.Validate(zipData); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	// Extract to plugins directory
	if err := claudeCodePluginOps.Install(ctx, zipData, targetBase, h.metadata.Asset.Name); err != nil {
		return err
	}

	installPath := filepath.Join(targetBase, h.GetInstallPath())
	marketplace := h.getMarketplace()

	// Register plugin in installed_plugins.json
	if err := RegisterPlugin(targetBase, h.metadata.Asset.Name, marketplace, h.metadata.Asset.Version, installPath); err != nil {
		return fmt.Errorf("failed to register plugin %q: %w", h.metadata.Asset.Name, err)
	}

	// Enable the plugin in settings.json if auto-enable is not disabled
	if h.shouldAutoEnable() {
		if err := EnablePlugin(targetBase, h.metadata.Asset.Name, marketplace, installPath); err != nil {
			return fmt.Errorf("failed to enable plugin: %w", err)
		}
	}

	return nil
}

// shouldAutoEnable checks if the plugin should be automatically enabled
func (h *ClaudeCodePluginHandler) shouldAutoEnable() bool {
	if h.metadata.ClaudeCodePlugin == nil {
		return true // Default is to auto-enable
	}
	if h.metadata.ClaudeCodePlugin.AutoEnable == nil {
		return true // Default is to auto-enable
	}
	return *h.metadata.ClaudeCodePlugin.AutoEnable
}

// getMarketplace returns the marketplace from metadata, or empty string if not set
func (h *ClaudeCodePluginHandler) getMarketplace() string {
	if h.metadata.ClaudeCodePlugin == nil {
		return ""
	}
	return h.metadata.ClaudeCodePlugin.Marketplace
}

// Remove uninstalls the plugin asset
func (h *ClaudeCodePluginHandler) Remove(ctx context.Context, targetBase string) error {
	marketplace := h.getMarketplace()

	// Unregister plugin from installed_plugins.json
	if err := UnregisterPlugin(targetBase, h.metadata.Asset.Name, marketplace); err != nil {
		return fmt.Errorf("failed to unregister plugin: %w", err)
	}

	// Disable the plugin in settings.json
	if err := DisablePlugin(targetBase, h.metadata.Asset.Name, marketplace); err != nil {
		return fmt.Errorf("failed to disable plugin: %w", err)
	}

	// Remove installation directory
	return claudeCodePluginOps.Remove(ctx, targetBase, h.metadata.Asset.Name)
}

// GetInstallPath returns the installation path relative to targetBase
func (h *ClaudeCodePluginHandler) GetInstallPath() string {
	return filepath.Join("plugins", h.metadata.Asset.Name)
}

// Validate checks if the zip structure is valid for a plugin asset
func (h *ClaudeCodePluginHandler) Validate(zipData []byte) error {
	// List files in zip
	files, err := utils.ListZipFiles(zipData)
	if err != nil {
		return fmt.Errorf("failed to list zip files: %w", err)
	}

	// Check that metadata.toml exists
	if !slices.Contains(files, "metadata.toml") {
		return errors.New("metadata.toml not found in zip")
	}

	// Extract and validate metadata
	metadataBytes, err := utils.ReadZipFile(zipData, "metadata.toml")
	if err != nil {
		return fmt.Errorf("failed to read metadata.toml: %w", err)
	}

	meta, err := metadata.Parse(metadataBytes)
	if err != nil {
		return fmt.Errorf("failed to parse metadata: %w", err)
	}

	// Validate metadata with file list
	if err := meta.ValidateWithFiles(files); err != nil {
		return fmt.Errorf("metadata validation failed: %w", err)
	}

	// Verify asset type matches
	if meta.Asset.Type != asset.TypeClaudeCodePlugin {
		return fmt.Errorf("asset type mismatch: expected claude-code-plugin, got %s", meta.Asset.Type)
	}

	// Check that plugin manifest exists
	manifestFile := ".claude-plugin/plugin.json"
	if meta.ClaudeCodePlugin != nil && meta.ClaudeCodePlugin.ManifestFile != "" {
		manifestFile = meta.ClaudeCodePlugin.ManifestFile
	}
	if !slices.Contains(files, manifestFile) {
		return fmt.Errorf("plugin manifest not found in zip: %s", manifestFile)
	}

	return nil
}

// CanDetectInstalledState returns true since plugins preserve metadata.toml
func (h *ClaudeCodePluginHandler) CanDetectInstalledState() bool {
	return true
}

// VerifyInstalled checks if the plugin is properly installed
func (h *ClaudeCodePluginHandler) VerifyInstalled(targetBase string) (bool, string) {
	return claudeCodePluginOps.VerifyInstalled(targetBase, h.metadata.Asset.Name, h.metadata.Asset.Version)
}
