package handlers

// CLI and configuration
const (
	// CLICommand is the Gemini CLI executable name
	CLICommand = "gemini"
	// ConfigDir is the Gemini configuration directory name (in home dir)
	ConfigDir = ".gemini"
)

// Directory names for Gemini assets
const (
	// DirCommands is the directory for Gemini custom commands
	DirCommands = "commands"
	// DirMCPServers is the directory for packaged MCP servers
	DirMCPServers = "mcp-servers"
)

// Configuration files
const (
	// SettingsFile is the Gemini settings configuration file
	SettingsFile = "settings.json"
	// GeminiRuleFile is the standard rule file name for Gemini
	GeminiRuleFile = "GEMINI.md"
)

// Default prompt files
const (
	// DefaultSkillPromptFile is the default prompt file for skills
	DefaultSkillPromptFile = "SKILL.md"
)
