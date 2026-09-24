package kiro

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sleuth-io/sx/v2/internal/bootstrap"
	"github.com/sleuth-io/sx/v2/internal/clients"
	"github.com/sleuth-io/sx/v2/internal/clients/kiro/handlers"
)

// hermeticHome points os.UserHomeDir() at a scratch directory for the duration
// of a test, so no case reads or writes the developer's real home. USERPROFILE
// is set alongside HOME because os.UserHomeDir() consults it on Windows.
func hermeticHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

// assertNoNestedConfigDir fails when a resolved path adds a .kiro segment below
// base. KIRO_HOME *is* the config root, so appending .kiro under it would write
// to $KIRO_HOME/.kiro/skills while kiro-cli reads $KIRO_HOME/skills — the exact
// mismatch this test guards. Only the portion below base is inspected: the
// ambient temp prefix may itself legitimately contain a .kiro directory.
func assertNoNestedConfigDir(t *testing.T, base, got string) {
	t.Helper()
	rel, err := filepath.Rel(base, got)
	if err != nil {
		t.Fatalf("filepath.Rel(%q, %q) returned error: %v", base, got, err)
	}
	for segment := range strings.SplitSeq(filepath.ToSlash(rel), "/") {
		if segment == handlers.ConfigDir {
			t.Errorf("resolved path %q adds a %q segment below %q; KIRO_HOME is the config root, not a parent of it", got, handlers.ConfigDir, base)
		}
	}
}

