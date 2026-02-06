package github_copilot

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/clients"
	"github.com/sleuth-io/sx/internal/metadata"
)

// RuleCapabilities returns the rule capabilities for GitHub Copilot
func RuleCapabilities() *clients.RuleCapabilities {
	return &clients.RuleCapabilities{
		ClientName:       "github-copilot",
		RulesDirectory:   ".github/instructions",
		FileExtension:    ".instructions.md",
		InstructionFiles: []string{}, // copilot-instructions.md exists but is not managed by sx
		MatchesPath:      matchesPath,
		MatchesContent:   matchesContent,
		ParseRuleFile:    parseRuleFile,
		GenerateRuleFile: generateRuleFile,
		DetectAssetType:  detectAssetType,
	}
}

// detectAssetType determines the asset type for GitHub Copilot paths
func detectAssetType(path string, _ []byte) *asset.Type {
	lower := strings.ToLower(path)

	if strings.Contains(lower, ".github/instructions/") && strings.HasSuffix(lower, ".instructions.md") {
		return &asset.TypeRule
	}

	if strings.Contains(lower, ".github/skills/") {
		return &asset.TypeSkill
	}

	return nil
}

// matchesPath checks if a path belongs to GitHub Copilot instructions
func matchesPath(path string) bool {
	return strings.Contains(path, ".github/instructions/") && strings.HasSuffix(path, ".instructions.md")
}

// matchesContent checks if content appears to be a Copilot instruction file
func matchesContent(_ string, content []byte) bool {
	return bytes.Contains(content, []byte("applyTo:"))
}

// parseRuleFile parses a Copilot instruction file and returns the canonical format
func parseRuleFile(content []byte) (*clients.ParsedRule, error) {
	fm, body, err := extractYAMLFrontmatter(content)
	if err != nil {
		// No frontmatter — return raw content
		return &clients.ParsedRule{
			Content:    string(content),
			ClientName: "github-copilot",
		}, nil
	}

	result := &clients.ParsedRule{
		ClientName:   "github-copilot",
		Content:      body,
		ClientFields: make(map[string]any),
	}

	knownFields := map[string]bool{"applyTo": true, "description": true, "name": true}

	// Extract applyTo (Copilot's name for path patterns)
	if applyTo, ok := fm["applyTo"].(string); ok {
		// applyTo is comma-separated globs
		parts := strings.Split(applyTo, ",")
		for i, p := range parts {
			parts[i] = strings.TrimSpace(p)
		}
		result.Globs = parts
	}

	// Extract description
	if desc, ok := fm["description"].(string); ok {
		result.Description = desc
	}

	// Preserve unknown fields
	for key, value := range fm {
		if !knownFields[key] {
			result.ClientFields[key] = value
		}
	}

	return result, nil
}

// generateRuleFile creates a complete instruction file for GitHub Copilot
func generateRuleFile(cfg *metadata.RuleConfig, body string) []byte {
	var buf bytes.Buffer

	// Build frontmatter
	buf.WriteString("---\n")

	// applyTo from globs
	var globs []string
	if cfg != nil {
		globs = cfg.Globs
	}
	if len(globs) > 0 {
		buf.WriteString(fmt.Sprintf("applyTo: \"%s\"\n", strings.Join(globs, ",")))
	}

	// Description
	description := ""
	if cfg != nil {
		description = cfg.Description
	}
	if description != "" {
		buf.WriteString(fmt.Sprintf("description: %s\n", description))
	}

	buf.WriteString("---\n\n")

	buf.WriteString(body)
	return buf.Bytes()
}

// extractYAMLFrontmatter extracts YAML frontmatter from markdown content
func extractYAMLFrontmatter(content []byte) (map[string]any, string, error) {
	str := string(content)

	if !strings.HasPrefix(str, "---\n") {
		return nil, "", errors.New("no frontmatter found")
	}

	endIdx := strings.Index(str[4:], "\n---")
	if endIdx == -1 {
		return nil, "", errors.New("unclosed frontmatter")
	}

	fmContent := str[4 : 4+endIdx]
	body := strings.TrimPrefix(str[4+endIdx+4:], "\n")

	var fm map[string]any
	if err := yaml.Unmarshal([]byte(fmContent), &fm); err != nil {
		return nil, "", fmt.Errorf("invalid YAML frontmatter: %w", err)
	}

	return fm, body, nil
}
