package handlers

// Configuration directories
const (
	// ConfigDir is the Cline configuration directory name (in home dir or repo)
	ConfigDir = ".cline"

	// RulesDir is the directory name for Cline rules (at repo root, not inside .cline)
	RulesDir = ".clinerules"
)

// Directory names for Cline assets (inside .cline/)
const (
	DirSkills     = "skills"
	DirMCPServers = "mcp-servers"
	DirRules      = "rules" // For CLI: ~/.cline/rules/
)

// VS Code extension ID for Cline (used for globalStorage path)
const (
	VSCodeExtensionID = "saoudrizwan.claude-dev"
)
