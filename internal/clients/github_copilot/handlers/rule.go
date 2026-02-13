package handlers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sleuth-io/sx/internal/handlers/rule"
	"github.com/sleuth-io/sx/internal/metadata"
	"github.com/sleuth-io/sx/internal/utils"
)

// RuleHandler handles rule asset installation for GitHub Copilot.
// Rules are written to instructions/{name}.instructions.md with YAML frontmatter
// using Copilot's applyTo field for glob patterns.
type RuleHandler struct {
	metadata *metadata.Metadata
}

// NewRuleHandler creates a new rule handler
func NewRuleHandler(meta *metadata.Metadata) *RuleHandler {
	return &RuleHandler{metadata: meta}
}

// Install writes the rule as an .instructions.md file to {targetBase}/instructions/
func (h *RuleHandler) Install(ctx context.Context, zipData []byte, targetBase string) error {
	// Read rule content from zip
	content, err := h.readRuleContent(zipData)
	if err != nil {
		return fmt.Errorf("failed to read rule content: %w", err)
	}

	// Ensure instructions directory exists
	instructionsDir := filepath.Join(targetBase, DirInstructions)
	if err := utils.EnsureDir(instructionsDir); err != nil {
		return fmt.Errorf("failed to create instructions directory: %w", err)
	}

	// Build instruction file content with frontmatter
	instructionContent := h.buildInstructionContent(content)

	// Write to instructions/{name}.instructions.md
	filePath := filepath.Join(instructionsDir, h.metadata.Asset.Name+".instructions.md")
	if err := os.WriteFile(filePath, []byte(instructionContent), 0644); err != nil {
		return fmt.Errorf("failed to write instruction file: %w", err)
	}

	return nil
}

// Remove removes the instruction file
func (h *RuleHandler) Remove(ctx context.Context, targetBase string) error {
	filePath := filepath.Join(targetBase, DirInstructions, h.metadata.Asset.Name+".instructions.md")

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return nil // Already removed
	}

	if err := os.Remove(filePath); err != nil {
		return fmt.Errorf("failed to remove instruction file: %w", err)
	}

	return nil
}

// VerifyInstalled checks if the instruction file exists
func (h *RuleHandler) VerifyInstalled(targetBase string) (bool, string) {
	filePath := filepath.Join(targetBase, DirInstructions, h.metadata.Asset.Name+".instructions.md")

	if _, err := os.Stat(filePath); err == nil {
		return true, "Found at " + filePath
	}

	return false, "Instruction file not found"
}

// buildInstructionContent creates the instruction file with YAML frontmatter.
// Uses Copilot's applyTo field for glob patterns.
func (h *RuleHandler) buildInstructionContent(content string) string {
	var sb strings.Builder

	// Write frontmatter
	sb.WriteString("---\n")

	// applyTo (Copilot's equivalent of Claude's paths / Cursor's globs)
	globs := h.getGlobs()
	if len(globs) > 0 {
		// Copilot uses comma-separated globs in a single applyTo string
		sb.WriteString(fmt.Sprintf("applyTo: \"%s\"\n", strings.Join(globs, ",")))
	}

	// Description
	description := h.getDescription()
	if description != "" {
		sb.WriteString(fmt.Sprintf("description: %s\n", description))
	}

	sb.WriteString("---\n\n")

	// Title as heading
	title := h.getTitle()
	if title != "" && !strings.HasPrefix(strings.TrimSpace(content), "#") {
		sb.WriteString("# ")
		sb.WriteString(title)
		sb.WriteString("\n\n")
	}

	// Content
	sb.WriteString(strings.TrimSpace(content))
	sb.WriteString("\n")

	return sb.String()
}

// getTitle returns the rule title, defaulting to asset name
func (h *RuleHandler) getTitle() string {
	if h.metadata.Rule != nil && h.metadata.Rule.Title != "" {
		return h.metadata.Rule.Title
	}
	return h.metadata.Asset.Name
}

// getDescription returns the description for frontmatter
func (h *RuleHandler) getDescription() string {
	if h.metadata.Rule != nil && h.metadata.Rule.Description != "" {
		return h.metadata.Rule.Description
	}
	return h.metadata.Asset.Description
}

// getGlobs returns the globs from rule metadata
func (h *RuleHandler) getGlobs() []string {
	if h.metadata.Rule != nil && len(h.metadata.Rule.Globs) > 0 {
		return h.metadata.Rule.Globs
	}
	return nil
}

// getPromptFile returns the prompt file name, defaulting to RULE.md
func (h *RuleHandler) getPromptFile() string {
	if h.metadata.Rule != nil && h.metadata.Rule.PromptFile != "" {
		return h.metadata.Rule.PromptFile
	}
	return rule.DefaultPromptFile
}

// readRuleContent reads the rule content from the zip
func (h *RuleHandler) readRuleContent(zipData []byte) (string, error) {
	promptFile := h.getPromptFile()

	content, err := utils.ReadZipFile(zipData, promptFile)
	if err != nil {
		// Try lowercase variant
		content, err = utils.ReadZipFile(zipData, "rule.md")
		if err != nil {
			return "", fmt.Errorf("prompt file not found: %s", promptFile)
		}
	}

	return string(content), nil
}
