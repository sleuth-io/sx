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
	// TypeMCPRemote is a deprecated alias for TypeMCP.
	// Kept for backwards compatibility with existing lock files and vaults.
	TypeMCPRemote = TypeMCP
	TypeSkill     = Type{
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
	// TypeAppPlugin extends the sx DESKTOP APP itself ("extension" in UI
	// copy — docs/app-plugins-spec.md). Never installed into AI coding
	// clients: no client declares support for it, and the app hides it
	// from asset views (it surfaces only in the Extensions screen).
	TypeAppPlugin = Type{
		Key:         "app-plugin",
		Label:       "Extension",
		Description: "sx desktop app extension",
	}
)

// IsValid checks if the asset type is valid
func (t Type) IsValid() bool {
	return t.Key == TypeMCP.Key ||
		t.Key == TypeSkill.Key ||
		t.Key == TypeAgent.Key ||
		t.Key == TypeCommand.Key ||
		t.Key == TypeHook.Key ||
		t.Key == TypeClaudeCodePlugin.Key ||
		t.Key == TypeRule.Key ||
		t.Key == TypeAppPlugin.Key
}

// String returns the string representation (key) of the asset type
func (t Type) String() string {
	return t.Key
}

// FromString creates a Type from a string key
func FromString(key string) Type {
	switch key {
	case "mcp", "mcp-remote":
		return TypeMCP
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
	case "app-plugin":
		return TypeAppPlugin
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
		TypeSkill,
		TypeAgent,
		TypeCommand,
		TypeHook,
		TypeClaudeCodePlugin,
		TypeRule,
		TypeAppPlugin,
	}
}

// ClientTypes returns the types AI coding clients can carry — AllTypes
// minus app-plugin, which extends the sx desktop app itself and never
// installs into an AI tool (docs/app-plugins-spec.md).
func ClientTypes() []Type {
	out := make([]Type, 0, len(AllTypes()))
	for _, t := range AllTypes() {
		if t.Key == TypeAppPlugin.Key {
			continue
		}
		out = append(out, t)
	}
	return out
}

// Asset represents a simple asset with just name, version, and type
type Asset struct {
	Name    string
	Version string
	Type    Type
	Config  map[string]string // Type-specific config (e.g., marketplace for plugins)
}
