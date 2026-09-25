package handlers

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sleuth-io/sx/v2/internal/utils"
)

// GlobalConfigDir resolves the global Kiro config root. KIRO_HOME, when set, is
// that root itself (it replaces ~/.kiro, it is not a parent of it); otherwise the
// root is ~/.kiro. A leading ~ is expanded and a relative value made absolute.
// Repo and path scopes never consult KIRO_HOME. See https://kiro.dev/docs/configuration/.
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
