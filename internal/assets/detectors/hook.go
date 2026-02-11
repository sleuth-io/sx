package detectors

import (
	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/metadata"
)

// HookDetector detects hook assets
type HookDetector struct{}

// Compile-time interface checks
var (
	_ AssetTypeDetector = (*HookDetector)(nil)
	_ UsageDetector     = (*HookDetector)(nil)
)

// DetectType returns true if files indicate this is a hook asset
func (h *HookDetector) DetectType(files []string) bool {
	for _, file := range files {
		if file == "hook.sh" || file == "hook.py" || file == "hook.js" {
			return true
		}
	}
	return false
}

// GetType returns the asset type string
func (h *HookDetector) GetType() string {
	return "hook"
}

// CreateDefaultMetadata creates default metadata for a hook
func (h *HookDetector) CreateDefaultMetadata(name, version string) *metadata.Metadata {
	return &metadata.Metadata{
		MetadataVersion: "1.0",
		Asset: metadata.Asset{
			Name:    name,
			Version: version,
			Type:    asset.TypeHook,
		},
		Hook: &metadata.HookConfig{
			Event:      "pre-tool-use",
			ScriptFile: "hook.sh",
		},
	}
}

// DetectUsageFromToolCall detects hook usage from tool calls
// Hooks are not detectable from tool usage, so this always returns false
func (h *HookDetector) DetectUsageFromToolCall(toolName string, toolInput map[string]any) (string, bool) {
	return "", false
}