// TestDetermineTargetBaseKiroHomeIsConfigRoot asserts that KIRO_HOME replaces
// ~/.kiro wholesale rather than naming a parent under which .kiro is created,
// matching https://kiro.dev/docs/configuration/. Repo and path scopes must keep
// resolving against RepoRoot and never consult KIRO_HOME. Every case is
// hermetic: t.TempDir()/t.Setenv only, never the real HOME.
func TestDetermineTargetBaseKiroHomeIsConfigRoot(t *testing.T) {
	c := NewClient()

	t.Run("global scope uses KIRO_HOME as the config root", func(t *testing.T) {
		// Isolate HOME so a fallback would be visible as a scratch path, never
		// the developer's own. The equality check below is what proves no
		// fallback happened: kiroHomeDir and the scratch home are distinct.
		hermeticHome(t)
		kiroHomeDir := t.TempDir()
		t.Setenv("KIRO_HOME", kiroHomeDir)

		got, err := c.determineTargetBase(&clients.InstallScope{Type: clients.ScopeGlobal})
		if err != nil {
			t.Fatalf("determineTargetBase returned error: %v", err)
		}

		if got != kiroHomeDir {
			t.Errorf("global target base = %q, want %q (KIRO_HOME verbatim)", got, kiroHomeDir)
		}

		// The regression: .kiro appended under KIRO_HOME.
		if nested := filepath.Join(kiroHomeDir, handlers.ConfigDir); got == nested {
			t.Errorf("global target base = %q, but KIRO_HOME is the config root; %q must not be appended", nested, handlers.ConfigDir)
		}
		assertNoNestedConfigDir(t, kiroHomeDir, got)

		// Skills land exactly where kiro-cli reads them: $KIRO_HOME/skills.
		skills := filepath.Join(got, handlers.DirSkills)
		if want := filepath.Join(kiroHomeDir, handlers.DirSkills); skills != want {
			t.Errorf("global skills dir = %q, want %q", skills, want)
		}
	})

	t.Run("global scope defaults to ~/.kiro when KIRO_HOME unset", func(t *testing.T) {
		home := hermeticHome(t)
		// t.Setenv registers the restore, so clearing here cannot leak.
		t.Setenv("KIRO_HOME", "")

		got, err := c.determineTargetBase(&clients.InstallScope{Type: clients.ScopeGlobal})
		if err != nil {
			t.Fatalf("determineTargetBase returned error: %v", err)
		}

		if want := filepath.Join(home, handlers.ConfigDir); got != want {
			t.Errorf("global target base = %q, want default %q", got, want)
		}
		if want := filepath.Join(home, handlers.ConfigDir, handlers.DirSkills); filepath.Join(got, handlers.DirSkills) != want {
			t.Errorf("global skills dir = %q, want %q", filepath.Join(got, handlers.DirSkills), want)
		}
	})

	t.Run("whitespace-only KIRO_HOME is treated as unset", func(t *testing.T) {
		home := hermeticHome(t)
		t.Setenv("KIRO_HOME", "   \t  ")

		got, err := c.determineTargetBase(&clients.InstallScope{Type: clients.ScopeGlobal})
		if err != nil {
			t.Fatalf("determineTargetBase returned error: %v", err)
		}

		if want := filepath.Join(home, handlers.ConfigDir); got != want {
			t.Errorf("global target base = %q, want default %q for a blank KIRO_HOME", got, want)
		}
	})

	t.Run("leading tilde in KIRO_HOME expands to home", func(t *testing.T) {
		home := hermeticHome(t)
		t.Setenv("KIRO_HOME", "~/kiro-profiles/work")

		got, err := c.determineTargetBase(&clients.InstallScope{Type: clients.ScopeGlobal})
		if err != nil {
			t.Fatalf("determineTargetBase returned error: %v", err)
		}

		if want := filepath.Join(home, "kiro-profiles", "work"); got != want {
			t.Errorf("global target base = %q, want expanded %q", got, want)
		}
		if strings.HasPrefix(got, "~") {
			t.Errorf("global target base %q still carries an unexpanded tilde", got)
		}
		assertNoNestedConfigDir(t, home, got)
	})

	t.Run("bare tilde KIRO_HOME expands to home itself", func(t *testing.T) {
		home := hermeticHome(t)
		t.Setenv("KIRO_HOME", "~")

		got, err := c.determineTargetBase(&clients.InstallScope{Type: clients.ScopeGlobal})
		if err != nil {
			t.Fatalf("determineTargetBase returned error: %v", err)
		}

		if got != home {
			t.Errorf("global target base = %q, want %q", got, home)
		}
	})

	t.Run("relative KIRO_HOME resolves to an absolute path", func(t *testing.T) {
		hermeticHome(t)
		workDir := t.TempDir()
		t.Chdir(workDir)
		t.Setenv("KIRO_HOME", filepath.Join("profiles", "staging"))

		got, err := c.determineTargetBase(&clients.InstallScope{Type: clients.ScopeGlobal})
		if err != nil {
			t.Fatalf("determineTargetBase returned error: %v", err)
		}

		if !filepath.IsAbs(got) {
			t.Errorf("global target base = %q, want an absolute path", got)
		}
		// Compare against the evaluated working directory: t.TempDir() can sit
		// under a symlinked prefix (/var -> /private/var on macOS), which
		// filepath.Abs resolves via the process cwd.
		wantSuffix := filepath.Join("profiles", "staging")
		if !strings.HasSuffix(got, wantSuffix) {
			t.Errorf("global target base = %q, want it to end with %q", got, wantSuffix)
		}
		// Base the segment check on the resolved path minus the expected
		// suffix, so only what the resolver added is inspected.
		assertNoNestedConfigDir(t, strings.TrimSuffix(got, wantSuffix), got)
	})

	t.Run("repo scope is unaffected by KIRO_HOME", func(t *testing.T) {
		hermeticHome(t)
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
		if strings.HasPrefix(got, kiroHomeDir) {
			t.Errorf("repo target base %q was redirected under KIRO_HOME %q", got, kiroHomeDir)
		}
	})

	t.Run("path scope is unaffected by KIRO_HOME", func(t *testing.T) {
		hermeticHome(t)
		kiroHomeDir := t.TempDir()
		t.Setenv("KIRO_HOME", kiroHomeDir)

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
			t.Errorf("path target base = %q, want %q (must stay rooted at RepoRoot)", got, want)
		}
		if strings.HasPrefix(got, kiroHomeDir) {
			t.Errorf("path target base %q was redirected under KIRO_HOME %q", got, kiroHomeDir)
		}
	})
}

