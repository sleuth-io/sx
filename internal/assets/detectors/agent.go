package detectors

import (
	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/metadata"
)

// AgentDetector detects agent assets
type AgentDetector struct{}

// Compile-time interface checks
var (
	_ AssetTypeDetector = (*AgentDetector)(nil)
	_ UsageDetector     = (*AgentDetector)(nil)
)

// DetectType returns true if files indicate this is an agent asset
func (h *AgentDetector) DetectType(files []string) bool {
	for _, file := range files {
		if file == "AGENT.md" || file == "agent.md" {
			return true
		}
	}
	return false
}

// GetType returns the asset type string
func (h *AgentDetector) GetType() string {
	return "agent"
}

// CreateDefaultMetadata creates default metadata for an agent
func (h *AgentDetector) CreateDefaultMetadata(name, version string) *metadata.Metadata {
	return &metadata.Metadata{
		MetadataVersion: "1.0",
		Asset: metadata.Asset{
			Name:    name,
			Version: version,
			Type:    asset.TypeAgent,
		},
		Agent: &metadata.AgentConfig{
			PromptFile: "AGENT.md",
		},
	}
}

// DetectUsageFromToolCall detects agent usage from tool calls
func (h *AgentDetector) DetectUsageFromToolCall(toolName string, toolInput map[string]any) (string, bool) {
	if toolName != "Task" {
		return "", false
	}
	agentName, ok := toolInput["subagent_type"].(string)
	return agentName, ok
}
