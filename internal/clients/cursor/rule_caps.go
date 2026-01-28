package cursor

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

// RuleCapabilities returns the rule capabilities for Cursor
func RuleCapabilities() *clients.RuleCapabilities {
	return &clients.RuleCapabilities{
		ClientName:       "cursor",
		RulesDirectory:   ".cursor/rules",
		FileExtension:    ".mdc",
		InstructionFiles: []string{}, // Cursor doesn't have instruction files
		MatchesPath:      matchesPath,
		MatchesContent:   matchesContent,
		ParseRuleFile:    parseRuleFile,
		GenerateRuleFile: generateRuleFile,
		DetectAssetType:  detectAssetType,
	}
}

// detectAssetType determines the asset type for Cursor paths
func detectAssetType(path string, _ []byte) *asset.Type {
	lower := strings.ToLower(path)

	// Claim .cursor/rules/ with any extension
	if strings.Contains(lower, ".cursor/rules/") {
		return &asset.TypeRule
	}

	// Claim .cursor/skills/ with any extension
	if strings.Contains(lower, ".cursor/skills/") {
		return &asset.TypeSkill
	}

	return nil
}

// matchesPath checks if a path belongs to Cursor rules
func matchesPath(path string) bool {
	return strings.Contains(path, ".cursor/rules/") && strings.HasSuffix(path, ".mdc")
}

// matchesContent checks if content appears to be a Cursor rule file
func matchesContent(path string, content []byte) bool {
	// Cursor uses "globs:" in frontmatter, or has .mdc extension
	if strings.HasSuffix(path, ".mdc") {
		return true
	}
	return bytes.Contains(content, []byte("globs:"))
}

// parseRuleFile parses a Cursor rule file and returns the canonical format
func parseRuleFile(content []byte) (*clients.ParsedRule, error) {
	fm, body, err := extractYAMLFrontmatter(content)
	if err != nil {
		// No frontmatter - just return raw content
		return &clients.ParsedRule{
			Content:    string(content),
			ClientName: "cursor",
		}, nil
	}

	result := &clients.ParsedRule{
		ClientName:   "cursor",
		Content:      body,
		ClientFields: make(map[string]any),
	}

	// Known fields that we handle explicitly
	knownFields := map[string]bool{"globs": true, "description": true, "alwaysApply": true}

	// Extract globs (Cursor's name for path patterns)
	if globs, ok := fm["globs"]; ok {
		result.Globs = toStringSlice(globs)
	}

	// Extract description
	if desc, ok := fm["description"].(string); ok {
		result.Description = desc
	}

	// Extract alwaysApply (Cursor-specific)
	if alwaysApply, ok := fm["alwaysApply"].(bool); ok {
		result.ClientFields["alwaysApply"] = alwaysApply
	}

	// Preserve unknown fields for lossless round-trip
	for key, value := range fm {
		if !knownFields[key] {
			result.ClientFields[key] = value
		}
	}

	return result, nil
}

// generateRuleFile creates a complete rule file for Cursor
func generateRuleFile(cfg *metadata.RuleConfig, body string) []byte {
	var buf bytes.Buffer

	// Build frontmatter - Cursor always has frontmatter
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
	if len(globs) > 0 {
		fields["globs"] = globs
	}

	// Check for alwaysApply in Cursor config
	alwaysApply := false
	if cfg != nil && cfg.Cursor != nil {
		if val, ok := cfg.Cursor["always-apply"].(bool); ok {
			alwaysApply = val
		}
	}

	// If no globs, default to alwaysApply
	if len(globs) == 0 && !alwaysApply {
		alwaysApply = true
	}

	if alwaysApply {
		fields["alwaysApply"] = true
	}

	// Write frontmatter
	buf.WriteString("---\n")

	// Write description first if present
	if desc, ok := fields["description"]; ok {
		buf.WriteString(fmt.Sprintf("description: %s\n", desc))
	}

	// Write alwaysApply if present
	if aa, ok := fields["alwaysApply"]; ok && aa.(bool) {
		buf.WriteString("alwaysApply: true\n")
	}

	// Write globs if present
	if globs, ok := fields["globs"].([]string); ok {
		if len(globs) == 1 {
			buf.WriteString(fmt.Sprintf("globs: %s\n", globs[0]))
		} else {
			buf.WriteString("globs:\n")
			for _, g := range globs {
				buf.WriteString(fmt.Sprintf("  - %s\n", g))
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
