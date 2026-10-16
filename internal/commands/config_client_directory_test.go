package commands

import (
	"path/filepath"
	"testing"

	"github.com/sleuth-io/sx/v2/internal/clients"
)

func TestGetClientDirectoryCopilotHome(t *testing.T) {
	t.Run("COPILOT_HOME relocates the Copilot directory", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("USERPROFILE", t.TempDir())
		copilotHome := t.TempDir()
		t.Setenv("COPILOT_HOME", copilotHome)

		if got := getClientDirectory(clients.ClientIDGitHubCopilot); got != copilotHome {
			t.Errorf("getClientDirectory(copilot) = %q, want %q", got, copilotHome)
		}
	})

	t.Run("defaults to ~/.copilot when unset", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)
		t.Setenv("COPILOT_HOME", "")

		if want, got := filepath.Join(home, ".copilot"), getClientDirectory(clients.ClientIDGitHubCopilot); got != want {
			t.Errorf("getClientDirectory(copilot) = %q, want %q", got, want)
		}
	})
}
