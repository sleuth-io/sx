package detectors

import (
	"strings"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/metadata"
)

// CommandDetector detects command assets
type CommandDetector struct{}

// Compile-time interface checks
var (
	_ AssetTypeDetector = (*CommandDetector)(nil)
	_ UsageDetector     = (*CommandDetector)(nil)
)

// DetectType returns true if files indicate this is a command asset
func (h *CommandDetector) DetectType(files []string) bool {
	for _, file := range files {
		if file == "COMMAND.md" || file == "command.md" {
			return true
		}
	}
	return false
}

// GetType returns the asset type string
func (h *CommandDetector) GetType() string {
	return "command"
}

// CreateDefaultMetadata creates default metadata for a command
func (h *CommandDetector) CreateDefaultMetadata(name, version string) *metadata.Metadata {
	return &metadata.Metadata{
		MetadataVersion: "1.0",
		Asset: metadata.Asset{
			Name:    name,
			Version: version,
			Type:    asset.TypeCommand,
		},
		Command: &metadata.CommandConfig{
			PromptFile: "COMMAND.md",
		},
	}
}

// DetectUsageFromToolCall detects command usage from tool calls
func (h *CommandDetector) DetectUsageFromToolCall(toolName string, toolInput map[string]any) (string, bool) {
	if toolName != "SlashCommand" {
		return "", false
	}
	command, ok := toolInput["command"].(string)
	if !ok {
		return "", false
	}
	// Strip leading slash: "/my-command" -> "my-command"
	commandName := strings.TrimPrefix(command, "/")
	return commandName, true
}
