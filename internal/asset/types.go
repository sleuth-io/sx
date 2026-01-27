package asset

// Type represents the type of asset (skill, command, agent, etc.)
type Type struct {
	Key         string
	Label       string
	Description string
}

var (
	TypeMCP = Type{
		Key:         "mcp",
		Label:       "MCP Server",
		Description: "Model Context Protocol server",
	}
	TypeMCPRemote = Type{
		Key:         "mcp-remote",
		Label:       "Remote MCP",
		Description: "Remote Model Context Protocol server configuration",
	}
	TypeSkill = Type{
		Key:         "skill",
		Label:       "Skill",
		Description: "Reusable AI skill",
	}
	TypeAgent = Type{
		Key:         "agent",
		Label:       "Agent",
		Description: "AI agent configuration",
	}
	TypeCommand = Type{
		Key:         "command",
		Label:       "Command",
		Description: "Slash command",
	}
	TypeHook = Type{
		Key:         "hook",
		Label:       "Hook",
		Description: "Git hook script",
	}
	TypeClaudeCodePlugin = Type{
		Key:         "claude-code-plugin",
		Label:       "Claude Code Plugin",
		Description: "Claude Code plugin with bundled assets",
	}
	TypeRule = Type{
		Key:         "rule",
		Label:       "Rule",
		Description: "Shared AI coding rule",
	}
)

// IsValid checks if the asset type is valid
func (t Type) IsValid() bool {
	return t.Key == TypeMCP.Key ||
		t.Key == TypeMCPRemote.Key ||
		t.Key == TypeSkill.Key ||
		t.Key == TypeAgent.Key ||
		t.Key == TypeCommand.Key ||
		t.Key == TypeHook.Key ||
		t.Key == TypeClaudeCodePlugin.Key ||
		t.Key == TypeRule.Key
}

// String returns the string representation (key) of the asset type
func (t Type) String() string {
	return t.Key
}

// FromString creates a Type from a string key
func FromString(key string) Type {
	switch key {
	case "mcp":
		return TypeMCP
	case "mcp-remote":
		return TypeMCPRemote
	case "skill":
		return TypeSkill
	case "agent":
		return TypeAgent
	case "command":
		return TypeCommand
	case "hook":
		return TypeHook
	case "claude-code-plugin":
		return TypeClaudeCodePlugin
	case "rule":
		return TypeRule
	default:
		return Type{Key: key} // Unknown type
	}
}

// MarshalText implements encoding.TextMarshaler for TOML/JSON serialization
func (t Type) MarshalText() ([]byte, error) {
	return []byte(t.Key), nil
}

// UnmarshalText implements encoding.TextUnmarshaler for TOML/JSON deserialization
func (t *Type) UnmarshalText(text []byte) error {
	*t = FromString(string(text))
	return nil
}

// AllTypes returns all defined asset types
func AllTypes() []Type {
	return []Type{
		TypeMCP,
		TypeMCPRemote,
		TypeSkill,
		TypeAgent,
		TypeCommand,
		TypeHook,
		TypeClaudeCodePlugin,
		TypeRule,
	}
}

// Asset represents a simple asset with just name, version, and type
type Asset struct {
	Name    string
	Version string
	Type    Type
	Config  map[string]string // Type-specific config (e.g., marketplace for plugins)
}
