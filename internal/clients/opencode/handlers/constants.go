package handlers

// Configuration directories for OpenCode.
const (
	// GlobalConfigDir is the OpenCode global config directory, relative to home.
	GlobalConfigDir = ".config/opencode"

	// ProjectConfigDir is the OpenCode project-scoped config directory.
	ProjectConfigDir = ".opencode"

	// ConfigFile is the OpenCode config filename (lives at the root of
	// either GlobalConfigDir or the project).
	ConfigFile = "opencode.json"
)

// Asset subdirectory names under an OpenCode config directory.
const (
	DirSkills     = "skills"
	DirCommands   = "commands"
	DirAgents     = "agent"
	DirRules      = "rules"
	DirMCPServers = "mcp-servers"
)

// Default prompt filenames.
const (
	DefaultSkillPromptFile   = "SKILL.md"
	DefaultCommandPromptFile = "COMMAND.md"
	DefaultAgentPromptFile   = "AGENT.md"
	DefaultRulePromptFile    = "RULE.md"
)
