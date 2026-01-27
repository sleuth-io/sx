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
	Instruction      *InstructionConfig      `toml:"instruction,omitempty"`
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
	PromptFile         string   `toml:"prompt-file"`
	Triggers           []string `toml:"triggers,omitempty"`
	Requires           []string `toml:"requires,omitempty"`
	SupportedLanguages []string `toml:"supported-languages,omitempty"`
}

// CommandConfig represents the [command] section
type CommandConfig struct {
	PromptFile   string   `toml:"prompt-file"`
	Aliases      []string `toml:"aliases,omitempty"`
	RequiresAuth bool     `toml:"requires-auth,omitempty"`
	Dangerous    bool     `toml:"dangerous,omitempty"`
}

// AgentConfig represents the [agent] section
type AgentConfig struct {
	PromptFile string   `toml:"prompt-file"`
	Triggers   []string `toml:"triggers,omitempty"`
	Requires   []string `toml:"requires,omitempty"`
}

// HookConfig represents the [hook] section
type HookConfig struct {
	Event       string `toml:"event"`
	ScriptFile  string `toml:"script-file"`
	Async       bool   `toml:"async,omitempty"`
	FailOnError bool   `toml:"fail-on-error,omitempty"`
	Timeout     int    `toml:"timeout,omitempty"`
}

// MCPConfig represents the [mcp] section (for both mcp and mcp-remote)
type MCPConfig struct {
	Command      string            `toml:"command"`
	Args         []string          `toml:"args"`
	Env          map[string]string `toml:"env,omitempty"`
	Timeout      int               `toml:"timeout,omitempty"`
	Capabilities []string          `toml:"capabilities,omitempty"`
}

// ClaudeCodePluginConfig represents the [claude-code-plugin] section
type ClaudeCodePluginConfig struct {
	ManifestFile     string `toml:"manifest-file,omitempty"`      // Default: .claude-plugin/plugin.json
	AutoEnable       *bool  `toml:"auto-enable,omitempty"`        // Default: true
	Marketplace      string `toml:"marketplace,omitempty"`        // Optional marketplace name
	MinClientVersion string `toml:"min-client-version,omitempty"` // Optional minimum Claude Code version
}

// InstructionConfig represents the [instruction] section
type InstructionConfig struct {
	Title      string                   `toml:"title,omitempty"`       // Heading when injected (defaults to asset name)
	PromptFile string                   `toml:"prompt-file,omitempty"` // Defaults to INSTRUCTION.md
	Cursor     *CursorInstructionConfig `toml:"cursor,omitempty"`      // Optional Cursor-specific settings
}

// CursorInstructionConfig represents Cursor-specific settings in [instruction.cursor]
type CursorInstructionConfig struct {
	AlwaysApply bool     `toml:"always-apply,omitempty"` // If true, ignores globs
	Globs       []string `toml:"globs,omitempty"`        // Override auto-generated glob
	Description string   `toml:"description,omitempty"`  // Override asset description
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
	case asset.TypeMCP, asset.TypeMCPRemote:
		return m.MCP
	case asset.TypeClaudeCodePlugin:
		return m.ClaudeCodePlugin
	case asset.TypeInstruction:
		return m.Instruction
	}
	return nil
}
