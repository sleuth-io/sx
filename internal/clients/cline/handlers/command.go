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

// DirWorkflows is the directory for Cline workflows (slash commands)
const DirWorkflows = "workflows"

// CommandHandler handles command asset installation for Cline.
// Commands are installed as Cline "workflows" which appear as slash commands.
// - Global scope: ~/Documents/Cline/Workflows/{name}.md
// - Project scope: .clinerules/workflows/{name}.md
type CommandHandler struct {
	metadata *metadata.Metadata
}

// NewCommandHandler creates a new command handler
func NewCommandHandler(meta *metadata.Metadata) *CommandHandler {
	return &CommandHandler{metadata: meta}
}

// Install installs a command as a Cline workflow
func (h *CommandHandler) Install(ctx context.Context, zipData []byte, targetBase string) error {
	workflowsDir, err := h.determineWorkflowsDir(targetBase)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		return fmt.Errorf("failed to create workflows directory: %w", err)
	}

	// Get prompt file from metadata
	promptFile := h.getPromptFile()
	if promptFile == "" {
		return errors.New("no prompt file specified in metadata")
	}

	// Read prompt file from zip
	promptContent, err := utils.ReadZipFile(zipData, promptFile)
	if err != nil {
		return fmt.Errorf("failed to read prompt file: %w", err)
	}

	// Write to workflows directory as {name}.md
	// Cline invokes workflows with /name.md
	destPath := filepath.Join(workflowsDir, h.metadata.Asset.Name+".md")
	if err := os.WriteFile(destPath, promptContent, 0644); err != nil {
		return fmt.Errorf("failed to write workflow file: %w", err)
	}

	return nil
}

// Remove removes a workflow from Cline
func (h *CommandHandler) Remove(ctx context.Context, targetBase string) error {
	workflowsDir, err := h.determineWorkflowsDir(targetBase)
	if err != nil {
		return err
	}

	workflowFile := filepath.Join(workflowsDir, h.metadata.Asset.Name+".md")
	if err := os.Remove(workflowFile); err != nil {
		if os.IsNotExist(err) {
			return nil // Already removed
		}
		return fmt.Errorf("failed to remove workflow file: %w", err)
	}
	return nil
}

// VerifyInstalled checks if the workflow is properly installed
func (h *CommandHandler) VerifyInstalled(targetBase string) (bool, string) {
	workflowsDir, err := h.determineWorkflowsDir(targetBase)
	if err != nil {
		return false, err.Error()
	}

	workflowFile := filepath.Join(workflowsDir, h.metadata.Asset.Name+".md")
	if !utils.FileExists(workflowFile) {
		return false, "workflow file not found"
	}
	return true, "installed"
}

// determineWorkflowsDir returns the workflows directory based on targetBase.
// - If targetBase is ~/.cline (global), use ~/Documents/Cline/Workflows/
// - If targetBase is {repo}/.cline (project), use {repo}/.clinerules/workflows/
func (h *CommandHandler) determineWorkflowsDir(targetBase string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	globalClineDir := filepath.Join(home, ConfigDir)

	// Check if this is global scope (targetBase == ~/.cline)
	if targetBase == globalClineDir {
		// Global workflows go to ~/Documents/Cline/Workflows/
		return filepath.Join(home, "Documents", "Cline", "Workflows"), nil
	}

	// Project scope: targetBase is {repo}/.cline or {repo}/{path}/.cline
	// Workflows go to sibling .clinerules/workflows/ directory
	parentDir := filepath.Dir(targetBase)
	return filepath.Join(parentDir, RulesDir, DirWorkflows), nil
}

func (h *CommandHandler) getPromptFile() string {
	// Check both Command and Skill metadata sections
	if h.metadata.Command != nil && h.metadata.Command.PromptFile != "" {
		return h.metadata.Command.PromptFile
	}
	if h.metadata.Skill != nil && h.metadata.Skill.PromptFile != "" {
		return h.metadata.Skill.PromptFile
	}
	return ""
}
