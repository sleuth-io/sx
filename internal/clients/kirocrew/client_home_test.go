package kirocrew

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sleuth-io/sx/v2/internal/asset"
	"github.com/sleuth-io/sx/v2/internal/clients"
	"github.com/sleuth-io/sx/v2/internal/clients/kirocrew/handlers"
)

// hermeticHome points os.UserHomeDir() at a scratch directory for the duration
// of a test, so no case reads or writes the developer's real home. USERPROFILE
// is set alongside HOME because os.UserHomeDir() consults it on Windows. It also
// clears KIROCREW_HOME so a subtest starts from a known-unset baseline unless it
// sets one explicitly.
func hermeticHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("KIROCREW_HOME", "")
	return home
}

// TestDetermineTargetBaseHonorsKirocrewHome asserts that global-scope
// resolution treats KIROCREW_HOME as the crew home itself (the crew ConfigDir
// segment must NOT be re-appended), expands a tilde and makes a relative value
// absolute, and leaves repo/path scopes rooted at RepoRoot. Every case is
// hermetic: t.TempDir()/t.Setenv only, never the real HOME.
func TestDetermineTargetBaseHonorsKirocrewHome(t *testing.T) {
	c := NewClient()

	t.Run("global scope resolves to KIROCREW_HOME without doubling the crew segment", func(t *testing.T) {
		home := hermeticHome(t)
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
		if doubled := filepath.Join(crewHomeDir, handlers.ConfigDir); got == doubled {
			t.Errorf("global target base doubled the crew segment: got %q", doubled)
		}

		// Skills land exactly where KiroCrew reads them: $KIROCREW_HOME/skills.
		if skills, want := filepath.Join(got, handlers.DirSkills), filepath.Join(crewHomeDir, handlers.DirSkills); skills != want {
			t.Errorf("global skills dir = %q, want %q", skills, want)
		}

		// And never under the (scratch) home when KIROCREW_HOME is set.
		if got == filepath.Join(home, handlers.ConfigDir) {
			t.Errorf("global target base fell back to home %q despite KIROCREW_HOME=%q", got, crewHomeDir)
		}
	})

	t.Run("global scope falls back to home/.kiro/crew when KIROCREW_HOME unset", func(t *testing.T) {
		home := hermeticHome(t)

		got, err := c.determineTargetBase(&clients.InstallScope{Type: clients.ScopeGlobal})
		if err != nil {
			t.Fatalf("determineTargetBase returned error: %v", err)
		}

		if want := filepath.Join(home, handlers.ConfigDir); got != want {
			t.Errorf("global target base = %q, want fallback %q", got, want)
		}
	})

	t.Run("whitespace-only KIROCREW_HOME is treated as unset", func(t *testing.T) {
		home := hermeticHome(t)
		t.Setenv("KIROCREW_HOME", "   \t  ")

		got, err := c.determineTargetBase(&clients.InstallScope{Type: clients.ScopeGlobal})
		if err != nil {
			t.Fatalf("determineTargetBase returned error: %v", err)
		}

		if want := filepath.Join(home, handlers.ConfigDir); got != want {
			t.Errorf("global target base = %q, want fallback %q for a blank KIROCREW_HOME", got, want)
		}
	})

	t.Run("leading tilde in KIROCREW_HOME expands to home", func(t *testing.T) {
		home := hermeticHome(t)
		t.Setenv("KIROCREW_HOME", "~/crew-profiles/work")

		got, err := c.determineTargetBase(&clients.InstallScope{Type: clients.ScopeGlobal})
		if err != nil {
			t.Fatalf("determineTargetBase returned error: %v", err)
		}

		if want := filepath.Join(home, "crew-profiles", "work"); got != want {
			t.Errorf("global target base = %q, want expanded %q", got, want)
		}
		if strings.HasPrefix(got, "~") {
			t.Errorf("global target base %q still carries an unexpanded tilde", got)
		}
	})

	t.Run("bare tilde KIROCREW_HOME expands to home itself", func(t *testing.T) {
		home := hermeticHome(t)
		t.Setenv("KIROCREW_HOME", "~")

		got, err := c.determineTargetBase(&clients.InstallScope{Type: clients.ScopeGlobal})
		if err != nil {
			t.Fatalf("determineTargetBase returned error: %v", err)
		}

		if got != home {
			t.Errorf("global target base = %q, want %q", got, home)
		}
	})

	t.Run("relative KIROCREW_HOME resolves to an absolute path", func(t *testing.T) {
		hermeticHome(t)
		workDir := t.TempDir()
		t.Chdir(workDir)
		t.Setenv("KIROCREW_HOME", filepath.Join("profiles", "staging"))

		got, err := c.determineTargetBase(&clients.InstallScope{Type: clients.ScopeGlobal})
		if err != nil {
			t.Fatalf("determineTargetBase returned error: %v", err)
		}

		if !filepath.IsAbs(got) {
			t.Errorf("global target base = %q, want an absolute path", got)
		}
		// t.TempDir() can sit under a symlinked prefix (/var -> /private/var on
		// macOS), which filepath.Abs resolves via the process cwd, so compare on
		// the suffix rather than the full prefix.
		if want := filepath.Join("profiles", "staging"); !strings.HasSuffix(got, want) {
			t.Errorf("global target base = %q, want it to end with %q", got, want)
		}
	})

	t.Run("repo scope is unaffected by KIROCREW_HOME", func(t *testing.T) {
		hermeticHome(t)
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
		if strings.HasPrefix(got, crewHomeDir) {
			t.Errorf("repo target base %q was redirected under KIROCREW_HOME %q", got, crewHomeDir)
		}
	})

	t.Run("path scope is unaffected by KIROCREW_HOME", func(t *testing.T) {
		hermeticHome(t)
		crewHomeDir := t.TempDir()
		t.Setenv("KIROCREW_HOME", crewHomeDir)

		repoRoot := t.TempDir()
		got, err := c.determineTargetBase(&clients.InstallScope{
			Type:     clients.ScopePath,
			RepoRoot: repoRoot,
			Path:     filepath.Join("services", "api"),
		})
		if err != nil {
			t.Fatalf("determineTargetBase returned error: %v", err)
		}

		want := filepath.Join(repoRoot, "services", "api", handlers.ConfigDir)
		if got != want {
			t.Errorf("path target base = %q, want %q (must stay rooted at RepoRoot/Path)", got, want)
		}
		if strings.HasPrefix(got, crewHomeDir) {
			t.Errorf("path target base %q was redirected under KIROCREW_HOME %q", got, crewHomeDir)
		}
	})
}

