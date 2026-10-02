package handlers

import (
	"path/filepath"

	"github.com/sleuth-io/sx/v2/internal/utils"
)

// GlobalConfigDir resolves the GLOBAL Kiro configuration root — the directory
// that holds agents, skills, steering, settings and sessions.
//
// KIRO_HOME, when set, *is* that root: it replaces ~/.kiro wholesale rather than
// naming a parent under which a .kiro directory gets created, which is what lets
// one machine hold several independent Kiro profiles. So KIRO_HOME=/opt/profile
// resolves to /opt/profile, never /opt/profile/.kiro. When it is unset or blank
// the root is the default ~/.kiro. See https://kiro.dev/docs/configuration/.
//
// A leading ~ is expanded and a relative value is made absolute, so the returned
// path is always absolute. This governs global scope only; repo and path scopes
// stay rooted at a repo root and never consult KIRO_HOME.
func GlobalConfigDir() (string, error) {
	base, set, err := utils.ResolveHomeEnv("KIRO_HOME")
	if err != nil {
		return "", err
	}
	if set {
		return base, nil
	}
	return filepath.Join(base, ConfigDir), nil
}
