package handlers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sleuth-io/sx/internal/metadata"
	"github.com/sleuth-io/sx/internal/utils"
)

// AgentHandler handles agent asset installation for GitHub Copilot.
// Agents are written to agents/{name}.agent.md with YAML frontmatter.
type AgentHandler struct {
	metadata *metadata.Metadata
}

// NewAgentHandler creates a new agent handler
func NewAgentHandler(meta *metadata.Metadata) *AgentHandler {
	return &AgentHandler{metadata: meta}
}

// Install writes the agent as an .agent.md file to {targetBase}/agents/
func (h *AgentHandler) Install(ctx context.Context, zipData []byte, targetBase string) error {
	// Read agent content from zip
	content, err := h.readAgentContent(zipData)
	if err != nil {
		return fmt.Errorf("failed to read agent content: %w", err)
	}

	// Ensure agents directory exists
	agentsDir := filepath.Join(targetBase, "agents")
	if err := utils.EnsureDir(agentsDir); err != nil {
		return fmt.Errorf("failed to create agents directory: %w", err)
	}

	// Build agent file content with frontmatter
	agentContent := h.buildAgentContent(content)

	// Write to agents/{name}.agent.md
	filePath := filepath.Join(agentsDir, h.metadata.Asset.Name+".agent.md")
	if err := os.WriteFile(filePath, []byte(agentContent), 0644); err != nil {
		return fmt.Errorf("failed to write agent file: %w", err)
	}

	return nil
}

// Remove removes the agent file
func (h *AgentHandler) Remove(ctx context.Context, targetBase string) error {
	filePath := filepath.Join(targetBase, "agents", h.metadata.Asset.Name+".agent.md")

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return nil // Already removed
	}

	if err := os.Remove(filePath); err != nil {
		return fmt.Errorf("failed to remove agent file: %w", err)
	}

	return nil
}

// VerifyInstalled checks if the agent file exists
func (h *AgentHandler) VerifyInstalled(targetBase string) (bool, string) {
	filePath := filepath.Join(targetBase, "agents", h.metadata.Asset.Name+".agent.md")

	if _, err := os.Stat(filePath); err == nil {
		return true, "Found at " + filePath
	}

	return false, "Agent file not found"
}

// buildAgentContent creates the agent file with YAML frontmatter.
func (h *AgentHandler) buildAgentContent(content string) string {
	var sb strings.Builder

	description := h.getDescription()

	// Only add frontmatter if there's a description
	if description != "" {
		sb.WriteString("---\n")
		sb.WriteString(fmt.Sprintf("description: %s\n", description))
		sb.WriteString("---\n\n")
	}

	// Content
	sb.WriteString(strings.TrimSpace(content))
	sb.WriteString("\n")

	return sb.String()
}

// getDescription returns the description for frontmatter
func (h *AgentHandler) getDescription() string {
	return h.metadata.Asset.Description
}

// getPromptFile returns the prompt file name
func (h *AgentHandler) getPromptFile() string {
	if h.metadata.Agent != nil && h.metadata.Agent.PromptFile != "" {
		return h.metadata.Agent.PromptFile
	}
	return "AGENT.md"
}

// readAgentContent reads the agent content from the zip
func (h *AgentHandler) readAgentContent(zipData []byte) (string, error) {
	promptFile := h.getPromptFile()

	content, err := utils.ReadZipFile(zipData, promptFile)
	if err != nil {
		// Try lowercase variant
		content, err = utils.ReadZipFile(zipData, "agent.md")
		if err != nil {
			return "", fmt.Errorf("prompt file not found: %s", promptFile)
		}
	}

	return string(content), nil
}
