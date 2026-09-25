package handlers

// Configuration directory
const (
	// ConfigDir is the KiroCrew configuration directory, relative to the crew
	// home base (os.UserHomeDir() by default) for a global-scope install.
	//
	// NOTE: when KIROCREW_HOME is set it already points AT the crew home, so
	// this segment must NOT be appended again — see globalCrewDir in the parent
	// package.
	ConfigDir = ".kiro/crew"
)

// Directory names for KiroCrew assets
const (
	// DirSkills is the subdirectory under the crew config dir where skills land.
	DirSkills = "skills"
)
