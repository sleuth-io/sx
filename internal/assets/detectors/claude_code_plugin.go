package detectors

import (
	"slices"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/metadata"
)

// ClaudeCodePluginDetector detects Claude Code plugin assets
type ClaudeCodePluginDetector struct{}

// Compile-time interface checks
var (
	_ AssetTypeDetector = (*ClaudeCodePluginDetector)(nil)
	_ UsageDetector     = (*ClaudeCodePluginDetector)(nil)
)

// DetectType returns true if files indicate this is a Claude Code plugin asset
func (d *ClaudeCodePluginDetector) DetectType(files []string) bool {
	return slices.Contains(files, ".claude-plugin/plugin.json")
}

// GetType returns the asset type string
func (d *ClaudeCodePluginDetector) GetType() string {
	return "claude-code-plugin"
}

// CreateDefaultMetadata creates default metadata for a Claude Code plugin
func (d *ClaudeCodePluginDetector) CreateDefaultMetadata(name, version string) *metadata.Metadata {
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

// DetectUsageFromToolCall detects plugin usage from tool calls
// Plugins are not directly invoked - their contents (commands, skills, etc.) are
func (d *ClaudeCodePluginDetector) DetectUsageFromToolCall(toolName string, toolInput map[string]any) (string, bool) {
	return "", false
}
