package metadata

import (
	"errors"
	"fmt"
	"regexp"
	"slices"

	"github.com/Masterminds/semver/v3"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/utils"
)

var (
	// nameRegex matches valid asset names (alphanumeric, dashes, underscores)
	nameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

	// Valid hook events (canonical AI client events)
	validHookEvents = map[string]bool{
		"session-start":         true,
		"session-end":           true,
		"pre-tool-use":          true,
		"post-tool-use":         true,
		"post-tool-use-failure": true,
		"user-prompt-submit":    true,
		"stop":                  true,
		"subagent-start":        true,
		"subagent-stop":         true,
		"pre-compact":           true,
	}
)

// Validate validates the entire metadata structure
func (m *Metadata) Validate() error {
	// Validate asset section
	if err := m.Asset.Validate(); err != nil {
		return fmt.Errorf("asset: %w", err)
	}

	// Validate type-specific configuration
	switch m.Asset.Type {
	case asset.TypeSkill:
		if m.Skill == nil {
			return errors.New("[skill] section is required for skill assets")
		}
		if err := m.Skill.Validate(); err != nil {
			return fmt.Errorf("skill: %w", err)
		}

	case asset.TypeCommand:
		if m.Command == nil {
			return errors.New("[command] section is required for command assets")
		}
		if err := m.Command.Validate(); err != nil {
			return fmt.Errorf("command: %w", err)
		}

	case asset.TypeAgent:
		if m.Agent == nil {
			return errors.New("[agent] section is required for agent assets")
		}
		if err := m.Agent.Validate(); err != nil {
			return fmt.Errorf("agent: %w", err)
		}

	case asset.TypeHook:
		if m.Hook == nil {
			return errors.New("[hook] section is required for hook assets")
		}
		if err := m.Hook.Validate(); err != nil {
			return fmt.Errorf("hook: %w", err)
		}

	case asset.TypeMCP:
		if m.MCP == nil {
			return fmt.Errorf("[mcp] section is required for %s assets", m.Asset.Type)
		}
		if err := m.MCP.Validate(); err != nil {
			return fmt.Errorf("mcp: %w", err)
		}

	case asset.TypeClaudeCodePlugin:
		// ClaudeCodePlugin section is optional - all fields have defaults
		if m.ClaudeCodePlugin != nil {
			if err := m.ClaudeCodePlugin.Validate(); err != nil {
				return fmt.Errorf("claude-code-plugin: %w", err)
			}
		}

	case asset.TypeRule:
		// Rule section is optional - all fields have defaults
		// title defaults to asset name, prompt-file defaults to RULE.md
		if m.Rule != nil {
			if err := m.Rule.Validate(); err != nil {
				return fmt.Errorf("rule: %w", err)
			}
		}
	}

	return nil
}

// Validate validates the [asset] section
func (a *Asset) Validate() error {
	// Validate required fields
	if a.Name == "" {
		return errors.New("name is required")
	}

	if !nameRegex.MatchString(a.Name) {
		return errors.New("name must contain only alphanumeric characters, dashes, and underscores")
	}

	if a.Version == "" {
		return errors.New("version is required")
	}

	// Validate semantic version
	if _, err := semver.NewVersion(a.Version); err != nil {
		return fmt.Errorf("invalid semantic version %q: %w", a.Version, err)
	}

	if !a.Type.IsValid() {
		return fmt.Errorf("invalid asset type: %s (must be one of: skill, command, agent, hook, rule, mcp, claude-code-plugin)", a.Type)
	}

	return nil
}

// Validate validates the [skill] section
func (s *SkillConfig) Validate() error {
	if s.PromptFile == "" {
		return errors.New("prompt-file is required")
	}
	return nil
}

// Validate validates the [command] section
func (c *CommandConfig) Validate() error {
	if c.PromptFile == "" {
		return errors.New("prompt-file is required")
	}
	return nil
}

// Validate validates the [agent] section
func (a *AgentConfig) Validate() error {
	if a.PromptFile == "" {
		return errors.New("prompt-file is required")
	}
	return nil
}

// Validate validates the [hook] section
func (h *HookConfig) Validate() error {
	if h.Event == "" {
		return errors.New("event is required")
	}

	if !validHookEvents[h.Event] {
		return fmt.Errorf("invalid hook event: %s (must be one of: session-start, session-end, pre-tool-use, post-tool-use, post-tool-use-failure, user-prompt-submit, stop, subagent-start, subagent-stop, pre-compact)", h.Event)
	}

	// Either script-file or command must be present, but not both
	if h.ScriptFile == "" && h.Command == "" {
		return errors.New("either script-file or command is required")
	}
	if h.ScriptFile != "" && h.Command != "" {
		return errors.New("script-file and command are mutually exclusive")
	}

	if h.Timeout < 0 {
		return errors.New("timeout must be non-negative")
	}

	return nil
}

// Validate validates the [mcp] section
func (m *MCPConfig) Validate() error {
	// Validate transport (Parse normalizes empty to "stdio")
	switch m.Transport {
	case "stdio":
		if m.Command == "" {
			return errors.New("command is required for stdio transport")
		}
		if len(m.Args) == 0 {
			return errors.New("args is required for stdio transport (must be a non-empty array)")
		}
		// URL is allowed but ignored for stdio transport (may be a reference/homepage URL)
	case "sse", "http":
		if m.URL == "" {
			return fmt.Errorf("url is required for %s transport", m.Transport)
		}
		if m.Command != "" {
			return fmt.Errorf("command is not allowed for %s transport", m.Transport)
		}
		if len(m.Args) > 0 {
			return fmt.Errorf("args is not allowed for %s transport", m.Transport)
		}
	default:
		return fmt.Errorf("invalid transport %q (must be one of: stdio, sse, http)", m.Transport)
	}

	if m.Timeout < 0 {
		return errors.New("timeout must be non-negative")
	}

	return nil
}

