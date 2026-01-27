package handlers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sleuth-io/sx/internal/handlers/instruction"
	"github.com/sleuth-io/sx/internal/metadata"
	"github.com/sleuth-io/sx/internal/utils"
)

// InstructionHandler handles instruction asset installation for Cursor
// Instructions are written to .cursor/rules/{name}.mdc with MDC frontmatter
type InstructionHandler struct {
	metadata *metadata.Metadata
	// pathScope is the path this instruction is scoped to (empty for repo-wide)
	pathScope string
}

// NewInstructionHandler creates a new instruction handler
func NewInstructionHandler(meta *metadata.Metadata, pathScope string) *InstructionHandler {
	return &InstructionHandler{
		metadata:  meta,
		pathScope: pathScope,
	}
}

// Install writes the instruction as an .mdc file to .cursor/rules/
func (h *InstructionHandler) Install(ctx context.Context, zipData []byte, targetBase string) error {
	// Read instruction content from zip
	content, err := h.readInstructionContent(zipData)
	if err != nil {
		return fmt.Errorf("failed to read instruction content: %w", err)
	}

	// Ensure rules directory exists
	rulesDir := filepath.Join(targetBase, "rules")
	if err := utils.EnsureDir(rulesDir); err != nil {
		return fmt.Errorf("failed to create rules directory: %w", err)
	}

	// Build MDC content with frontmatter
	mdcContent := h.buildMDCContent(content)

	// Write to .cursor/rules/{name}.mdc
	filePath := filepath.Join(rulesDir, h.metadata.Asset.Name+".mdc")
	if err := os.WriteFile(filePath, []byte(mdcContent), 0644); err != nil {
		return fmt.Errorf("failed to write rule file: %w", err)
	}

	return nil
}

// Remove removes the instruction .mdc file
func (h *InstructionHandler) Remove(ctx context.Context, targetBase string) error {
	filePath := filepath.Join(targetBase, "rules", h.metadata.Asset.Name+".mdc")

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return nil // Already removed
	}

	if err := os.Remove(filePath); err != nil {
		return fmt.Errorf("failed to remove rule file: %w", err)
	}

	return nil
}

// VerifyInstalled checks if the instruction .mdc file exists
func (h *InstructionHandler) VerifyInstalled(targetBase string) (bool, string) {
	filePath := filepath.Join(targetBase, "rules", h.metadata.Asset.Name+".mdc")

	if _, err := os.Stat(filePath); err == nil {
		return true, "Found at " + filePath
	}

	return false, "Rule file not found"
}

// buildMDCContent creates the MDC format content with frontmatter
func (h *InstructionHandler) buildMDCContent(content string) string {
	var sb strings.Builder

	// Write frontmatter
	sb.WriteString("---\n")

	// Description
	description := h.getDescription()
	if description != "" {
		sb.WriteString(fmt.Sprintf("description: %s\n", description))
	}

	// Globs or alwaysApply
	if h.shouldAlwaysApply() {
		sb.WriteString("alwaysApply: true\n")
	} else {
		globs := h.getGlobs()
		if len(globs) > 0 {
			// Single glob as string, multiple as array
			if len(globs) == 1 {
				sb.WriteString(fmt.Sprintf("globs: %s\n", globs[0]))
			} else {
				sb.WriteString("globs:\n")
				for _, glob := range globs {
					sb.WriteString(fmt.Sprintf("  - %s\n", glob))
				}
			}
		}
	}

	sb.WriteString("---\n\n")

	// Title as heading
	title := h.getTitle()
	sb.WriteString("# ")
	sb.WriteString(title)
	sb.WriteString("\n\n")

	// Content
	sb.WriteString(strings.TrimSpace(content))
	sb.WriteString("\n")

	return sb.String()
}

// getTitle returns the instruction title, defaulting to asset name
func (h *InstructionHandler) getTitle() string {
	if h.metadata.Instruction != nil && h.metadata.Instruction.Title != "" {
		return h.metadata.Instruction.Title
	}
	return h.metadata.Asset.Name
}

// getDescription returns the description for the MDC frontmatter
func (h *InstructionHandler) getDescription() string {
	// First check cursor-specific description
	if h.metadata.Instruction != nil && h.metadata.Instruction.Cursor != nil {
		if h.metadata.Instruction.Cursor.Description != "" {
			return h.metadata.Instruction.Cursor.Description
		}
	}
	// Fall back to asset description
	return h.metadata.Asset.Description
}

// shouldAlwaysApply returns true if the instruction should always apply
func (h *InstructionHandler) shouldAlwaysApply() bool {
	if h.metadata.Instruction != nil && h.metadata.Instruction.Cursor != nil {
		return h.metadata.Instruction.Cursor.AlwaysApply
	}
	// If no path scope and no explicit globs, default to always apply
	return h.pathScope == "" && len(h.getExplicitGlobs()) == 0
}

// getGlobs returns the globs for the MDC frontmatter
func (h *InstructionHandler) getGlobs() []string {
	// First check for explicit globs in cursor config
	explicitGlobs := h.getExplicitGlobs()
	if len(explicitGlobs) > 0 {
		return explicitGlobs
	}

	// Auto-generate from path scope
	if h.pathScope != "" {
		// Ensure path ends with /**/* for glob matching
		scope := strings.TrimSuffix(h.pathScope, "/")
		return []string{scope + "/**/*"}
	}

	return nil
}

// getExplicitGlobs returns explicitly configured globs
func (h *InstructionHandler) getExplicitGlobs() []string {
	if h.metadata.Instruction != nil && h.metadata.Instruction.Cursor != nil {
		return h.metadata.Instruction.Cursor.Globs
	}
	return nil
}

// getPromptFile returns the prompt file, using the shared default
func (h *InstructionHandler) getPromptFile() string {
	if h.metadata.Instruction != nil && h.metadata.Instruction.PromptFile != "" {
		return h.metadata.Instruction.PromptFile
	}
	return instruction.DefaultPromptFile
}

// readInstructionContent reads the instruction content from the zip
func (h *InstructionHandler) readInstructionContent(zipData []byte) (string, error) {
	promptFile := h.getPromptFile()

	content, err := utils.ReadZipFile(zipData, promptFile)
	if err != nil {
		// Try lowercase variant
		content, err = utils.ReadZipFile(zipData, "instruction.md")
		if err != nil {
			return "", fmt.Errorf("prompt file not found: %s", promptFile)
		}
	}

	return string(content), nil
}
