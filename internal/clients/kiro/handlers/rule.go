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

// RuleHandler handles rule asset installation for Kiro
// Rules are written to .kiro/steering/{name}.md with Kiro-specific frontmatter
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

// Install writes the rule as a .md file to .kiro/steering/
func (h *RuleHandler) Install(ctx context.Context, zipData []byte, targetBase string) error {
	// Read rule content from zip
	content, err := h.readRuleContent(zipData)
	if err != nil {
		return fmt.Errorf("failed to read rule content: %w", err)
	}

	// Ensure steering directory exists
	steeringDir := filepath.Join(targetBase, DirSteering)
	if err := utils.EnsureDir(steeringDir); err != nil {
		return fmt.Errorf("failed to create steering directory: %w", err)
	}

	// Build steering content with Kiro frontmatter
	steeringContent := h.buildSteeringContent(content)

	// Write to .kiro/steering/{name}.md
	filePath := filepath.Join(steeringDir, h.metadata.Asset.Name+".md")
	if err := os.WriteFile(filePath, []byte(steeringContent), 0644); err != nil {
		return fmt.Errorf("failed to write steering file: %w", err)
	}

	return nil
}

// Remove removes the steering .md file
func (h *RuleHandler) Remove(ctx context.Context, targetBase string) error {
	filePath := filepath.Join(targetBase, DirSteering, h.metadata.Asset.Name+".md")

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return nil // Already removed
	}

	if err := os.Remove(filePath); err != nil {
		return fmt.Errorf("failed to remove steering file: %w", err)
	}

	return nil
}

// VerifyInstalled checks if the steering .md file exists
func (h *RuleHandler) VerifyInstalled(targetBase string) (bool, string) {
	filePath := filepath.Join(targetBase, DirSteering, h.metadata.Asset.Name+".md")

	if _, err := os.Stat(filePath); err == nil {
		return true, "Found at " + filePath
	}

	return false, "Steering file not found"
}

// buildSteeringContent creates the Kiro steering format content with frontmatter
// Kiro uses:
// - inclusion: always | fileMatch | manual | auto
// - fileMatchPattern: single glob or array
// - name: for manual/auto inclusion
// - description: for auto inclusion
func (h *RuleHandler) buildSteeringContent(content string) string {
	var sb strings.Builder

	// Write frontmatter
	sb.WriteString("---\n")

	// Description (used for auto inclusion)
	description := h.getDescription()
	if description != "" {
		fmt.Fprintf(&sb, "description: %q\n", description)
	}

	// Determine inclusion mode and write appropriate fields
	globs := h.getGlobs()
	if len(globs) > 0 {
		// File matching mode
		sb.WriteString("inclusion: fileMatch\n")
		if len(globs) == 1 {
			fmt.Fprintf(&sb, "fileMatchPattern: %q\n", globs[0])
		} else {
			sb.WriteString("fileMatchPattern:\n")
			for _, glob := range globs {
				fmt.Fprintf(&sb, "  - %q\n", glob)
			}
		}
	} else {
		// Default to always apply if no globs
		sb.WriteString("inclusion: always\n")
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

// getDescription returns the description for the steering frontmatter
func (h *RuleHandler) getDescription() string {
	// First check rule-level description (common field)
	if h.metadata.Rule != nil && h.metadata.Rule.Description != "" {
		return h.metadata.Rule.Description
	}
	// Fall back to asset description
	return h.metadata.Asset.Description
}

// getGlobs returns the globs for file matching
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
