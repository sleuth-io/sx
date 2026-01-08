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

// CommandHandler handles command/skill installation for Cursor
type CommandHandler struct {
	metadata *metadata.Metadata
}

// NewCommandHandler creates a new command handler
func NewCommandHandler(meta *metadata.Metadata) *CommandHandler {
	return &CommandHandler{metadata: meta}
}

// Install installs a command/skill as a Cursor slash command
func (h *CommandHandler) Install(ctx context.Context, zipData []byte, targetBase string) error {
	commandsDir := filepath.Join(targetBase, "commands")
	if err := os.MkdirAll(commandsDir, 0755); err != nil {
		return fmt.Errorf("failed to create commands directory: %w", err)
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

	// Write to .cursor/commands/{name}.md
	destPath := filepath.Join(commandsDir, h.metadata.Asset.Name+".md")
	if err := os.WriteFile(destPath, promptContent, 0644); err != nil {
		return fmt.Errorf("failed to write command file: %w", err)
	}

	return nil
}

// Remove removes a slash command from Cursor
func (h *CommandHandler) Remove(ctx context.Context, targetBase string) error {
	commandFile := filepath.Join(targetBase, "commands", h.metadata.Asset.Name+".md")
	if err := os.Remove(commandFile); err != nil {
		if os.IsNotExist(err) {
			return nil // Already removed
		}
		return fmt.Errorf("failed to remove command file: %w", err)
	}
	return nil
}

func (h *CommandHandler) getPromptFile() string {
	// Check both Skill and Command metadata sections (for skill → command transformation)
	if h.metadata.Skill != nil && h.metadata.Skill.PromptFile != "" {
		return h.metadata.Skill.PromptFile
	}
	if h.metadata.Command != nil && h.metadata.Command.PromptFile != "" {
		return h.metadata.Command.PromptFile
	}
	return ""
}

// VerifyInstalled checks if the command is properly installed
func (h *CommandHandler) VerifyInstalled(targetBase string) (bool, string) {
	commandFile := filepath.Join(targetBase, "commands", h.metadata.Asset.Name+".md")
	if !utils.FileExists(commandFile) {
		return false, "command file not found"
	}
	// Cursor commands don't have version tracking
	return true, "installed"
}
