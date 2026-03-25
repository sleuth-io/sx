package handlers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sleuth-io/sx/internal/metadata"
	"github.com/sleuth-io/sx/internal/utils"
)

// CommandHandler handles command installation for Kiro CLI.
// Commands are stored as plain markdown files in .kiro/prompts/{name}.md
// and invoked with @name in the Kiro CLI chat.
type CommandHandler struct {
	metadata *metadata.Metadata
}

// NewCommandHandler creates a new command handler
func NewCommandHandler(meta *metadata.Metadata) *CommandHandler {
	return &CommandHandler{metadata: meta}
}

// Install writes the command as a .md file to .kiro/prompts/
func (h *CommandHandler) Install(ctx context.Context, zipData []byte, targetBase string) error {
	promptsDir := filepath.Join(targetBase, DirPrompts)
	if err := os.MkdirAll(promptsDir, 0755); err != nil {
		return fmt.Errorf("failed to create prompts directory: %w", err)
	}

	promptFile := h.getPromptFile()
	content, err := utils.ReadZipFile(zipData, promptFile)
	if err != nil {
		return fmt.Errorf("failed to read prompt file: %w", err)
	}

	destPath := filepath.Join(promptsDir, h.metadata.Asset.Name+".md")
	if err := os.WriteFile(destPath, content, 0644); err != nil {
		return fmt.Errorf("failed to write command file: %w", err)
	}

	return nil
}

// Remove removes the command from .kiro/prompts/
func (h *CommandHandler) Remove(ctx context.Context, targetBase string) error {
	filePath := filepath.Join(targetBase, DirPrompts, h.metadata.Asset.Name+".md")
	if err := os.Remove(filePath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to remove command file: %w", err)
	}
	return nil
}

// VerifyInstalled checks if the command file exists
func (h *CommandHandler) VerifyInstalled(targetBase string) (bool, string) {
	filePath := filepath.Join(targetBase, DirPrompts, h.metadata.Asset.Name+".md")
	if !utils.FileExists(filePath) {
		return false, "command file not found"
	}
	return true, "installed"
}

func (h *CommandHandler) getPromptFile() string {
	if h.metadata.Command != nil && h.metadata.Command.PromptFile != "" {
		return h.metadata.Command.PromptFile
	}
	return "COMMAND.md"
}
