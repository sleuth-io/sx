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

// CommandHandler installs custom slash commands into OpenCode.
// OpenCode loads commands as markdown files from <config>/commands/<name>.md.
type CommandHandler struct {
	metadata *metadata.Metadata
}

// NewCommandHandler creates a new OpenCode command handler.
func NewCommandHandler(meta *metadata.Metadata) *CommandHandler {
	return &CommandHandler{metadata: meta}
}

// Install writes the command markdown file to <targetBase>/commands/<name>.md.
func (h *CommandHandler) Install(ctx context.Context, zipData []byte, targetBase string) error {
	commandsDir := filepath.Join(targetBase, DirCommands)
	if err := os.MkdirAll(commandsDir, 0755); err != nil {
		return fmt.Errorf("failed to create commands directory: %w", err)
	}

	promptFile := h.getPromptFile()
	if promptFile == "" {
		return errors.New("no prompt file specified in metadata")
	}

	content, err := utils.ReadZipFile(zipData, promptFile)
	if err != nil {
		return fmt.Errorf("failed to read prompt file: %w", err)
	}

	destPath := filepath.Join(commandsDir, h.metadata.Asset.Name+".md")
	if err := os.WriteFile(destPath, content, 0644); err != nil {
		return fmt.Errorf("failed to write command file: %w", err)
	}

	return nil
}

// Remove deletes the command markdown file from <targetBase>/commands/.
func (h *CommandHandler) Remove(ctx context.Context, targetBase string) error {
	filePath := filepath.Join(targetBase, DirCommands, h.metadata.Asset.Name+".md")
	if err := os.Remove(filePath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to remove command file: %w", err)
	}
	return nil
}

// VerifyInstalled returns whether the command markdown file exists.
func (h *CommandHandler) VerifyInstalled(targetBase string) (bool, string) {
	filePath := filepath.Join(targetBase, DirCommands, h.metadata.Asset.Name+".md")
	if !utils.FileExists(filePath) {
		return false, "command file not found"
	}
	return true, "installed"
}

func (h *CommandHandler) getPromptFile() string {
	if h.metadata.Command != nil && h.metadata.Command.PromptFile != "" {
		return h.metadata.Command.PromptFile
	}
	return DefaultCommandPromptFile
}