// TestDetermineTargetBaseScopeIsolation asserts that KIRO_HOME resolution is
// reached only by the scopes that actually use it. A repo- or path-scoped
// install resolves against RepoRoot and must succeed even when KIRO_HOME cannot
// be resolved at all — resolving it up front, before the scope switch, made an
// unusable KIRO_HOME fail installs that never consult the variable.
//
// The unresolvable value here is "~/relocated" with no home to expand it
// against, which is the one failure mode reachable hermetically.
func TestDetermineTargetBaseScopeIsolation(t *testing.T) {
	c := NewClient()

	// withUnresolvableKiroHome makes kiroConfigDir fail: a leading ~ needs
	// os.UserHomeDir(), which errors when HOME is empty.
	withUnresolvableKiroHome := func(t *testing.T) {
		t.Helper()
		t.Setenv("HOME", "")
		t.Setenv("USERPROFILE", "")
		t.Setenv("KIRO_HOME", "~/relocated")
	}

	t.Run("global scope surfaces the KIRO_HOME failure", func(t *testing.T) {
		withUnresolvableKiroHome(t)

		if _, err := c.determineTargetBase(&clients.InstallScope{Type: clients.ScopeGlobal}); err == nil {
			t.Fatal("global scope returned no error for an unresolvable KIRO_HOME")
		}
	})

	t.Run("repo scope succeeds despite an unresolvable KIRO_HOME", func(t *testing.T) {
		withUnresolvableKiroHome(t)
		repoRoot := t.TempDir()

		got, err := c.determineTargetBase(&clients.InstallScope{
			Type:     clients.ScopeRepository,
			RepoRoot: repoRoot,
		})
		if err != nil {
			t.Fatalf("repo scope must not consult KIRO_HOME, but returned error: %v", err)
		}

		if want := filepath.Join(repoRoot, handlers.ConfigDir); got != want {
			t.Errorf("repo target base = %q, want %q", got, want)
		}
	})

	t.Run("path scope succeeds despite an unresolvable KIRO_HOME", func(t *testing.T) {
		withUnresolvableKiroHome(t)
		repoRoot := t.TempDir()

		got, err := c.determineTargetBase(&clients.InstallScope{
			Type:     clients.ScopePath,
			RepoRoot: repoRoot,
			Path:     filepath.Join("services", "api"),
		})
		if err != nil {
			t.Fatalf("path scope must not consult KIRO_HOME, but returned error: %v", err)
		}

		if want := filepath.Join(repoRoot, "services", "api", handlers.ConfigDir); got != want {
			t.Errorf("path target base = %q, want %q", got, want)
		}
	})

	t.Run("repo scope still requires a RepoRoot", func(t *testing.T) {
		withUnresolvableKiroHome(t)

		if _, err := c.determineTargetBase(&clients.InstallScope{Type: clients.ScopeRepository}); err == nil {
			t.Fatal("repo scope with no RepoRoot returned no error")
		}
	})
}

// TestMCPServerWritesUnderKiroHome covers the global MCP config writers, which
// determineTargetBase does not reach: they resolve the config root themselves.
// The settings/mcp.json they write must land at $KIRO_HOME/settings/mcp.json,
// where kiro-cli reads it, and must not create a nested $KIRO_HOME/.kiro.
// Hermetic: t.TempDir()/t.Setenv only, never the real HOME.
func TestMCPServerWritesUnderKiroHome(t *testing.T) {
	hermeticHome(t)
	kiroHomeDir := t.TempDir()
	t.Setenv("KIRO_HOME", kiroHomeDir)

	c := NewClient()
	const serverName = "sx-test"

	if err := c.installMCPServerFromConfig(&bootstrap.MCPServerConfig{
		Name:    serverName,
		Command: "sx",
		Args:    []string{"serve"},
		Env:     map[string]string{"SX_TEST": "1"},
	}); err != nil {
		t.Fatalf("installMCPServerFromConfig returned error: %v", err)
	}

	mcpPath := filepath.Join(kiroHomeDir, handlers.DirSettings, "mcp.json")
	data, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatalf("expected MCP config at %q: %v", mcpPath, err)
	}

	// The regression: a .kiro appended under KIRO_HOME, so sx writes to
	// $KIRO_HOME/.kiro/settings/mcp.json while kiro-cli reads $KIRO_HOME/settings.
	nested := filepath.Join(kiroHomeDir, handlers.ConfigDir)
	if _, err := os.Stat(nested); !os.IsNotExist(err) {
		t.Errorf("%q exists; KIRO_HOME is the config root, not a parent of %q", nested, handlers.ConfigDir)
	}

	var config handlers.MCPConfig
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("mcp.json is not valid JSON: %v", err)
	}
	if _, ok := config.MCPServers[serverName]; !ok {
		t.Fatalf("mcp.json has no %q server entry, got %v", serverName, config.MCPServers)
	}

	if err := c.uninstallMCPServerByName(serverName); err != nil {
		t.Fatalf("uninstallMCPServerByName returned error: %v", err)
	}

	data, err = os.ReadFile(mcpPath)
	if err != nil {
		t.Fatalf("expected MCP config to remain at %q after uninstall: %v", mcpPath, err)
	}
	config = handlers.MCPConfig{}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("mcp.json is not valid JSON after uninstall: %v", err)
	}
	if _, ok := config.MCPServers[serverName]; ok {
		t.Errorf("mcp.json still carries the %q server entry after uninstall", serverName)
	}
}
