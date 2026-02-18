package claude_code

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

// RuleCapabilities returns the rule capabilities for Claude Code
func RuleCapabilities() *clients.RuleCapabilities {
	return &clients.RuleCapabilities{
		ClientName:       "claude-code",
		RulesDirectory:   ".claude/rules",
		FileExtension:    ".md",
		InstructionFiles: []string{"CLAUDE.md", "AGENTS.md"},
		MatchesPath:      matchesPath,
		MatchesContent:   matchesContent,
		ParseRuleFile:    parseRuleFile,
		GenerateRuleFile: generateRuleFile,
		DetectAssetType:  detectAssetType,
	}
}

// detectAssetType determines the asset type for Claude Code paths
func detectAssetType(path string, _ []byte) *asset.Type {
	lower := strings.ToLower(path)

	// Only claim .claude/ subdirectories
	if strings.Contains(lower, ".claude/rules/") {
		return &asset.TypeRule
	}
	if strings.Contains(lower, ".claude/agents/") && strings.HasSuffix(lower, ".md") {
		return &asset.TypeAgent
	}
	if strings.Contains(lower, ".claude/commands/") && strings.HasSuffix(lower, ".md") {
		return &asset.TypeCommand
	}

	return nil
}

// matchesPath checks if a path belongs to Claude Code rules
func matchesPath(path string) bool {
	return strings.Contains(path, ".claude/rules/") && strings.HasSuffix(path, ".md")
}

// matchesContent checks if content appears to be a Claude Code rule file
func matchesContent(path string, content []byte) bool {
	// Claude Code uses "paths:" in frontmatter
	return bytes.Contains(content, []byte("paths:"))
}

// parseRuleFile parses a Claude Code rule file and returns the canonical format
func parseRuleFile(content []byte) (*clients.ParsedRule, error) {
	fm, body, err := extractYAMLFrontmatter(content)
	if err != nil {
		// No frontmatter - just return raw content
		return &clients.ParsedRule{
			Content:    string(content),
			ClientName: "claude-code",
		}, nil
	}

	result := &clients.ParsedRule{
		ClientName:   "claude-code",
		Content:      body,
		ClientFields: make(map[string]any),
	}

	// Known fields that we handle explicitly
	knownFields := map[string]bool{"paths": true, "description": true}

	// Extract paths (Claude Code's name for globs)
	if paths, ok := fm["paths"]; ok {
		result.Globs = toStringSlice(paths)
	}

	// Extract description
	if desc, ok := fm["description"].(string); ok {
		result.Description = desc
	}

	// Preserve unknown fields for lossless round-trip
	for key, value := range fm {
		if !knownFields[key] {
			result.ClientFields[key] = value
		}
	}

	return result, nil
}

// generateRuleFile creates a complete rule file for Claude Code
func generateRuleFile(cfg *metadata.RuleConfig, body string) []byte {
	var buf bytes.Buffer

	// Build frontmatter if needed
	fields := make(map[string]any)

	// Get description from rule config or leave empty
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
	if len(globs) > 0 {
		fields["paths"] = globs
	}

	// Write frontmatter if there are fields
	if len(fields) > 0 {
		buf.WriteString("---\n")

		// Write description first if present
		if desc, ok := fields["description"]; ok {
			fmt.Fprintf(&buf, "description: %s\n", desc)
		}

		// Write paths if present
		if paths, ok := fields["paths"].([]string); ok {
			if len(paths) == 1 {
				fmt.Fprintf(&buf, "paths:\n  - %s\n", paths[0])
			} else {
				buf.WriteString("paths:\n")
				for _, p := range paths {
					fmt.Fprintf(&buf, "  - %s\n", p)
				}
			}
		}

		buf.WriteString("---\n\n")
	}

	buf.WriteString(body)
	return buf.Bytes()
}

// extractYAMLFrontmatter extracts YAML frontmatter from markdown content
func extractYAMLFrontmatter(content []byte) (map[string]any, string, error) {
	str := string(content)

	if !strings.HasPrefix(str, "---\n") {
		return nil, "", errors.New("no frontmatter found")
	}

	// Find end of frontmatter
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
