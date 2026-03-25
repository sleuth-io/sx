package kiro

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

// RuleCapabilities returns the rule capabilities for Kiro
func RuleCapabilities() *clients.RuleCapabilities {
	return &clients.RuleCapabilities{
		ClientName:       clients.ClientIDKiro,
		RulesDirectory:   ".kiro/steering",
		FileExtension:    ".md",
		InstructionFiles: []string{"AGENTS.md"}, // Kiro recognizes AGENTS.md files
		MatchesPath:      matchesPath,
		MatchesContent:   matchesContent,
		ParseRuleFile:    parseRuleFile,
		GenerateRuleFile: generateRuleFile,
		DetectAssetType:  detectAssetType,
	}
}

// detectAssetType determines the asset type for Kiro paths
func detectAssetType(path string, _ []byte) *asset.Type {
	lower := strings.ToLower(path)

	// Claim .kiro/steering/ with .md extension
	if strings.Contains(lower, ".kiro/steering/") && strings.HasSuffix(lower, ".md") {
		return &asset.TypeRule
	}

	// Claim .kiro/skills/ directories
	if strings.Contains(lower, ".kiro/skills/") {
		return &asset.TypeSkill
	}

	return nil
}

// matchesPath checks if a path belongs to Kiro steering files
func matchesPath(path string) bool {
	return strings.Contains(path, ".kiro/steering/") && strings.HasSuffix(path, ".md")
}

// matchesContent checks if content appears to be a Kiro steering file
func matchesContent(path string, content []byte) bool {
	// Kiro uses "inclusion:" in frontmatter
	if bytes.Contains(content, []byte("inclusion:")) {
		return true
	}
	// Also check for fileMatchPattern which is Kiro-specific
	if bytes.Contains(content, []byte("fileMatchPattern:")) {
		return true
	}
	return false
}

// parseRuleFile parses a Kiro steering file and returns the canonical format
func parseRuleFile(content []byte) (*clients.ParsedRule, error) {
	fm, body, err := extractYAMLFrontmatter(content)
	if err != nil {
		// No frontmatter - just return raw content
		return &clients.ParsedRule{
			Content:    string(content),
			ClientName: clients.ClientIDKiro,
		}, nil
	}

	result := &clients.ParsedRule{
		ClientName:   clients.ClientIDKiro,
		Content:      body,
		ClientFields: make(map[string]any),
	}

	// Known fields that we handle explicitly
	knownFields := map[string]bool{
		"inclusion":        true,
		"fileMatchPattern": true,
		"description":      true,
		"name":             true,
	}

	// Extract fileMatchPattern (Kiro's name for path patterns)
	if patterns, ok := fm["fileMatchPattern"]; ok {
		result.Globs = toStringSlice(patterns)
	}

	// Extract description
	if desc, ok := fm["description"].(string); ok {
		result.Description = desc
	}

	// Extract inclusion mode (Kiro-specific)
	if inclusion, ok := fm["inclusion"].(string); ok {
		result.ClientFields["inclusion"] = inclusion
	}

	// Extract name (for manual/auto inclusion)
	if name, ok := fm["name"].(string); ok {
		result.ClientFields["name"] = name
	}

	// Preserve unknown fields for lossless round-trip
	for key, value := range fm {
		if !knownFields[key] {
			result.ClientFields[key] = value
		}
	}

	return result, nil
}

// generateRuleFile creates a complete steering file for Kiro
func generateRuleFile(cfg *metadata.RuleConfig, body string) []byte {
	var buf bytes.Buffer

	// Build frontmatter
	fields := make(map[string]any)

	// Get description from rule config
	description := ""
	if cfg != nil {
		description = cfg.Description
	}
	if description != "" {
		fields["description"] = description
	}

	// Get globs from rule config
	var globs []string
	if cfg != nil {
		globs = cfg.Globs
	}

	// Determine inclusion mode
	inclusion := "always"
	if cfg != nil && cfg.Kiro != nil {
		if inc, ok := cfg.Kiro["inclusion"].(string); ok {
			inclusion = inc
		}
	}

	// If globs are set, use fileMatch inclusion
	if len(globs) > 0 {
		inclusion = "fileMatch"
	}

	// Write frontmatter
	buf.WriteString("---\n")

	// Write description first if present
	if desc, ok := fields["description"]; ok {
		fmt.Fprintf(&buf, "description: %q\n", desc)
	}

	// Write inclusion mode
	fmt.Fprintf(&buf, "inclusion: %s\n", inclusion)

	// Write fileMatchPattern if using fileMatch
	if len(globs) > 0 {
		if len(globs) == 1 {
			fmt.Fprintf(&buf, "fileMatchPattern: %q\n", globs[0])
		} else {
			buf.WriteString("fileMatchPattern:\n")
			for _, g := range globs {
				fmt.Fprintf(&buf, "  - %q\n", g)
			}
		}
	}

	buf.WriteString("---\n\n")

	buf.WriteString(body)
	return buf.Bytes()
}

// extractYAMLFrontmatter extracts YAML frontmatter from markdown content
func extractYAMLFrontmatter(content []byte) (map[string]any, string, error) {
	str := string(content)

	if !strings.HasPrefix(str, "---\n") && !strings.HasPrefix(str, "---\r\n") {
		return nil, "", errors.New("no frontmatter found")
	}

	// Find end of frontmatter
	rest := str[4:]
	fmContent, body, found := strings.Cut(rest, "\n---")
	if !found {
		return nil, "", errors.New("unclosed frontmatter")
	}

	body = strings.TrimPrefix(body, "\n")
	body = strings.TrimPrefix(body, "\r\n")

	var fm map[string]any
	if err := yaml.Unmarshal([]byte(fmContent), &fm); err != nil {
		return nil, "", fmt.Errorf("invalid YAML frontmatter: %w", err)
	}

	return fm, body, nil
}

// toStringSlice converts an interface to a string slice
func toStringSlice(v any) []string {
	switch val := v.(type) {
	case []string:
		return val
	case []any:
		result := make([]string, 0, len(val))
		for _, item := range val {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	case string:
		return []string{val}
	default:
		return nil
	}
}
