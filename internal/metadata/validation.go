package metadata

import (
	"fmt"
	"regexp"

	"github.com/Masterminds/semver/v3"
	"github.com/sleuth-io/skills/internal/asset"
)

var (
	// nameRegex matches valid artifact names (alphanumeric, dashes, underscores)
	nameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

	// Valid hook events
	validHookEvents = map[string]bool{
		"pre-commit":  true,
		"post-commit": true,
		"pre-push":    true,
		"post-push":   true,
		"pre-merge":   true,
		"post-merge":  true,
	}
)

// Validate validates the entire metadata structure
func (m *Metadata) Validate() error {
	// Validate artifact section
	if err := m.Artifact.Validate(); err != nil {
		return fmt.Errorf("artifact: %w", err)
	}

	// Validate type-specific configuration
	switch m.Artifact.Type {
	case asset.TypeSkill:
		if m.Skill == nil {
			return fmt.Errorf("[skill] section is required for skill artifacts")
		}
		if err := m.Skill.Validate(); err != nil {
			return fmt.Errorf("skill: %w", err)
		}

	case asset.TypeCommand:
		if m.Command == nil {
			return fmt.Errorf("[command] section is required for command artifacts")
		}
		if err := m.Command.Validate(); err != nil {
			return fmt.Errorf("command: %w", err)
		}

	case asset.TypeAgent:
		if m.Agent == nil {
			return fmt.Errorf("[agent] section is required for agent artifacts")
		}
		if err := m.Agent.Validate(); err != nil {
			return fmt.Errorf("agent: %w", err)
		}

	case asset.TypeHook:
		if m.Hook == nil {
			return fmt.Errorf("[hook] section is required for hook artifacts")
		}
		if err := m.Hook.Validate(); err != nil {
			return fmt.Errorf("hook: %w", err)
		}

	case asset.TypeMCP, asset.TypeMCPRemote:
		if m.MCP == nil {
			return fmt.Errorf("[mcp] section is required for %s artifacts", m.Artifact.Type)
		}
		if err := m.MCP.Validate(); err != nil {
			return fmt.Errorf("mcp: %w", err)
		}
	}

	return nil
}

// Validate validates the [artifact] section
func (a *Artifact) Validate() error {
	// Validate required fields
	if a.Name == "" {
		return fmt.Errorf("name is required")
	}

	if !nameRegex.MatchString(a.Name) {
		return fmt.Errorf("name must contain only alphanumeric characters, dashes, and underscores")
	}

	if a.Version == "" {
		return fmt.Errorf("version is required")
	}

	// Validate semantic version
	if _, err := semver.NewVersion(a.Version); err != nil {
		return fmt.Errorf("invalid semantic version %q: %w", a.Version, err)
	}

	if !a.Type.IsValid() {
		return fmt.Errorf("invalid artifact type: %s (must be one of: skill, command, agent, hook, mcp, mcp-remote)", a.Type)
	}

	return nil
}

// Validate validates the [skill] section
func (s *SkillConfig) Validate() error {
	if s.PromptFile == "" {
		return fmt.Errorf("prompt-file is required")
	}
	return nil
}

// Validate validates the [command] section
func (c *CommandConfig) Validate() error {
	if c.PromptFile == "" {
		return fmt.Errorf("prompt-file is required")
	}
	return nil
}

// Validate validates the [agent] section
func (a *AgentConfig) Validate() error {
	if a.PromptFile == "" {
		return fmt.Errorf("prompt-file is required")
	}
	return nil
}

// Validate validates the [hook] section
func (h *HookConfig) Validate() error {
	if h.Event == "" {
		return fmt.Errorf("event is required")
	}

	if !validHookEvents[h.Event] {
		return fmt.Errorf("invalid hook event: %s (must be one of: pre-commit, post-commit, pre-push, post-push, pre-merge, post-merge)", h.Event)
	}

	if h.ScriptFile == "" {
		return fmt.Errorf("script-file is required")
	}

	if h.Timeout < 0 {
		return fmt.Errorf("timeout must be non-negative")
	}

	return nil
}

// Validate validates the [mcp] section
func (m *MCPConfig) Validate() error {
	if m.Command == "" {
		return fmt.Errorf("command is required")
	}

	if len(m.Args) == 0 {
		return fmt.Errorf("args is required (must be a non-empty array)")
	}

	if m.Timeout < 0 {
		return fmt.Errorf("timeout must be non-negative")
	}

	return nil
}

// ValidateWithFiles validates metadata and checks that required files exist in the provided file list
func (m *Metadata) ValidateWithFiles(fileList []string) error {
	// First validate the structure
	if err := m.Validate(); err != nil {
		return err
	}

	// Note: File existence validation is handled by the handlers package
	// This method only validates the metadata structure itself
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