// Validate validates the [claude-code-plugin] section
func (c *ClaudeCodePluginConfig) Validate() error {
	// All fields are optional with sensible defaults
	// ManifestFile defaults to .claude-plugin/plugin.json
	// AutoEnable defaults to true
	return nil
}

// Validate validates the [rule] section
func (r *RuleConfig) Validate() error {
	// All fields are optional with sensible defaults:
	// - title defaults to asset name
	// - prompt-file defaults to RULE.md
	return nil
}

// ValidateWithFiles validates metadata and checks that required files exist in the provided file list
func (m *Metadata) ValidateWithFiles(fileList []string) error {
	// First validate the structure
	if err := m.Validate(); err != nil {
		return err
	}

	// Check that files referenced in metadata actually exist in the file list
	switch m.Asset.Type {
	case asset.TypeSkill:
		if !slices.Contains(fileList, m.Skill.PromptFile) {
			return fmt.Errorf("prompt file not found: %s", m.Skill.PromptFile)
		}

	case asset.TypeCommand:
		if !slices.Contains(fileList, m.Command.PromptFile) {
			return fmt.Errorf("prompt file not found: %s", m.Command.PromptFile)
		}

	case asset.TypeAgent:
		if !slices.Contains(fileList, m.Agent.PromptFile) {
			return fmt.Errorf("prompt file not found: %s", m.Agent.PromptFile)
		}

	case asset.TypeHook:
		// Only check script file if using script-file mode (not command-only)
		if m.Hook.ScriptFile != "" && !slices.Contains(fileList, m.Hook.ScriptFile) {
			return fmt.Errorf("script file not found: %s", m.Hook.ScriptFile)
		}

	case asset.TypeRule:
		promptFile := "RULE.md"
		if m.Rule != nil && m.Rule.PromptFile != "" {
			promptFile = m.Rule.PromptFile
		}
		if !slices.Contains(fileList, promptFile) {
			return fmt.Errorf("prompt file not found: %s", promptFile)
		}

	case asset.TypeClaudeCodePlugin:
		// Skip manifest check for marketplace source (manifest lives on marketplace, not in zip)
		if m.ClaudeCodePlugin != nil && m.ClaudeCodePlugin.Source == "marketplace" {
			break
		}
		manifestFile := ".claude-plugin/plugin.json"
		if m.ClaudeCodePlugin != nil && m.ClaudeCodePlugin.ManifestFile != "" {
			manifestFile = m.ClaudeCodePlugin.ManifestFile
		}
		if !slices.Contains(fileList, manifestFile) {
			return fmt.Errorf("manifest file not found: %s", manifestFile)
		}

	case asset.TypeMCP, asset.TypeMCPRemote:
		// MCP assets use command + args, not files in the zip
	}

	return nil
}

// ValidateZip validates that a zip contains valid asset contents.
// It checks metadata.toml exists and is parseable, referenced files exist, and the asset type matches.
func ValidateZip(zipData []byte, expectedType *asset.Type) error {
	files, err := utils.ListZipFiles(zipData)
	if err != nil {
		return fmt.Errorf("failed to list zip files: %w", err)
	}

	if !slices.Contains(files, "metadata.toml") {
		return errors.New("metadata.toml not found in zip")
	}

	metadataBytes, err := utils.ReadZipFile(zipData, "metadata.toml")
	if err != nil {
		return fmt.Errorf("failed to read metadata.toml: %w", err)
	}

	meta, err := Parse(metadataBytes)
	if err != nil {
		return fmt.Errorf("failed to parse metadata: %w", err)
	}

	if err := meta.ValidateWithFiles(files); err != nil {
		return fmt.Errorf("metadata validation failed: %w", err)
	}

	if expectedType != nil && meta.Asset.Type != *expectedType {
		return fmt.Errorf("asset type mismatch: expected %s, got %s", expectedType.Key, meta.Asset.Type.Key)
	}

	return nil
}

// ParseDependency parses a dependency string (e.g., "package>=1.0.0,<2.0.0")
// Returns the package name and version constraint
func ParseDependency(dep string) (name string, constraint string, err error) {
	// Simple regex to split name and version constraint
	// Format: name[>=version][,<version]...
	re := regexp.MustCompile(`^([a-zA-Z0-9_-]+)([><=~!,.\d\s]*)$`)
	matches := re.FindStringSubmatch(dep)

	if matches == nil {
		return "", "", fmt.Errorf("invalid dependency format: %s", dep)
	}

	name = matches[1]
	constraint = matches[2]

	return name, constraint, nil
}

// ValidateDependencyConstraint validates a version constraint string
// Supports: >=X.Y.Z, ~=X.Y.Z, ~X.Y.Z, and comma-separated constraints
func ValidateDependencyConstraint(constraint string) error {
	if constraint == "" {
		return nil // No constraint is valid
	}

	// Try to parse as semver constraint
	_, err := semver.NewConstraint(constraint)
	if err != nil {
		return fmt.Errorf("invalid version constraint %q: %w", constraint, err)
	}

	return nil
}
