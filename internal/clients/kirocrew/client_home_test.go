package kirocrew

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sleuth-io/sx/v2/internal/asset"
	"github.com/sleuth-io/sx/v2/internal/clients"
	"github.com/sleuth-io/sx/v2/internal/clients/kirocrew/handlers"
)

// TestDetermineTargetBaseHonorsKirocrewHome asserts that global-scope
// resolution honors KIROCREW_HOME (which already points AT the crew home, so
// the crew ConfigDir segment must NOT be re-appended) while repo/path scopes
// stay rooted at RepoRoot. All cases are hermetic: they use t.TempDir()/
// t.Setenv and never touch the real HOME.
func TestDetermineTargetBaseHonorsKirocrewHome(t *testing.T) {
	c := NewClient()

	t.Run("global scope resolves to KIROCREW_HOME without doubling the crew segment", func(t *testing.T) {
		crewHomeDir := t.TempDir()
		t.Setenv("KIROCREW_HOME", crewHomeDir)

		got, err := c.determineTargetBase(&clients.InstallScope{Type: clients.ScopeGlobal})
		if err != nil {
			t.Fatalf("determineTargetBase returned error: %v", err)
		}

		// KIROCREW_HOME is already the crew home; the target base is exactly it,
		// NOT crewHomeDir/.kiro/crew.
		if got != crewHomeDir {
			t.Errorf("global target base = %q, want %q (must not re-append the crew segment)", got, crewHomeDir)
		}

		doubled := filepath.Join(crewHomeDir, handlers.ConfigDir)
		if got == doubled {
			t.Errorf("global target base doubled the crew segment: got %q", doubled)
		}

		// Guard against resolving to the real home's crew dir when KIROCREW_HOME is set.
		realHome, herr := os.UserHomeDir()
		if herr == nil {
			realBase := filepath.Join(realHome, handlers.ConfigDir)
			if got == realBase {
				t.Errorf("global target base resolved to real home %q despite KIROCREW_HOME=%q", realBase, crewHomeDir)
			}
		}
	})

	t.Run("global scope falls back to home/.kiro/crew when KIROCREW_HOME unset", func(t *testing.T) {
		// Explicitly clear KIROCREW_HOME for this subtest; t.Setenv restores it after.
		t.Setenv("KIROCREW_HOME", "")

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

	t.Run("global scope falls back to home when KIROCREW_HOME is whitespace", func(t *testing.T) {
		// A whitespace-only value must be treated as unset (TrimSpace).
		t.Setenv("KIROCREW_HOME", "   ")

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

	t.Run("repo scope is unaffected by KIROCREW_HOME", func(t *testing.T) {
		crewHomeDir := t.TempDir()
		t.Setenv("KIROCREW_HOME", crewHomeDir)

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
		if got == crewHomeDir {
			t.Errorf("repo target base was redirected by KIROCREW_HOME to %q", got)
		}
	})

	t.Run("path scope is unaffected by KIROCREW_HOME", func(t *testing.T) {
		crewHomeDir := t.TempDir()
		t.Setenv("KIROCREW_HOME", crewHomeDir)

		repoRoot := t.TempDir()
		got, err := c.determineTargetBase(&clients.InstallScope{
			Type:     clients.ScopePath,
			RepoRoot: repoRoot,
			Path:     "sub/dir",
		})
		if err != nil {
			t.Fatalf("determineTargetBase returned error: %v", err)
		}

		want := filepath.Join(repoRoot, "sub/dir", handlers.ConfigDir)
		if got != want {
			t.Errorf("path target base = %q, want %q (must stay rooted at RepoRoot/Path)", got, want)
		}
	})
}

// TestSupportsSkillsOnly asserts the client supports the skill asset type and
// rejects the KiroCrew-adjacent types the kiro client carries (agent, mcp,
// rule, command, hook) — those are deferred and must not be silently accepted.
func TestSupportsSkillsOnly(t *testing.T) {
	c := NewClient()

	if !c.SupportsAssetType(asset.TypeSkill) {
		t.Errorf("SupportsAssetType(skill) = false, want true")
	}

	for _, unsupported := range []asset.Type{
		asset.TypeAgent,
		asset.TypeMCP,
		asset.TypeRule,
		asset.TypeCommand,
		asset.TypeHook,
	} {
		if c.SupportsAssetType(unsupported) {
			t.Errorf("SupportsAssetType(%s) = true, want false (skills-only)", unsupported.Key)
		}
	}
}
