package handlers

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sleuth-io/sx/internal/metadata"
	"github.com/sleuth-io/sx/internal/utils"
)

// AgentHandler installs OpenCode agents as markdown files.
// OpenCode loads agents from <config>/agent/<name>.md (and also from the
// plural `agents/` directory, but agent/ is canonical per the built-in
// customize-opencode skill).
type AgentHandler struct {
	metadata *metadata.Metadata
}

// NewAgentHandler creates a new OpenCode agent handler.
func NewAgentHandler(meta *metadata.Metadata) *AgentHandler {
	return &AgentHandler{metadata: meta}
}

// Install writes the agent markdown file to <targetBase>/agent/<name>.md.
func (h *AgentHandler) Install(ctx context.Context, zipData []byte, targetBase string) error {
	agentsDir := filepath.Join(targetBase, DirAgents)
	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		return fmt.Errorf("failed to create agent directory: %w", err)
	}

	promptFile := h.getPromptFile()
	if promptFile == "" {
		return errors.New("no prompt file specified in metadata")
	}

	content, err := utils.ReadZipFile(zipData, promptFile)
	if err != nil {
		return fmt.Errorf("failed to read prompt file: %w", err)
	}

	destPath := filepath.Join(agentsDir, h.metadata.Asset.Name+".md")
	if err := os.WriteFile(destPath, content, 0644); err != nil {
		return fmt.Errorf("failed to write agent file: %w", err)
	}

	return nil
}

// Remove deletes the agent markdown file from <targetBase>/agent/.
func (h *AgentHandler) Remove(ctx context.Context, targetBase string) error {
	filePath := filepath.Join(targetBase, DirAgents, h.metadata.Asset.Name+".md")
	if err := os.Remove(filePath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to remove agent file: %w", err)
	}
	return nil
}

// VerifyInstalled returns whether the agent markdown file exists.
func (h *AgentHandler) VerifyInstalled(targetBase string) (bool, string) {
	filePath := filepath.Join(targetBase, DirAgents, h.metadata.Asset.Name+".md")
	if !utils.FileExists(filePath) {
		return false, "agent file not found"
	}
	return true, "installed"
}

func (h *AgentHandler) getPromptFile() string {
	if h.metadata.Agent != nil && h.metadata.Agent.PromptFile != "" {
		return h.metadata.Agent.PromptFile
	}
	return DefaultAgentPromptFile
}
