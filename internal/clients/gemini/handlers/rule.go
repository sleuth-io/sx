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

// RuleHandler handles rule asset installation for Gemini
// Rules are written to GEMINI.md files (plain markdown, no frontmatter)
type RuleHandler struct {
	metadata *metadata.Metadata
}

// NewRuleHandler creates a new rule handler
func NewRuleHandler(meta *metadata.Metadata) *RuleHandler {
	return &RuleHandler{
		metadata: meta,
	}
}

// Install writes the rule as GEMINI.md file
// For multiple rules in the same scope, content is appended to GEMINI.md
func (h *RuleHandler) Install(ctx context.Context, zipData []byte, targetBase string) error {
	// Read rule content from zip
	content, err := h.readRuleContent(zipData)
	if err != nil {
		return fmt.Errorf("failed to read rule content: %w", err)
	}

	// Build markdown content
	mdContent := h.buildMarkdownContent(content)

	// Gemini uses a single GEMINI.md file per scope
	// Each rule gets its own section with a marker comment
	filePath := filepath.Join(targetBase, GeminiRuleFile)

	// Read existing content if file exists
	existingContent := ""
	if data, err := os.ReadFile(filePath); err == nil {
		existingContent = string(data)
	}

	// Update or add our rule section
	newContent := h.updateRuleSection(existingContent, mdContent)

	// Write the file
	if err := os.MkdirAll(targetBase, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	if err := os.WriteFile(filePath, []byte(newContent), 0644); err != nil {
		return fmt.Errorf("failed to write rule file: %w", err)
	}

	return nil
}

// Remove removes the rule section from GEMINI.md
func (h *RuleHandler) Remove(ctx context.Context, targetBase string) error {
	filePath := filepath.Join(targetBase, GeminiRuleFile)

	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // Already removed
		}
		return fmt.Errorf("failed to read rule file: %w", err)
	}

	// Remove our rule section
	newContent := h.removeRuleSection(string(data))

	// If file is now empty (or just whitespace), remove it
	if strings.TrimSpace(newContent) == "" {
		return os.Remove(filePath)
	}

	return os.WriteFile(filePath, []byte(newContent), 0644)
}

// VerifyInstalled checks if the rule is present in GEMINI.md
func (h *RuleHandler) VerifyInstalled(targetBase string) (bool, string) {
	filePath := filepath.Join(targetBase, GeminiRuleFile)

	data, err := os.ReadFile(filePath)
	if err != nil {
		return false, "GEMINI.md not found"
	}

	// Check if our marker exists
	marker := h.getSectionMarker()
	if strings.Contains(string(data), marker) {
		return true, "Found in " + filePath
	}

	return false, "Rule section not found in GEMINI.md"
}

// buildMarkdownContent creates the plain markdown content for the rule
func (h *RuleHandler) buildMarkdownContent(content string) string {
	var sb strings.Builder

	// Add title as heading
	title := h.getTitle()
	sb.WriteString("## ")
	sb.WriteString(title)
	sb.WriteString("\n\n")

	// Add content
	sb.WriteString(strings.TrimSpace(content))
	sb.WriteString("\n")

	return sb.String()
}

// getSectionMarker returns a unique marker for this rule's section
func (h *RuleHandler) getSectionMarker() string {
	return fmt.Sprintf("<!-- sx:%s -->", h.metadata.Asset.Name)
}

// updateRuleSection updates or adds the rule section in the existing content
func (h *RuleHandler) updateRuleSection(existingContent, newRuleContent string) string {
	marker := h.getSectionMarker()
	markerEnd := fmt.Sprintf("<!-- /sx:%s -->", h.metadata.Asset.Name)

	// Check if section already exists
	startIdx := strings.Index(existingContent, marker)
	if startIdx != -1 {
		// Find end marker
		endIdx := strings.Index(existingContent[startIdx:], markerEnd)
		if endIdx != -1 {
			// Replace existing section
			before := existingContent[:startIdx]
			after := existingContent[startIdx+endIdx+len(markerEnd):]

			// Build new section
			section := marker + "\n" + newRuleContent + markerEnd + "\n"
			return strings.TrimSpace(before) + "\n\n" + section + strings.TrimSpace(after)
		}
	}

	// Add new section at the end
	section := marker + "\n" + newRuleContent + markerEnd + "\n"
	if existingContent != "" {
		return strings.TrimSpace(existingContent) + "\n\n" + section
	}
	return section
}

// removeRuleSection removes the rule section from content
func (h *RuleHandler) removeRuleSection(content string) string {
	marker := h.getSectionMarker()
	markerEnd := fmt.Sprintf("<!-- /sx:%s -->", h.metadata.Asset.Name)

	startIdx := strings.Index(content, marker)
	if startIdx == -1 {
		return content
	}

	endIdx := strings.Index(content[startIdx:], markerEnd)
	if endIdx == -1 {
		return content
	}

	before := content[:startIdx]
	after := content[startIdx+endIdx+len(markerEnd):]

	// Clean up extra newlines
	result := strings.TrimSpace(before) + "\n\n" + strings.TrimSpace(after)
	return strings.TrimSpace(result) + "\n"
}

// getTitle returns the rule title, defaulting to asset name
func (h *RuleHandler) getTitle() string {
	if h.metadata.Rule != nil && h.metadata.Rule.Title != "" {
		return h.metadata.Rule.Title
	}
	return h.metadata.Asset.Name
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
