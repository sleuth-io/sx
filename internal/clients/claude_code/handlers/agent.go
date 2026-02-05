package handlers

import (
	"context"
	"errors"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/handlers/fileasset"
	"github.com/sleuth-io/sx/internal/metadata"
)

var agentOps = fileasset.NewOperations("agents", &asset.TypeAgent)

// AgentHandler handles agent asset installation
type AgentHandler struct {
	metadata *metadata.Metadata
}

// NewAgentHandler creates a new agent handler
func NewAgentHandler(meta *metadata.Metadata) *AgentHandler {
	return &AgentHandler{
		metadata: meta,
	}
}

// DetectType returns true if files indicate this is an agent asset
func (h *AgentHandler) DetectType(files []string) bool {
	for _, file := range files {
		if file == "AGENT.md" || file == "agent.md" {
			return true
		}
	}
	return false
}

// GetType returns the asset type string
func (h *AgentHandler) GetType() string {
	return "agent"
}

// CreateDefaultMetadata creates default metadata for an agent
func (h *AgentHandler) CreateDefaultMetadata(name, version string) *metadata.Metadata {
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

// GetPromptFile returns the prompt file path for agents
func (h *AgentHandler) GetPromptFile(meta *metadata.Metadata) string {
	if meta.Agent != nil {
		return meta.Agent.PromptFile
	}
	return ""
}

// GetScriptFile returns empty for agents (not applicable)
func (h *AgentHandler) GetScriptFile(meta *metadata.Metadata) string {
	return ""
}

// ValidateMetadata validates agent-specific metadata
func (h *AgentHandler) ValidateMetadata(meta *metadata.Metadata) error {
	if meta.Agent == nil {
		return errors.New("agent configuration missing")
	}
	if meta.Agent.PromptFile == "" {
		return errors.New("agent prompt-file is required")
	}
	return nil
}

// DetectUsageFromToolCall detects agent usage from tool calls
func (h *AgentHandler) DetectUsageFromToolCall(toolName string, toolInput map[string]any) (string, bool) {
	if toolName != "Task" {
		return "", false
	}
	agentName, ok := toolInput["subagent_type"].(string)
	return agentName, ok
}

// Install extracts and installs the agent asset as a single .md file
func (h *AgentHandler) Install(ctx context.Context, zipData []byte, targetBase string) error {
	return agentOps.Install(ctx, zipData, targetBase, h.metadata.Asset.Name, h.metadata.Agent.PromptFile)
}

// Remove uninstalls the agent asset
func (h *AgentHandler) Remove(ctx context.Context, targetBase string) error {
	return agentOps.Remove(ctx, targetBase, h.metadata.Asset.Name)
}

// GetInstallPath returns the installation path relative to targetBase
func (h *AgentHandler) GetInstallPath() string {
	return agentOps.GetInstallPath(h.metadata.Asset.Name)
}

// CanDetectInstalledState returns true since agents preserve metadata via adjacent files
func (h *AgentHandler) CanDetectInstalledState() bool {
	return true
}

// VerifyInstalled checks if the agent is properly installed
func (h *AgentHandler) VerifyInstalled(targetBase string) (bool, string) {
	return agentOps.VerifyInstalled(targetBase, h.metadata.Asset.Name, h.metadata.Asset.Version)
}
