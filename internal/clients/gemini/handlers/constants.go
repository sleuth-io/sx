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
	// DirHooks is the directory for hook scripts
	DirHooks = "hooks"
)

// Configuration files
const (
	// SettingsFile is the Gemini settings configuration file (CLI)
	SettingsFile = "settings.json"
	// GeminiRuleFile is the standard rule file name for Gemini
	GeminiRuleFile = "GEMINI.md"
	// JetBrainsMCPFile is the MCP config file for JetBrains plugin
	JetBrainsMCPFile = "mcp.json"
)

// JetBrains IDE configuration directory paths (relative to home)
// Note: <product> is e.g. "IntelliJIdea", "PyCharm", "GoLand", etc.
// Note: <version> is e.g. "2025.1"
const (
	// JetBrainsConfigLinux is the config path pattern on Linux
	// Full path: ~/.config/JetBrains/<product><version>/
	JetBrainsConfigLinux = ".config/JetBrains"
	// JetBrainsConfigMacOS is the config path pattern on macOS
	// Full path: ~/Library/Application Support/JetBrains/<product><version>/
	JetBrainsConfigMacOS = "Library/Application Support/JetBrains"
	// JetBrainsConfigWindows is the config path pattern on Windows
	// Full path: %APPDATA%\JetBrains\<product><version>\
	JetBrainsConfigWindows = "AppData/Roaming/JetBrains"
)

// VS Code extension paths
const (
	// VSCodeExtensionsDir is the VS Code extensions directory (relative to home)
	VSCodeExtensionsDir = ".vscode/extensions"
	// VSCodeGeminiExtensionPrefix is the prefix for Gemini extension folders
	VSCodeGeminiExtensionPrefix = "google.geminicodeassist"
)

// Default prompt files
const (
	// DefaultSkillPromptFile is the default prompt file for skills
	DefaultSkillPromptFile = "SKILL.md"
)
