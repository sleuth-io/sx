package detectors

import (
	"strings"

	"github.com/sleuth-io/skills/internal/asset"
	"github.com/sleuth-io/skills/internal/metadata"
)

// CommandHandler handles command artifact installation
type CommandDetector struct{}

// Compile-time interface checks
var (
	_ ArtifactTypeDetector = (*CommandDetector)(nil)
	_ UsageDetector        = (*CommandDetector)(nil)
)

// DetectType returns true if files indicate this is a command artifact
func (h *CommandDetector) DetectType(files []string) bool {
	for _, file := range files {
		if file == "COMMAND.md" || file == "command.md" {
			return true
		}
	}
	return false
}

// GetType returns the artifact type string
func (h *CommandDetector) GetType() string {
	return "command"
}

// CreateDefaultMetadata creates default metadata for a command
func (h *CommandDetector) CreateDefaultMetadata(name, version string) *metadata.Metadata {
	return &metadata.Metadata{
		MetadataVersion: "1.0",
		Artifact: metadata.Artifact{
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
func (h *CommandDetector) DetectUsageFromToolCall(toolName string, toolInput map[string]interface{}) (string, bool) {
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
