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

// CommandHandler handles command asset installation for GitHub Copilot.
// Commands are written to prompts/{name}.prompt.md with optional YAML frontmatter.
type CommandHandler struct {
	metadata *metadata.Metadata
}

// NewCommandHandler creates a new command handler
func NewCommandHandler(meta *metadata.Metadata) *CommandHandler {
	return &CommandHandler{metadata: meta}
}

// Install writes the command as a .prompt.md file to {targetBase}/prompts/
func (h *CommandHandler) Install(ctx context.Context, zipData []byte, targetBase string) error {
	// Read prompt content from zip
	content, err := h.readPromptContent(zipData)
	if err != nil {
		return fmt.Errorf("failed to read prompt content: %w", err)
	}

	// Ensure prompts directory exists
	promptsDir := filepath.Join(targetBase, "prompts")
	if err := utils.EnsureDir(promptsDir); err != nil {
		return fmt.Errorf("failed to create prompts directory: %w", err)
	}

	// Build prompt file content with frontmatter
	promptContent := h.buildPromptContent(content)

	// Write to prompts/{name}.prompt.md
	filePath := filepath.Join(promptsDir, h.metadata.Asset.Name+".prompt.md")
	if err := os.WriteFile(filePath, []byte(promptContent), 0644); err != nil {
		return fmt.Errorf("failed to write prompt file: %w", err)
	}

	return nil
}

// Remove removes the prompt file
func (h *CommandHandler) Remove(ctx context.Context, targetBase string) error {
	filePath := filepath.Join(targetBase, "prompts", h.metadata.Asset.Name+".prompt.md")

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return nil // Already removed
	}

	if err := os.Remove(filePath); err != nil {
		return fmt.Errorf("failed to remove prompt file: %w", err)
	}

	return nil
}

// VerifyInstalled checks if the prompt file exists
func (h *CommandHandler) VerifyInstalled(targetBase string) (bool, string) {
	filePath := filepath.Join(targetBase, "prompts", h.metadata.Asset.Name+".prompt.md")

	if _, err := os.Stat(filePath); err == nil {
		return true, "Found at " + filePath
	}

	return false, "Prompt file not found"
}

// buildPromptContent creates the prompt file with optional YAML frontmatter.
func (h *CommandHandler) buildPromptContent(content string) string {
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
func (h *CommandHandler) getDescription() string {
	return h.metadata.Asset.Description
}

// getPromptFile returns the prompt file name
func (h *CommandHandler) getPromptFile() string {
	// Check Command metadata first
	if h.metadata.Command != nil && h.metadata.Command.PromptFile != "" {
		return h.metadata.Command.PromptFile
	}
	// Fall back to Skill metadata (for skill → command transformation)
	if h.metadata.Skill != nil && h.metadata.Skill.PromptFile != "" {
		return h.metadata.Skill.PromptFile
	}
	// Default
	return "COMMAND.md"
}

// readPromptContent reads the prompt content from the zip
func (h *CommandHandler) readPromptContent(zipData []byte) (string, error) {
	promptFile := h.getPromptFile()

	content, err := utils.ReadZipFile(zipData, promptFile)
	if err != nil {
		// Try lowercase variant
		content, err = utils.ReadZipFile(zipData, "command.md")
		if err != nil {
			return "", fmt.Errorf("prompt file not found: %s", promptFile)
		}
	}

	return string(content), nil
}
