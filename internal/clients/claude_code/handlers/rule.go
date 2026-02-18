package handlers

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/handlers/rule"
	"github.com/sleuth-io/sx/internal/metadata"
	"github.com/sleuth-io/sx/internal/utils"
)

// RuleHandler handles rule asset installation for Claude Code
// Rules are written to .claude/rules/{name}.md with optional YAML frontmatter
type RuleHandler struct {
	metadata *metadata.Metadata
}

// NewRuleHandler creates a new rule handler
// Note: lockConfig parameter is deprecated and ignored (kept for API compatibility)
func NewRuleHandler(meta *metadata.Metadata, _ any) *RuleHandler {
	return &RuleHandler{
		metadata: meta,
	}
}

// Install writes the rule as a .md file to .claude/rules/
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

	// Build rule file content with frontmatter
	ruleContent := h.buildRuleContent(content)

	// Write to .claude/rules/{name}.md
	filePath := filepath.Join(rulesDir, h.metadata.Asset.Name+".md")
	if err := os.WriteFile(filePath, []byte(ruleContent), 0644); err != nil {
		return fmt.Errorf("failed to write rule file: %w", err)
	}

	return nil
}

// Remove removes the rule .md file
func (h *RuleHandler) Remove(ctx context.Context, targetBase string) error {
	filePath := filepath.Join(targetBase, "rules", h.metadata.Asset.Name+".md")

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return nil // Already removed
	}

	if err := os.Remove(filePath); err != nil {
		return fmt.Errorf("failed to remove rule file: %w", err)
	}

	return nil
}

// GetInstallPath returns a description of where the rule is installed
func (h *RuleHandler) GetInstallPath() string {
	return ".claude/rules/"
}

// CanDetectInstalledState returns true since we can check for the rule file
func (h *RuleHandler) CanDetectInstalledState() bool {
	return true
}

// VerifyInstalled checks if the rule .md file exists
func (h *RuleHandler) VerifyInstalled(targetBase string) (bool, string) {
	filePath := filepath.Join(targetBase, "rules", h.metadata.Asset.Name+".md")

	if _, err := os.Stat(filePath); err == nil {
		return true, "Found at " + filePath
	}

	return false, "Rule file not found"
}

// buildRuleContent creates the rule file content with optional frontmatter
func (h *RuleHandler) buildRuleContent(content string) string {
	var sb strings.Builder

	// Get globs and description from metadata
	globs := h.getGlobs()
	description := h.getDescription()

	// Only add frontmatter if there are globs or description
	if len(globs) > 0 || description != "" {
		sb.WriteString("---\n")

		// Description
		if description != "" {
			fmt.Fprintf(&sb, "description: %s\n", description)
		}

		// Globs (Claude Code uses "paths" field)
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

	// Title as heading (optional but nice for readability)
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
	// First check rule-level description
	if h.metadata.Rule != nil && h.metadata.Rule.Description != "" {
		return h.metadata.Rule.Description
	}
	// Fall back to asset description
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

// Validate checks if the zip structure is valid for a rule asset
func (h *RuleHandler) Validate(zipData []byte) error {
	files, err := utils.ListZipFiles(zipData)
	if err != nil {
		return fmt.Errorf("failed to list zip files: %w", err)
	}

	if !containsFile(files, "metadata.toml") {
		return errors.New("metadata.toml not found in zip")
	}

	metadataBytes, err := utils.ReadZipFile(zipData, "metadata.toml")
	if err != nil {
		return fmt.Errorf("failed to read metadata.toml: %w", err)
	}

	meta, err := metadata.Parse(metadataBytes)
	if err != nil {
		return fmt.Errorf("failed to parse metadata: %w", err)
	}

	if err := meta.ValidateWithFiles(files); err != nil {
		return fmt.Errorf("metadata validation failed: %w", err)
	}

	if meta.Asset.Type != asset.TypeRule {
		return fmt.Errorf("asset type mismatch: expected rule, got %s", meta.Asset.Type)
	}

	// Check that prompt file exists
	promptFile := rule.DefaultPromptFile
	if meta.Rule != nil && meta.Rule.PromptFile != "" {
		promptFile = meta.Rule.PromptFile
	}

	if !containsFile(files, promptFile) && !containsFile(files, "rule.md") {
		return fmt.Errorf("prompt file not found in zip: %s", promptFile)
	}

	return nil
}
