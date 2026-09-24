package handlers

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
//
// It lives in this package, beside the ConfigDir/Dir* path vocabulary, so the
// one resolution rule is reachable from every caller that needs a global Kiro
// path — the client's install/MCP paths and the report-usage skill-path matcher —
// without any of them re-deriving it from a hardcoded literal.
func GlobalConfigDir() (string, error) {
	if raw := strings.TrimSpace(os.Getenv("KIRO_HOME")); raw != "" {
		expanded, err := utils.ExpandTilde(raw)
		if err != nil {
			return "", fmt.Errorf("cannot expand KIRO_HOME %q: %w", raw, err)
		}
		abs, err := filepath.Abs(expanded)
		if err != nil {
			return "", fmt.Errorf("cannot resolve KIRO_HOME %q: %w", raw, err)
		}
		return abs, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ConfigDir), nil
}
