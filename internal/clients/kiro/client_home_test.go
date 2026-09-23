package kiro

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sleuth-io/sx/v2/internal/clients"
	"github.com/sleuth-io/sx/v2/internal/clients/kiro/handlers"
)

// TestDetermineTargetBaseHonorsKiroHome asserts that global-scope resolution
// honors KIRO_HOME while repo/path scopes stay rooted at RepoRoot. All cases
// are hermetic: they use t.TempDir()/t.Setenv and never touch the real HOME.
func TestDetermineTargetBaseHonorsKiroHome(t *testing.T) {
	c := NewClient()

	t.Run("global scope resolves under KIRO_HOME", func(t *testing.T) {
		kiroHomeDir := t.TempDir()
		t.Setenv("KIRO_HOME", kiroHomeDir)

		got, err := c.determineTargetBase(&clients.InstallScope{Type: clients.ScopeGlobal})
		if err != nil {
			t.Fatalf("determineTargetBase returned error: %v", err)
		}

		want := filepath.Join(kiroHomeDir, handlers.ConfigDir)
		if got != want {
			t.Errorf("global target base = %q, want %q", got, want)
		}

		// Guard against ever resolving to the real home's .kiro when KIRO_HOME is set.
		realHome, herr := os.UserHomeDir()
		if herr == nil {
			realBase := filepath.Join(realHome, handlers.ConfigDir)
			if got == realBase {
				t.Errorf("global target base resolved to real home %q despite KIRO_HOME=%q", realBase, kiroHomeDir)
			}
		}
	})

	t.Run("global scope falls back to home when KIRO_HOME unset", func(t *testing.T) {
		// Explicitly clear KIRO_HOME for this subtest; t.Setenv restores it after.
		t.Setenv("KIRO_HOME", "")

		got, err := c.determineTargetBase(&clients.InstallScope{Type: clients.ScopeGlobal})
		if err != nil {
			t.Fatalf("determineTargetBase returned error: %v", err)
		}

		realHome, herr := os.UserHomeDir()
		if herr != nil {
			t.Fatalf("os.UserHomeDir returned error: %v", herr)
		}
		want := filepath.Join(realHome, handlers.ConfigDir)
		if got != want {
			t.Errorf("global target base = %q, want fallback %q", got, want)
		}
	})

	t.Run("global scope falls back to home when KIRO_HOME is whitespace", func(t *testing.T) {
		// A whitespace-only value must be treated as unset (TrimSpace).
		t.Setenv("KIRO_HOME", "   ")

		got, err := c.determineTargetBase(&clients.InstallScope{Type: clients.ScopeGlobal})
		if err != nil {
			t.Fatalf("determineTargetBase returned error: %v", err)
		}

		realHome, herr := os.UserHomeDir()
		if herr != nil {
			t.Fatalf("os.UserHomeDir returned error: %v", herr)
		}
		want := filepath.Join(realHome, handlers.ConfigDir)
		if got != want {
			t.Errorf("global target base = %q, want fallback %q", got, want)
		}
	})

	t.Run("repo scope is unaffected by KIRO_HOME", func(t *testing.T) {
		kiroHomeDir := t.TempDir()
		t.Setenv("KIRO_HOME", kiroHomeDir)

		repoRoot := t.TempDir()
		got, err := c.determineTargetBase(&clients.InstallScope{
			Type:     clients.ScopeRepository,
			RepoRoot: repoRoot,
		})
		if err != nil {
			t.Fatalf("determineTargetBase returned error: %v", err)
		}

		want := filepath.Join(repoRoot, handlers.ConfigDir)
		if got != want {
			t.Errorf("repo target base = %q, want %q (must stay rooted at RepoRoot)", got, want)
		}
		if got == filepath.Join(kiroHomeDir, handlers.ConfigDir) {
			t.Errorf("repo target base was redirected by KIRO_HOME to %q", got)
		}
	})
}
