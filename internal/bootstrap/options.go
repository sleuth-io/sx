package bootstrap

import "os"

// Option describes a configurable bootstrap item
type Option struct {
	Key         string           // Unique key for config storage
	Description string           // What to show user
	Prompt      string           // Question to ask
	Default     bool             // Suggested answer
	DeclineNote string           // Note shown if declined (optional)
	MCPConfig   *MCPServerConfig // For MCP options - generic install config
}

// MCPServerConfig contains info to install an MCP server generically
type MCPServerConfig struct {
	Name    string            // Server name (e.g., "sx")
	Command string            // Command to run
	Args    []string          // Arguments
	Env     map[string]string // Environment variables
}

// Pre-defined options - clients/vaults return these
// Use Option.Key for comparisons (e.g., opt.Key == SessionHookKey)

// Option keys as constants for comparison
const (
	SessionHookKey          = "session_hook"
	AnalyticsHookKey        = "analytics_hook"
	SleuthAIQueryMCPKey     = "sleuth_ai_query_mcp"
	CursorSessionHookKey    = "cursor_session_hook"
	CopilotSessionHookKey   = "copilot_session_hook"
	CopilotAnalyticsHookKey = "copilot_analytics_hook"
)

// SessionHook is the Claude Code SessionStart hook option for auto-update.
// Integrates with Claude Code's native SessionStart hook in ~/.claude/settings.json.
var SessionHook = Option{
	Key:         SessionHookKey,
	Description: "Claude Code SessionStart hook - Auto-update assets when sessions start",
	Prompt:      "Install hook? (recommended)",
	Default:     true,
	DeclineNote: "Without this hook, you'll need to run 'sx install' manually.",
}

// AnalyticsHook is the Claude Code PostToolUse hook option for usage tracking.
// Integrates with Claude Code's native PostToolUse hook in ~/.claude/settings.json.
var AnalyticsHook = Option{
	Key:         AnalyticsHookKey,
	Description: "Claude Code PostToolUse hook - Track skill usage for analytics",
	Prompt:      "Install hook?",
	Default:     true,
	DeclineNote: "Skill usage analytics will not be tracked.",
}

// CursorBeforeSubmitHook is the Cursor hook option for auto-update.
// Integrates with Cursor's beforeSubmitPrompt hook in ~/.cursor/hooks.json.
// Runs once per conversation (tracked via conversation IDs).
var CursorBeforeSubmitHook = Option{
	Key:         CursorSessionHookKey,
	Description: "Cursor beforeSubmitPrompt hook - Auto-update assets once per conversation",
	Prompt:      "Install hook? (recommended)",
	Default:     true,
	DeclineNote: "Without this hook, you'll need to run 'sx install' manually.",
}

// CopilotSessionStartHook is the GitHub Copilot CLI hook option for auto-update.
// Integrates with Copilot CLI's sessionStart hook in .github/hooks/sx.json (workspace level).
var CopilotSessionStartHook = Option{
	Key:         CopilotSessionHookKey,
	Description: "Copilot CLI sessionStart hook - Auto-update assets when sessions start",
	Prompt:      "Install hook? (recommended)",
	Default:     true,
	DeclineNote: "Without this hook, you'll need to run 'sx install' manually.",
}

// CopilotAnalyticsHook is the GitHub Copilot CLI hook option for usage tracking.
// Integrates with Copilot CLI's postToolUse hook in .github/hooks/sx.json (workspace level).
var CopilotAnalyticsHook = Option{
	Key:         CopilotAnalyticsHookKey,
	Description: "Copilot CLI postToolUse hook - Track skill usage for analytics",
	Prompt:      "Install hook?",
	Default:     true,
	DeclineNote: "Skill usage analytics will not be tracked.",
}

// SleuthAIQueryMCP returns the Sleuth AI query MCP server option
// Future: may split into multiple options to enable specific tools
func SleuthAIQueryMCP() Option {
	sxPath, _ := os.Executable()
	return Option{
		Key:         SleuthAIQueryMCPKey,
		Description: "Sleuth AI Query MCP - Enables 'sx query' tool for GitHub, CI, Linear, Datadog",
		Prompt:      "Install Sleuth AI Query MCP server?",
		Default:     false,
		MCPConfig: &MCPServerConfig{
			Name:    "sx",
			Command: sxPath,
			Args:    []string{"serve"},
		},
	}
}

// ContainsKey returns true if the options slice contains an option with the given key
func ContainsKey(opts []Option, key string) bool {
	for _, opt := range opts {
		if opt.Key == key {
			return true
		}
	}
	return false
}

// Filter returns options where isEnabled returns true for the option's key
func Filter(opts []Option, isEnabled func(key string) bool) []Option {
	var result []Option
	for _, opt := range opts {
		if isEnabled(opt.Key) {
			result = append(result, opt)
		}
	}
	return result
}
