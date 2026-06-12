package handlers

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/metadata"
	"github.com/sleuth-io/sx/internal/utils"
)

// AgentHandler installs Codex custom agent definitions as standalone TOML files.
type AgentHandler struct {
	metadata *metadata.Metadata
}

// NewAgentHandler creates a new Codex agent handler.
func NewAgentHandler(meta *metadata.Metadata) *AgentHandler {
	return &AgentHandler{metadata: meta}
}

// Install writes the agent TOML file to {targetBase}/agents/{name}.toml.
func (h *AgentHandler) Install(ctx context.Context, zipData []byte, targetBase string) error {
	if err := metadata.ValidateZip(zipData, &asset.TypeAgent); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	promptFile := h.getPromptFile()
	if promptFile == "" {
		return errors.New("no agent TOML file specified in metadata")
	}

	content, err := utils.ReadZipFile(zipData, promptFile)
	if err != nil {
		return fmt.Errorf("failed to read agent TOML file: %w", err)
	}

	agentsDir := filepath.Join(targetBase, DirAgents)
	if err := utils.EnsureDir(agentsDir); err != nil {
		return fmt.Errorf("failed to create agent directory: %w", err)
	}

	if err := os.WriteFile(h.agentPath(targetBase), content, 0644); err != nil {
		return fmt.Errorf("failed to write agent TOML file: %w", err)
	}

	return nil
}

// Remove deletes the Codex custom agent TOML file.
func (h *AgentHandler) Remove(ctx context.Context, targetBase string) error {
	if err := os.Remove(h.agentPath(targetBase)); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to remove agent TOML file: %w", err)
	}
	return nil
}

// VerifyInstalled returns whether the Codex custom agent TOML file exists.
func (h *AgentHandler) VerifyInstalled(targetBase string) (bool, string) {
	if !utils.FileExists(h.agentPath(targetBase)) {
		return false, "agent TOML file not found"
	}
	return true, "installed"
}

func (h *AgentHandler) getPromptFile() string {
	if h.metadata.Agent != nil && h.metadata.Agent.PromptFile != "" {
		return h.metadata.Agent.PromptFile
	}
	return h.metadata.Asset.Name + ".toml"
}

func (h *AgentHandler) agentPath(targetBase string) string {
	return filepath.Join(targetBase, DirAgents, h.metadata.Asset.Name+".toml")
}
