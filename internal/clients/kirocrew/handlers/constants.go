package handlers

// Configuration directory
const (
	// ConfigDir is the KiroCrew configuration directory, relative to the crew
	// home. It is joined onto the crew home base (os.UserHomeDir() by default)
	// when resolving a global-scope install. It is never joined onto a
	// repository root: KiroCrew reads skills only from its global crew root, so
	// this client refuses repo/path scopes rather than writing into a repo tree.
	//
	// NOTE: when KIROCREW_HOME is set it already points AT the crew home, so the
	// skills dir is <KIROCREW_HOME>/skills and this segment must NOT be appended
	// again. The client's home resolver handles that coherently — see
	// globalCrewDir (and crewHomeBase, which it builds on) in the parent
	// package.
	ConfigDir = ".kiro/crew"
)

// Directory names for KiroCrew assets
const (
	// DirSkills is the subdirectory under the crew config dir where skills land.
	DirSkills = "skills"
)
