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

// RuleHandler handles rule asset installation for Cline
// Rules are written to .clinerules/{name}.md with paths: frontmatter
type RuleHandler struct {
	metadata *metadata.Metadata
}

// NewRuleHandler creates a new rule handler
func NewRuleHandler(meta *metadata.Metadata) *RuleHandler {
	return &RuleHandler{metadata: meta}
}

// Install writes the rule as an .md file to .clinerules/
func (h *RuleHandler) Install(ctx context.Context, zipData []byte, targetBase string) error {
	// Read rule content from zip
	content, err := h.readRuleContent(zipData)
	if err != nil {
		return fmt.Errorf("failed to read rule content: %w", err)
	}

	// Determine rules directory
	// For Cline, rules go to .clinerules/ at repo root, not inside .cline/
	// targetBase is typically {repo}/.cline, so we need to go up one level
	rulesDir := h.determineRulesDir(targetBase)
	if err := utils.EnsureDir(rulesDir); err != nil {
		return fmt.Errorf("failed to create rules directory: %w", err)
	}

	// Build content with frontmatter (same format as Claude Code)
	mdContent := h.buildMDContent(content)

	// Write to .clinerules/{name}.md
	filePath := filepath.Join(rulesDir, h.metadata.Asset.Name+".md")
	if err := os.WriteFile(filePath, []byte(mdContent), 0644); err != nil {
		return fmt.Errorf("failed to write rule file: %w", err)
	}

	return nil
}

// Remove removes the rule .md file
func (h *RuleHandler) Remove(ctx context.Context, targetBase string) error {
	rulesDir := h.determineRulesDir(targetBase)
	filePath := filepath.Join(rulesDir, h.metadata.Asset.Name+".md")

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return nil // Already removed
	}

	if err := os.Remove(filePath); err != nil {
		return fmt.Errorf("failed to remove rule file: %w", err)
	}

	return nil
}

// VerifyInstalled checks if the rule .md file exists
func (h *RuleHandler) VerifyInstalled(targetBase string) (bool, string) {
	rulesDir := h.determineRulesDir(targetBase)
	filePath := filepath.Join(rulesDir, h.metadata.Asset.Name+".md")

	if _, err := os.Stat(filePath); err == nil {
		return true, "Found at " + filePath
	}

	return false, "Rule file not found"
}

// determineRulesDir returns the rules directory based on targetBase
// For global scope (targetBase contains home dir), use ~/Documents/Cline/Rules/
// For repo/path scope, use {repo}/.clinerules/ (sibling to .cline/)
func (h *RuleHandler) determineRulesDir(targetBase string) string {
	// Check if this is a global install by looking for home directory pattern
	home, _ := os.UserHomeDir()
	if strings.HasPrefix(targetBase, filepath.Join(home, ConfigDir)) {
		// Global scope - use ~/Documents/Cline/Rules/
		return filepath.Join(home, "Documents", GlobalRulesSubdir)
	}

	// Repo/path scope - targetBase is {repo}/.cline or {repo}/{path}/.cline
	// Rules go to .clinerules/ as sibling to .cline/
	parent := filepath.Dir(targetBase)
	return filepath.Join(parent, RulesDir)
}

// buildMDContent creates the markdown content with paths: frontmatter
func (h *RuleHandler) buildMDContent(content string) string {
	var sb strings.Builder

	// Check if we need frontmatter
	globs := h.getGlobs()
	description := h.getDescription()

	if len(globs) > 0 || description != "" {
		sb.WriteString("---\n")

		// Description
		if description != "" {
			fmt.Fprintf(&sb, "description: %s\n", description)
		}

		// Paths (Cline uses same format as Claude Code)
		if len(globs) > 0 {
			if len(globs) == 1 {
				fmt.Fprintf(&sb, "paths:\n  - %s\n", globs[0])
			} else {
				sb.WriteString("paths:\n")
				for _, glob := range globs {
					fmt.Fprintf(&sb, "  - %s\n", glob)
				}
			}
		}

		sb.WriteString("---\n\n")
	}

	// Content
	sb.WriteString(strings.TrimSpace(content))
	sb.WriteString("\n")

	return sb.String()
}

// getDescription returns the description for the frontmatter
func (h *RuleHandler) getDescription() string {
	if h.metadata.Rule != nil && h.metadata.Rule.Description != "" {
		return h.metadata.Rule.Description
	}
	return h.metadata.Asset.Description
}

// getGlobs returns the globs for the frontmatter
func (h *RuleHandler) getGlobs() []string {
	if h.metadata.Rule != nil && len(h.metadata.Rule.Globs) > 0 {
		return h.metadata.Rule.Globs
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
