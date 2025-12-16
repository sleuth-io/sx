package handlers

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/sleuth-io/skills/internal/asset"
	"github.com/sleuth-io/skills/internal/handlers/dirasset"
	"github.com/sleuth-io/skills/internal/metadata"
	"github.com/sleuth-io/skills/internal/utils"
)

var agentOps = dirasset.NewOperations("agents", &asset.TypeAgent)

// AgentHandler handles agent artifact installation
type AgentHandler struct {
	metadata *metadata.Metadata
}

// NewAgentHandler creates a new agent handler
func NewAgentHandler(meta *metadata.Metadata) *AgentHandler {
	return &AgentHandler{
		metadata: meta,
	}
}

// DetectType returns true if files indicate this is an agent artifact
func (h *AgentHandler) DetectType(files []string) bool {
	for _, file := range files {
		if file == "AGENT.md" || file == "agent.md" {
			return true
		}
	}
	return false
}

// GetType returns the artifact type string
func (h *AgentHandler) GetType() string {
	return "agent"
}

// CreateDefaultMetadata creates default metadata for an agent
func (h *AgentHandler) CreateDefaultMetadata(name, version string) *metadata.Metadata {
	return &metadata.Metadata{
		MetadataVersion: "1.0",
		Artifact: metadata.Artifact{
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
		return fmt.Errorf("agent configuration missing")
	}
	if meta.Agent.PromptFile == "" {
		return fmt.Errorf("agent prompt-file is required")
	}
	return nil
}

// DetectUsageFromToolCall detects agent usage from tool calls
func (h *AgentHandler) DetectUsageFromToolCall(toolName string, toolInput map[string]interface{}) (string, bool) {
	if toolName != "Task" {
		return "", false
	}
	agentName, ok := toolInput["subagent_type"].(string)
	return agentName, ok
}

// Install extracts and installs the agent artifact
func (h *AgentHandler) Install(ctx context.Context, zipData []byte, targetBase string) error {
	// Validate zip structure
	if err := h.Validate(zipData); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	return agentOps.Install(ctx, zipData, targetBase, h.metadata.Artifact.Name)
}

// Remove uninstalls the agent artifact
func (h *AgentHandler) Remove(ctx context.Context, targetBase string) error {
	return agentOps.Remove(ctx, targetBase, h.metadata.Artifact.Name)
}

// GetInstallPath returns the installation path relative to targetBase
func (h *AgentHandler) GetInstallPath() string {
	return filepath.Join("agents", h.metadata.Artifact.Name)
}

// Validate checks if the zip structure is valid for an agent artifact
func (h *AgentHandler) Validate(zipData []byte) error {
	// List files in zip
	files, err := utils.ListZipFiles(zipData)
	if err != nil {
		return fmt.Errorf("failed to list zip files: %w", err)
	}

	// Check that metadata.toml exists
	if !containsFile(files, "metadata.toml") {
		return fmt.Errorf("metadata.toml not found in zip")
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

	// Verify artifact type matches
	if meta.Artifact.Type != asset.TypeAgent {
		return fmt.Errorf("artifact type mismatch: expected agent, got %s", meta.Artifact.Type)
	}

	// Check that prompt file exists
	if meta.Agent == nil {
		return fmt.Errorf("[agent] section missing in metadata")
	}

	if !containsFile(files, meta.Agent.PromptFile) {
		return fmt.Errorf("prompt file not found in zip: %s", meta.Agent.PromptFile)
	}

	return nil
}

// CanDetectInstalledState returns true since agents preserve metadata.toml
func (h *AgentHandler) CanDetectInstalledState() bool {
	return true
}

// VerifyInstalled checks if the agent is properly installed
func (h *AgentHandler) VerifyInstalled(targetBase string) (bool, string) {
	return agentOps.VerifyInstalled(targetBase, h.metadata.Artifact.Name, h.metadata.Artifact.Version)
}