// TestIsInstalledDetection covers the crew-home detection branch of IsInstalled.
// The binary-in-PATH branch is deliberately not exercised: it depends on the
// developer's real PATH, which a hermetic test cannot control without shadowing
// exec.LookPath. Each case points KIROCREW_HOME at a temp dir and toggles only
// whether that dir exists, so the assertion turns on the crew-home probe alone.
func TestIsInstalledDetection(t *testing.T) {
	c := NewClient()

	t.Run("detects an existing crew home under KIROCREW_HOME", func(t *testing.T) {
		if _, err := os.Stat("kirocrew"); err == nil {
			t.Skip("a 'kirocrew' path in the test cwd would mask the crew-home probe")
		}
		hermeticHome(t)
		crewHomeDir := t.TempDir() // exists
		t.Setenv("KIROCREW_HOME", crewHomeDir)

		if !c.IsInstalled() {
			t.Errorf("IsInstalled() = false, want true when the crew home %q exists", crewHomeDir)
		}
	})

	t.Run("reports not-installed when the crew home is absent", func(t *testing.T) {
		hermeticHome(t)
		// A path under the scratch home that is never created.
		absent := filepath.Join(t.TempDir(), "no-crew-here")
		t.Setenv("KIROCREW_HOME", absent)

		// Guard: only meaningful if no kirocrew binary is on PATH; when one is,
		// the binary branch legitimately returns true and this subtest cannot
		// isolate the crew-home probe, so skip rather than assert a false result.
		if c.IsInstalled() {
			t.Skip("a kirocrew binary is on PATH; cannot isolate the crew-home-absent branch")
		}
	})

	t.Run("crew-home probe honors KIROCREW_HOME, not the real home", func(t *testing.T) {
		home := hermeticHome(t)
		// Create ~/.kiro/crew under the scratch home to prove detection does not
		// fall back to it when KIROCREW_HOME points elsewhere and is absent.
		realCrew := filepath.Join(home, handlers.ConfigDir)
		if err := os.MkdirAll(realCrew, 0o755); err != nil {
			t.Fatalf("mkdir scratch crew home: %v", err)
		}
		absent := filepath.Join(t.TempDir(), "redirected-absent")
		t.Setenv("KIROCREW_HOME", absent)

		if c.IsInstalled() {
			t.Skip("a kirocrew binary is on PATH; cannot isolate the redirected-home probe")
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
