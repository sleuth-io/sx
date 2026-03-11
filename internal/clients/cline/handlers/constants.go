package handlers

// Configuration directories
const (
	// ConfigDir is the Cline configuration directory name (in home dir or repo)
	ConfigDir = ".cline"

	// RulesDir is the directory name for Cline rules (at repo root, not inside .cline)
	RulesDir = ".clinerules"

	// GlobalRulesSubdir is the subdirectory under ~/Documents for global rules
	GlobalRulesSubdir = "Cline/Rules"
)

// Directory names for Cline assets (inside .cline/)
const (
	DirSkills     = "skills"
	DirMCPServers = "mcp-servers"
)

// VS Code extension ID for Cline (used for globalStorage path)
const (
	VSCodeExtensionID = "saoudrizwan.claude-dev"
)
