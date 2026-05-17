package opencode

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/clients"
	"github.com/sleuth-io/sx/internal/metadata"
)

// RuleCapabilities returns the rule capabilities for OpenCode. OpenCode
// reads project rules from `AGENTS.md` (with `CLAUDE.md` as a migration
// fallback) and any additional files listed in the `instructions` array
// of opencode.json. sx installs rules into a `<config>/rules/` directory
// and registers each rule path under `instructions`.
func RuleCapabilities() *clients.RuleCapabilities {
	return &clients.RuleCapabilities{
		ClientName:       "opencode",
		RulesDirectory:   ".opencode/rules",
		FileExtension:    ".md",
		InstructionFiles: []string{"AGENTS.md"},
		MatchesPath:      matchesPath,
		MatchesContent:   matchesContent,
		ParseRuleFile:    parseRuleFile,
		GenerateRuleFile: generateRuleFile,
		DetectAssetType:  detectAssetType,
	}
}

func matchesPath(path string) bool {
	lower := strings.ToLower(path)
	return (strings.Contains(lower, ".opencode/rules/") && strings.HasSuffix(lower, ".md")) ||
		strings.HasSuffix(path, "/AGENTS.md") || path == "AGENTS.md"
}

func matchesContent(path string, _ []byte) bool {
	return matchesPath(path)
}

func detectAssetType(path string, _ []byte) *asset.Type {
	lower := strings.ToLower(path)

	if strings.Contains(lower, ".opencode/rules/") && strings.HasSuffix(lower, ".md") {
		return &asset.TypeRule
	}
	if strings.Contains(lower, ".opencode/skills/") {
		return &asset.TypeSkill
	}
	if strings.Contains(lower, ".opencode/agent/") || strings.Contains(lower, ".opencode/agents/") {
		return &asset.TypeAgent
	}
	if strings.Contains(lower, ".opencode/commands/") {
		return &asset.TypeCommand
	}
	return nil
}

// parseRuleFile treats the entire content as the rule body. OpenCode rules
// are plain markdown — no required frontmatter — so we don't try to parse
// fields out of them. AGENTS.md often contains free-form prose.
func parseRuleFile(content []byte) (*clients.ParsedRule, error) {
	return &clients.ParsedRule{
		ClientName: "opencode",
		Content:    string(content),
	}, nil
}

// generateRuleFile renders a rule file for OpenCode. OpenCode does not
// require frontmatter, so we only emit a description heading when the
// metadata has one, and otherwise pass the body through verbatim.
func generateRuleFile(cfg *metadata.RuleConfig, body string) []byte {
	var buf bytes.Buffer
	if cfg != nil && cfg.Description != "" {
		fmt.Fprintf(&buf, "<!-- %s -->\n\n", cfg.Description)
	}
	buf.WriteString(body)
	return buf.Bytes()
}
