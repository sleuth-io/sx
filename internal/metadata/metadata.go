package metadata

import (
	"bytes"
	"fmt"
	"os"

	"github.com/BurntSushi/toml"

	"github.com/sleuth-io/sx/internal/asset"
)

// CurrentMetadataVersion is the current version of the metadata format
const CurrentMetadataVersion = "1.0"

// Metadata represents the complete metadata.toml structure
type Metadata struct {
	MetadataVersion string `toml:"metadata-version,omitempty"`
	Asset           Asset  `toml:"asset"`

	// Type-specific sections (only one should be present based on asset.type)
	Skill            *SkillConfig            `toml:"skill,omitempty"`
	Command          *CommandConfig          `toml:"command,omitempty"`
	Agent            *AgentConfig            `toml:"agent,omitempty"`
	Hook             *HookConfig             `toml:"hook,omitempty"`
	MCP              *MCPConfig              `toml:"mcp,omitempty"`
	ClaudeCodePlugin *ClaudeCodePluginConfig `toml:"claude-code-plugin,omitempty"`
	Rule             *RuleConfig             `toml:"rule,omitempty"`
	Custom           map[string]any          `toml:"custom,omitempty"`
}

// Asset represents the [asset] section (formerly [artifact])
type Asset struct {
	Name          string     `toml:"name"`
	Version       string     `toml:"version"`
	Type          asset.Type `toml:"type"`
	Description   string     `toml:"description,omitempty"`
	License       string     `toml:"license,omitempty"`
	Authors       []string   `toml:"authors,omitempty"`
	Keywords      []string   `toml:"keywords,omitempty"`
	Homepage      string     `toml:"homepage,omitempty"`
	Repository    string     `toml:"repository,omitempty"`
	Documentation string     `toml:"documentation,omitempty"`
	Readme        string     `toml:"readme,omitempty"`
	Dependencies  []string   `toml:"dependencies,omitempty"`
}

// SkillConfig represents the [skill] section
type SkillConfig struct {
	PromptFile string `toml:"prompt-file"`
}

// CommandConfig represents the [command] section
type CommandConfig struct {
	PromptFile string `toml:"prompt-file"`
}

// AgentConfig represents the [agent] section
type AgentConfig struct {
	PromptFile string `toml:"prompt-file"`
}

// HookConfig represents the [hook] section
type HookConfig struct {
	Event      string         `toml:"event"`
	ScriptFile string         `toml:"script-file,omitempty"`
	Command    string         `toml:"command,omitempty"`
	Args       []string       `toml:"args,omitempty"`
	Timeout    int            `toml:"timeout,omitempty"`
	Matcher    string         `toml:"matcher,omitempty"`
	Cursor     map[string]any `toml:"cursor,omitempty"`
	ClaudeCode map[string]any `toml:"claude-code,omitempty"`
	Copilot    map[string]any `toml:"copilot,omitempty"`
	Gemini     map[string]any `toml:"gemini,omitempty"`
}

// MCPConfig represents the [mcp] section (for both mcp and mcp-remote)
type MCPConfig struct {
	Transport string            `toml:"transport,omitempty"`
	Command   string            `toml:"command,omitempty"`
	Args      []string          `toml:"args,omitempty"`
	URL       string            `toml:"url,omitempty"`
	Env       map[string]string `toml:"env,omitempty"`
	Timeout   int               `toml:"timeout,omitempty"`
}

// IsRemote returns true if the MCP config uses a remote transport (sse, http, or streamable-http)
func (m *MCPConfig) IsRemote() bool {
	return m.Transport == "sse" || m.Transport == "http" || m.Transport == "streamable-http"
}

// ClaudeCodePluginConfig represents the [claude-code-plugin] section
type ClaudeCodePluginConfig struct {
	ManifestFile string `toml:"manifest-file,omitempty"` // Default: .claude-plugin/plugin.json
	AutoEnable   *bool  `toml:"auto-enable,omitempty"`   // Default: true
	Marketplace  string `toml:"marketplace,omitempty"`   // Optional marketplace name
	Source       string `toml:"source,omitempty"`        // "marketplace" or "local" (default)
}

// RuleConfig represents the [rule] section
type RuleConfig struct {
	Title       string         `toml:"title,omitempty"`       // Title/heading for the rule (defaults to asset name)
	PromptFile  string         `toml:"prompt-file,omitempty"` // Defaults to RULE.md
	Description string         `toml:"description,omitempty"` // Rule description (used in frontmatter)
	Globs       []string       `toml:"globs,omitempty"`       // File patterns this rule applies to
	Cursor      map[string]any `toml:"cursor,omitempty"`      // Cursor-specific settings
	ClaudeCode  map[string]any `toml:"claude-code,omitempty"` // Claude Code-specific settings
	Copilot     map[string]any `toml:"copilot,omitempty"`     // GitHub Copilot-specific settings
}

// metadataCompat is used for parsing old-style metadata with [artifact] section
type metadataCompat struct {
	MetadataVersion string `toml:"metadata-version,omitempty"`
	Artifact        Asset  `toml:"artifact"` // Old name for backwards compatibility
}

// Parse parses metadata from bytes
// Supports both new [asset] and old [artifact] section names
func Parse(data []byte) (*Metadata, error) {
	var metadata Metadata

	if err := toml.Unmarshal(data, &metadata); err != nil {
		return nil, fmt.Errorf("failed to parse metadata: %w", err)
	}

	// Check if we got data from [asset] section
	if metadata.Asset.Name == "" {
		// Try parsing with old [artifact] section name
		var compat metadataCompat
		if err := toml.Unmarshal(data, &compat); err == nil && compat.Artifact.Name != "" {
			metadata.Asset = compat.Artifact
		}
	}

	// Normalize MCP transport: default to "stdio" if not set
	if metadata.MCP != nil && metadata.MCP.Transport == "" {
		metadata.MCP.Transport = "stdio"
	}

	return &metadata, nil
}

// ParseFile parses metadata from a file path
func ParseFile(filePath string) (*Metadata, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read metadata file: %w", err)
	}

	return Parse(data)
}

// Marshal converts metadata to TOML bytes
func Marshal(metadata *Metadata) ([]byte, error) {
	buf := new(bytes.Buffer)
	encoder := toml.NewEncoder(buf)

	if err := encoder.Encode(metadata); err != nil {
		return nil, fmt.Errorf("failed to marshal metadata: %w", err)
	}

	return buf.Bytes(), nil
}

// Write writes metadata to a file path
func Write(metadata *Metadata, filePath string) error {
	data, err := Marshal(metadata)
	if err != nil {
		return err
	}

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write metadata file: %w", err)
	}

	return nil
}

// GetTypeConfig returns the type-specific configuration section
func (m *Metadata) GetTypeConfig() any {
	switch m.Asset.Type {
	case asset.TypeSkill:
		return m.Skill
	case asset.TypeCommand:
		return m.Command
	case asset.TypeAgent:
		return m.Agent
	case asset.TypeHook:
		return m.Hook
	case asset.TypeMCP:
		return m.MCP
	case asset.TypeClaudeCodePlugin:
		return m.ClaudeCodePlugin
	case asset.TypeRule:
		return m.Rule
	}
	return nil
}
