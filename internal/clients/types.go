package clients

import (
	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/metadata"
)

// ParsedRule is the canonical format returned by all client rule parsers.
// When a rule file is parsed, the client-specific frontmatter is converted
// to this common format, which can then be transformed back to client-specific
// format during installation.
type ParsedRule struct {
	// Globs are the file patterns this rule applies to.
	// This is the canonical format - clients may call this "paths", "globs", or "applyTo".
	Globs []string

	// Description is a short description of the rule.
	Description string

	// ClientFields contains client-specific fields that don't have a canonical equivalent.
	// For example, Cursor's "alwaysApply" field.
	ClientFields map[string]any

	// ClientName is the name of the client that parsed this rule.
	// Empty if no client was detected.
	ClientName string

	// Content is the clean markdown content without frontmatter.
	Content string
}

// RuleCapabilities defines what a client supports for rules.
// Each client registers its capabilities to enable detection, parsing, and generation.
type RuleCapabilities struct {
	// ClientName is the identifier for this client (e.g., "claude-code", "cursor")
	ClientName string

	// RulesDirectory is where rules are stored (e.g., ".claude/rules", ".cursor/rules")
	RulesDirectory string

	// FileExtension is the file extension for rules (e.g., ".md", ".mdc")
	FileExtension string

	// InstructionFiles are files that can be parsed for rule sections (e.g., "CLAUDE.md", "AGENTS.md")
	InstructionFiles []string

	// MatchesPath checks if a path belongs to this client's rules
	MatchesPath func(path string) bool

	// MatchesContent checks if content appears to belong to this client
	MatchesContent func(path string, content []byte) bool

	// ParseRuleFile parses a rule file and returns the canonical format
	ParseRuleFile func(content []byte) (*ParsedRule, error)

	// GenerateRuleFile creates a complete rule file for this client
	GenerateRuleFile func(cfg *metadata.RuleConfig, body string) []byte

	// DetectAssetType determines what type of asset this file is.
	// Returns nil if the client doesn't recognize the file.
	// content may be nil if the file hasn't been read yet.
	DetectAssetType func(path string, content []byte) *asset.Type
}
