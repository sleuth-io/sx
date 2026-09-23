package handlers

// Configuration directory
const (
	// ConfigDir is the KiroCrew configuration directory, relative to the crew
	// home. When resolving a global-scope install this is joined onto the crew
	// home base (os.UserHomeDir() by default, or $KIROCREW_HOME when set); for
	// repo/path scopes it is joined onto the repo/worktree root.
	//
	// NOTE: when KIROCREW_HOME is set it already points AT the crew home, so the
	// skills dir is <KIROCREW_HOME>/skills and this segment must NOT be appended
	// again. The client's home resolver handles that coherently — see
	// determineTargetBase in the parent package.
	ConfigDir = ".kiro/crew"
)

// Directory names for KiroCrew assets
const (
	// DirSkills is the subdirectory under the crew config dir where skills land.
	DirSkills = "skills"
)
