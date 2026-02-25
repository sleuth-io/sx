package gemini

import (
	"path/filepath"
	"strings"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/clients"
	"github.com/sleuth-io/sx/internal/metadata"
)

// RuleCapabilities returns the rule capabilities for Gemini Code Assist
func RuleCapabilities() *clients.RuleCapabilities {
	return &clients.RuleCapabilities{
		ClientName:       "gemini",
		RulesDirectory:   "", // Gemini uses GEMINI.md at root, not a directory
		FileExtension:    ".md",
		InstructionFiles: []string{"GEMINI.md", "AGENT.md", "AGENTS.md"},
		MatchesPath:      matchesPath,
		MatchesContent:   matchesContent,
		ParseRuleFile:    parseRuleFile,
		GenerateRuleFile: generateRuleFile,
		DetectAssetType:  detectAssetType,
	}
}

// detectAssetType determines the asset type for Gemini paths
func detectAssetType(path string, _ []byte) *asset.Type {
	// Only claim exact GEMINI.md, AGENT.md, AGENTS.md filenames
	if isGeminiRuleFile(path) {
		return &asset.TypeRule
	}

	return nil
}

// matchesPath checks if a path belongs to Gemini rules
func matchesPath(path string) bool {
	return isGeminiRuleFile(path)
}

// isGeminiRuleFile checks if the path is an exact Gemini rule file
// (GEMINI.md, AGENT.md, or AGENTS.md - not files ending with those strings)
func isGeminiRuleFile(path string) bool {
	// Extract just the filename from the path
	base := strings.ToLower(filepath.Base(path))

	// Only match exact filenames
	switch base {
	case "gemini.md", "agent.md", "agents.md":
		return true
	default:
		return false
	}
}

// matchesContent checks if content appears to be a Gemini rule file
// Gemini rules are plain markdown without specific markers
func matchesContent(path string, content []byte) bool {
	// Check file name first
	return matchesPath(path)
}

// parseRuleFile parses a Gemini rule file and returns the canonical format
// Gemini rules are plain markdown without frontmatter
func parseRuleFile(content []byte) (*clients.ParsedRule, error) {
	return &clients.ParsedRule{
		Content:    string(content),
		ClientName: "gemini",
		// No globs - Gemini doesn't support them
		// No frontmatter - Gemini uses plain markdown
	}, nil
}

// generateRuleFile creates a complete rule file for Gemini.
// Uses plain markdown without frontmatter.
func generateRuleFile(cfg *metadata.RuleConfig, body string) []byte {
	// Gemini rules are plain markdown
	// Add title as H1 if we have one
	var content strings.Builder

	if cfg != nil && cfg.Title != "" {
		content.WriteString("# ")
		content.WriteString(cfg.Title)
		content.WriteString("\n\n")
	}

	content.WriteString(strings.TrimSpace(body))
	content.WriteString("\n")

	return []byte(content.String())
}
