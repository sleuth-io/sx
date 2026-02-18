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

// RuleHandler handles rule asset installation for Cursor
// Rules are written to .cursor/rules/{name}.mdc with MDC frontmatter
type RuleHandler struct {
	metadata *metadata.Metadata
	// pathScope is the path this rule is scoped to (empty for repo-wide)
	pathScope string
}

// NewRuleHandler creates a new rule handler
func NewRuleHandler(meta *metadata.Metadata, pathScope string) *RuleHandler {
	return &RuleHandler{
		metadata:  meta,
		pathScope: pathScope,
	}
}

// Install writes the rule as an .mdc file to .cursor/rules/
func (h *RuleHandler) Install(ctx context.Context, zipData []byte, targetBase string) error {
	// Read rule content from zip
	content, err := h.readRuleContent(zipData)
	if err != nil {
		return fmt.Errorf("failed to read rule content: %w", err)
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

// Remove removes the rule .mdc file
func (h *RuleHandler) Remove(ctx context.Context, targetBase string) error {
	filePath := filepath.Join(targetBase, "rules", h.metadata.Asset.Name+".mdc")

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return nil // Already removed
	}

	if err := os.Remove(filePath); err != nil {
		return fmt.Errorf("failed to remove rule file: %w", err)
	}

	return nil
}

// VerifyInstalled checks if the rule .mdc file exists
func (h *RuleHandler) VerifyInstalled(targetBase string) (bool, string) {
	filePath := filepath.Join(targetBase, "rules", h.metadata.Asset.Name+".mdc")

	if _, err := os.Stat(filePath); err == nil {
		return true, "Found at " + filePath
	}

	return false, "Rule file not found"
}

// buildMDCContent creates the MDC format content with frontmatter
func (h *RuleHandler) buildMDCContent(content string) string {
	var sb strings.Builder

	// Write frontmatter
	sb.WriteString("---\n")

	// Description
	description := h.getDescription()
	if description != "" {
		fmt.Fprintf(&sb, "description: %s\n", description)
	}

	// Globs or alwaysApply
	if h.shouldAlwaysApply() {
		sb.WriteString("alwaysApply: true\n")
	} else {
		globs := h.getGlobs()
		if len(globs) > 0 {
			// Single glob as string, multiple as array
			if len(globs) == 1 {
				fmt.Fprintf(&sb, "globs: %s\n", globs[0])
			} else {
				sb.WriteString("globs:\n")
				for _, glob := range globs {
					fmt.Fprintf(&sb, "  - %s\n", glob)
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

// getTitle returns the rule title, defaulting to asset name
func (h *RuleHandler) getTitle() string {
	if h.metadata.Rule != nil && h.metadata.Rule.Title != "" {
		return h.metadata.Rule.Title
	}
	return h.metadata.Asset.Name
}

// getDescription returns the description for the MDC frontmatter
func (h *RuleHandler) getDescription() string {
	// First check rule-level description (common field)
	if h.metadata.Rule != nil && h.metadata.Rule.Description != "" {
		return h.metadata.Rule.Description
	}
	// Fall back to asset description
	return h.metadata.Asset.Description
}

// shouldAlwaysApply returns true if the rule should always apply
func (h *RuleHandler) shouldAlwaysApply() bool {
	// Check cursor-specific always-apply setting
	if h.metadata.Rule != nil && h.metadata.Rule.Cursor != nil {
		if alwaysApply, ok := h.metadata.Rule.Cursor["always-apply"].(bool); ok {
			return alwaysApply
		}
	}
	// If no path scope and no explicit globs, default to always apply
	return h.pathScope == "" && len(h.getGlobs()) == 0
}

// getGlobs returns the globs for the MDC frontmatter
func (h *RuleHandler) getGlobs() []string {
	// Check for globs in rule config (common field)
	if h.metadata.Rule != nil && len(h.metadata.Rule.Globs) > 0 {
		return h.metadata.Rule.Globs
	}

	// Auto-generate from path scope
	if h.pathScope != "" {
		// Ensure path ends with /**/* for glob matching
		scope := strings.TrimSuffix(h.pathScope, "/")
		return []string{scope + "/**/*"}
	}

	return nil
}

// getPromptFile returns the prompt file, using the shared default
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
