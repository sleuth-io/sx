package github_copilot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sleuth-io/sx/v2/internal/asset"
	"github.com/sleuth-io/sx/v2/internal/bootstrap"
	"github.com/sleuth-io/sx/v2/internal/clients"
	"github.com/sleuth-io/sx/v2/internal/clients/github_copilot/handlers"
	"github.com/sleuth-io/sx/v2/internal/lockfile"
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

// assertNoNestedConfigDir fails when a resolved path adds a .copilot segment
// below base. COPILOT_HOME *is* the config root, so appending .copilot under it
// would write to $COPILOT_HOME/.copilot/skills while copilot reads
// $COPILOT_HOME/skills — the exact mismatch this test guards. Only the portion
// below base is inspected: the ambient temp prefix may itself legitimately
// contain a .copilot directory.
func assertNoNestedConfigDir(t *testing.T, base, got string) {
	t.Helper()
	rel, err := filepath.Rel(base, got)
	if err != nil {
		t.Fatalf("filepath.Rel(%q, %q) returned error: %v", base, got, err)
	}
	for segment := range strings.SplitSeq(filepath.ToSlash(rel), "/") {
		if segment == handlers.ConfigDir {
			t.Errorf("resolved path %q adds a %q segment below %q; COPILOT_HOME is the config root, not a parent of it", got, handlers.ConfigDir, base)
		}
	}
}

// TestDetermineTargetBaseCopilotHomeIsConfigRoot asserts that COPILOT_HOME
// replaces ~/.copilot wholesale rather than naming a parent under which
// .copilot is created, matching the GitHub docs. Repo and path scopes must keep
// resolving against RepoRoot and never consult COPILOT_HOME. Every case is
// hermetic: t.TempDir()/t.Setenv only, never the real HOME.
func TestDetermineTargetBaseCopilotHomeIsConfigRoot(t *testing.T) {
	c := NewClient()

	t.Run("global scope uses COPILOT_HOME as the config root", func(t *testing.T) {
		// Isolate HOME so a fallback would be visible as a scratch path, never
		// the developer's own. The equality check below is what proves no
		// fallback happened: copilotHomeDir and the scratch home are distinct.
		hermeticHome(t)
		copilotHomeDir := t.TempDir()
		t.Setenv("COPILOT_HOME", copilotHomeDir)

		got, err := c.determineTargetBase(&clients.InstallScope{Type: clients.ScopeGlobal})
		if err != nil {
			t.Fatalf("determineTargetBase returned error: %v", err)
		}

		if got != copilotHomeDir {
			t.Errorf("global target base = %q, want %q (COPILOT_HOME verbatim)", got, copilotHomeDir)
		}

		// The regression: .copilot appended under COPILOT_HOME.
		if nested := filepath.Join(copilotHomeDir, handlers.ConfigDir); got == nested {
			t.Errorf("global target base = %q, but COPILOT_HOME is the config root; %q must not be appended", nested, handlers.ConfigDir)
		}
		assertNoNestedConfigDir(t, copilotHomeDir, got)

		// Skills land exactly where copilot reads them: $COPILOT_HOME/skills.
		skills := filepath.Join(got, handlers.DirSkills)
		if want := filepath.Join(copilotHomeDir, handlers.DirSkills); skills != want {
			t.Errorf("global skills dir = %q, want %q", skills, want)
		}
	})

	t.Run("global scope defaults to ~/.copilot when COPILOT_HOME unset", func(t *testing.T) {
		home := hermeticHome(t)
		// t.Setenv registers the restore, so clearing here cannot leak.
		t.Setenv("COPILOT_HOME", "")

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

	t.Run("whitespace-only COPILOT_HOME is treated as unset", func(t *testing.T) {
		home := hermeticHome(t)
		t.Setenv("COPILOT_HOME", "   \t  ")

		got, err := c.determineTargetBase(&clients.InstallScope{Type: clients.ScopeGlobal})
		if err != nil {
			t.Fatalf("determineTargetBase returned error: %v", err)
		}

		if want := filepath.Join(home, handlers.ConfigDir); got != want {
			t.Errorf("global target base = %q, want default %q for a blank COPILOT_HOME", got, want)
		}
	})

	t.Run("leading tilde in COPILOT_HOME expands to home", func(t *testing.T) {
		home := hermeticHome(t)
		t.Setenv("COPILOT_HOME", "~/copilot-profiles/work")

		got, err := c.determineTargetBase(&clients.InstallScope{Type: clients.ScopeGlobal})
		if err != nil {
			t.Fatalf("determineTargetBase returned error: %v", err)
		}

		if want := filepath.Join(home, "copilot-profiles", "work"); got != want {
			t.Errorf("global target base = %q, want expanded %q", got, want)
		}
		if strings.HasPrefix(got, "~") {
			t.Errorf("global target base %q still carries an unexpanded tilde", got)
		}
		assertNoNestedConfigDir(t, home, got)
	})

	t.Run("bare tilde COPILOT_HOME expands to home itself", func(t *testing.T) {
		home := hermeticHome(t)
		t.Setenv("COPILOT_HOME", "~")

		got, err := c.determineTargetBase(&clients.InstallScope{Type: clients.ScopeGlobal})
		if err != nil {
			t.Fatalf("determineTargetBase returned error: %v", err)
		}

		if got != home {
			t.Errorf("global target base = %q, want %q", got, home)
		}
	})

	t.Run("relative COPILOT_HOME resolves to an absolute path", func(t *testing.T) {
		hermeticHome(t)
		workDir := t.TempDir()
		t.Chdir(workDir)
		t.Setenv("COPILOT_HOME", filepath.Join("profiles", "staging"))

		got, err := c.determineTargetBase(&clients.InstallScope{Type: clients.ScopeGlobal})
		if err != nil {
			t.Fatalf("determineTargetBase returned error: %v", err)
		}

		if !filepath.IsAbs(got) {
			t.Errorf("global target base = %q, want an absolute path", got)
		}
		// Checked as a suffix rather than against filepath.Join(workDir, ...):
		// t.TempDir() can sit under a symlinked prefix (/var -> /private/var on
		// macOS) that filepath.Abs resolves via the process cwd, so the resolved
		// path legitimately differs from workDir in its leading segments.
		wantSuffix := filepath.Join("profiles", "staging")
		if !strings.HasSuffix(got, wantSuffix) {
			t.Errorf("global target base = %q, want it to end with %q", got, wantSuffix)
		}
		// Base the segment check on the resolved path minus the expected
		// suffix, so only what the resolver added is inspected.
		assertNoNestedConfigDir(t, strings.TrimSuffix(got, wantSuffix), got)
	})

	t.Run("repo scope is unaffected by COPILOT_HOME", func(t *testing.T) {
		hermeticHome(t)
		copilotHomeDir := t.TempDir()
		t.Setenv("COPILOT_HOME", copilotHomeDir)

		repoRoot := t.TempDir()
		got, err := c.determineTargetBase(&clients.InstallScope{
			Type:     clients.ScopeRepository,
			RepoRoot: repoRoot,
		})
		if err != nil {
			t.Fatalf("determineTargetBase returned error: %v", err)
		}

		want := filepath.Join(repoRoot, ".github")
		if got != want {
			t.Errorf("repo target base = %q, want %q (must stay rooted at RepoRoot)", got, want)
		}
		if strings.HasPrefix(got, copilotHomeDir) {
			t.Errorf("repo target base %q was redirected under COPILOT_HOME %q", got, copilotHomeDir)
		}
	})

	t.Run("path scope is unaffected by COPILOT_HOME", func(t *testing.T) {
		hermeticHome(t)
		copilotHomeDir := t.TempDir()
		t.Setenv("COPILOT_HOME", copilotHomeDir)

		repoRoot := t.TempDir()
		got, err := c.determineTargetBase(&clients.InstallScope{
			Type:     clients.ScopePath,
			RepoRoot: repoRoot,
			Path:     filepath.Join("services", "api"),
		})
		if err != nil {
			t.Fatalf("determineTargetBase returned error: %v", err)
		}

		want := filepath.Join(repoRoot, "services", "api", ".github")
		if got != want {
			t.Errorf("path target base = %q, want %q (must stay rooted at RepoRoot)", got, want)
		}
		if strings.HasPrefix(got, copilotHomeDir) {
			t.Errorf("path target base %q was redirected under COPILOT_HOME %q", got, copilotHomeDir)
		}
	})
}

// TestDetermineTargetBaseScopeIsolation asserts that COPILOT_HOME resolution is
// reached only by the scopes that actually use it. A repo- or path-scoped
// install resolves against RepoRoot and must succeed even when COPILOT_HOME
// cannot be resolved at all — resolving it up front, before the scope switch,
// made an unusable COPILOT_HOME fail installs that never consult the variable.
//
// The unresolvable value here is "~/relocated" with no home to expand it
// against, which is the one failure mode reachable hermetically.
func TestDetermineTargetBaseScopeIsolation(t *testing.T) {
	c := NewClient()

	// withUnresolvableCopilotHome makes GlobalConfigDir fail: a leading ~ needs
	// os.UserHomeDir(), which errors when HOME is empty.
	withUnresolvableCopilotHome := func(t *testing.T) {
		t.Helper()
		t.Setenv("HOME", "")
		t.Setenv("USERPROFILE", "")
		t.Setenv("COPILOT_HOME", "~/relocated")
	}

	t.Run("global scope surfaces the COPILOT_HOME failure", func(t *testing.T) {
		withUnresolvableCopilotHome(t)

		if _, err := c.determineTargetBase(&clients.InstallScope{Type: clients.ScopeGlobal}); err == nil {
			t.Fatal("global scope returned no error for an unresolvable COPILOT_HOME")
		}
	})

	t.Run("repo scope succeeds despite an unresolvable COPILOT_HOME", func(t *testing.T) {
		withUnresolvableCopilotHome(t)
		repoRoot := t.TempDir()

		got, err := c.determineTargetBase(&clients.InstallScope{
			Type:     clients.ScopeRepository,
			RepoRoot: repoRoot,
		})
		if err != nil {
			t.Fatalf("repo scope must not consult COPILOT_HOME, but returned error: %v", err)
		}

		if want := filepath.Join(repoRoot, ".github"); got != want {
			t.Errorf("repo target base = %q, want %q", got, want)
		}
	})

	t.Run("path scope succeeds despite an unresolvable COPILOT_HOME", func(t *testing.T) {
		withUnresolvableCopilotHome(t)
		repoRoot := t.TempDir()

		got, err := c.determineTargetBase(&clients.InstallScope{
			Type:     clients.ScopePath,
			RepoRoot: repoRoot,
			Path:     filepath.Join("services", "api"),
		})
		if err != nil {
			t.Fatalf("path scope must not consult COPILOT_HOME, but returned error: %v", err)
		}

		if want := filepath.Join(repoRoot, "services", "api", ".github"); got != want {
			t.Errorf("path target base = %q, want %q", got, want)
		}
	})

	t.Run("repo scope still requires a RepoRoot", func(t *testing.T) {
		withUnresolvableCopilotHome(t)

		if _, err := c.determineTargetBase(&clients.InstallScope{Type: clients.ScopeRepository}); err == nil {
			t.Fatal("repo scope with no RepoRoot returned no error")
		}
	})
}

// TestDetermineCLIMCPConfigHonorsCopilotHome asserts the global Copilot CLI MCP
// mirror file lands at $COPILOT_HOME/mcp-config.json when the env var is set,
// and at ~/.copilot/mcp-config.json otherwise. Repo/path scopes write the
// committed .github/mcp.json and ignore COPILOT_HOME.
func TestDetermineCLIMCPConfigHonorsCopilotHome(t *testing.T) {
	c := NewClient()

	t.Run("global mirror lands under COPILOT_HOME", func(t *testing.T) {
		hermeticHome(t)
		copilotHomeDir := t.TempDir()
		t.Setenv("COPILOT_HOME", copilotHomeDir)

		got, shared := c.determineCLIMCPConfig(&clients.InstallScope{Type: clients.ScopeGlobal})
		if want := filepath.Join(copilotHomeDir, "mcp-config.json"); got != want {
			t.Errorf("global CLI MCP config = %q, want %q", got, want)
		}
		if shared {
			t.Error("global CLI MCP config marked shared; want local")
		}
		assertNoNestedConfigDir(t, copilotHomeDir, got)
	})

	t.Run("global mirror defaults to ~/.copilot when unset", func(t *testing.T) {
		home := hermeticHome(t)
		t.Setenv("COPILOT_HOME", "")

		got, _ := c.determineCLIMCPConfig(&clients.InstallScope{Type: clients.ScopeGlobal})
		if want := filepath.Join(home, handlers.ConfigDir, "mcp-config.json"); got != want {
			t.Errorf("global CLI MCP config = %q, want default %q", got, want)
		}
	})

	t.Run("repo scope ignores COPILOT_HOME", func(t *testing.T) {
		hermeticHome(t)
		copilotHomeDir := t.TempDir()
		t.Setenv("COPILOT_HOME", copilotHomeDir)

		repoRoot := t.TempDir()
		got, shared := c.determineCLIMCPConfig(&clients.InstallScope{
			Type:     clients.ScopeRepository,
			RepoRoot: repoRoot,
		})
		if want := filepath.Join(repoRoot, ".github", "mcp.json"); got != want {
			t.Errorf("repo CLI MCP config = %q, want %q", got, want)
		}
		if !shared {
			t.Error("repo CLI MCP config marked local; want shared")
		}
		if strings.HasPrefix(got, copilotHomeDir) {
			t.Errorf("repo CLI MCP config %q was redirected under COPILOT_HOME %q", got, copilotHomeDir)
		}
	})
}

// TestMCPServerWritesUnderCopilotHome covers the global Copilot CLI MCP config
// writers, which resolve the config root themselves. The mcp-config.json they
// write must land at $COPILOT_HOME/mcp-config.json, where copilot reads it, and
// must not create a nested $COPILOT_HOME/.copilot. HOME is pointed at a
// separate scratch dir with no .vscode, so the optional VS Code mirror is a
// no-op and the test exercises only the COPILOT_HOME path. Hermetic:
// t.TempDir()/t.Setenv only, never the real HOME.
func TestMCPServerWritesUnderCopilotHome(t *testing.T) {
	home := hermeticHome(t)
	copilotHomeDir := t.TempDir()
	t.Setenv("COPILOT_HOME", copilotHomeDir)

	const serverName = "sx-test"
	cfg := &bootstrap.MCPServerConfig{
		Name:    serverName,
		Command: "sx",
		Args:    []string{"serve"},
		Env:     map[string]string{"SX_TEST": "1"},
	}

	if err := installMCPServerFromConfig(home, cfg); err != nil {
		t.Fatalf("installMCPServerFromConfig returned error: %v", err)
	}

	mcpPath := filepath.Join(copilotHomeDir, "mcp-config.json")
	data, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatalf("expected MCP config at %q: %v", mcpPath, err)
	}

	// The regression: a .copilot appended under COPILOT_HOME, so sx writes to
	// $COPILOT_HOME/.copilot/mcp-config.json while copilot reads
	// $COPILOT_HOME/mcp-config.json.
	nested := filepath.Join(copilotHomeDir, handlers.ConfigDir)
	if _, err := os.Stat(nested); !os.IsNotExist(err) {
		t.Errorf("%q exists; COPILOT_HOME is the config root, not a parent of %q", nested, handlers.ConfigDir)
	}

	if !strings.Contains(string(data), serverName) {
		t.Fatalf("mcp-config.json has no %q server entry, got %s", serverName, data)
	}

	if err := uninstallMCPServerByName(home, serverName); err != nil {
		t.Fatalf("uninstallMCPServerByName returned error: %v", err)
	}

	data, err = os.ReadFile(mcpPath)
	if err != nil {
		t.Fatalf("expected MCP config to remain at %q after uninstall: %v", mcpPath, err)
	}
	if strings.Contains(string(data), serverName) {
		t.Errorf("mcp-config.json still carries the %q server entry after uninstall", serverName)
	}
}

// TestIsInstalledHonorsCopilotHome covers hasCopilotCLIConfig's config-root
// probe under a relocated COPILOT_HOME. IsInstalled also consults a VS Code
// extension under ~/.vscode, so HOME is pointed at a scratch dir with none,
// isolating the COPILOT_HOME branch. Hermetic: t.TempDir()/t.Setenv only.
func TestIsInstalledHonorsCopilotHome(t *testing.T) {
	c := NewClient()

	t.Run("true when only the relocated home holds the config root", func(t *testing.T) {
		// HOME points at a directory with no .copilot and no .vscode, so only
		// consulting COPILOT_HOME can make this true.
		absentHome := filepath.Join(t.TempDir(), "absent-home")
		t.Setenv("HOME", absentHome)
		t.Setenv("USERPROFILE", absentHome)

		copilotHomeDir := t.TempDir() // exists, so it is the install marker
		t.Setenv("COPILOT_HOME", copilotHomeDir)

		if !c.IsInstalled() {
			t.Errorf("IsInstalled() = false with COPILOT_HOME=%q present, want true", copilotHomeDir)
		}
	})

	t.Run("false when neither the relocated nor the default home holds it", func(t *testing.T) {
		hermeticHome(t)

		missing := filepath.Join(t.TempDir(), "no-such-profile")
		t.Setenv("COPILOT_HOME", missing)

		if c.IsInstalled() {
			t.Errorf("IsInstalled() = true with COPILOT_HOME=%q absent, want false", missing)
		}
	})

	t.Run("false when COPILOT_HOME is unset and the default root is absent", func(t *testing.T) {
		hermeticHome(t)
		t.Setenv("COPILOT_HOME", "")

		if c.IsInstalled() {
			t.Error("IsInstalled() = true with no config root anywhere, want false")
		}
	})

	t.Run("a file at the config root is not an install", func(t *testing.T) {
		absentHome := filepath.Join(t.TempDir(), "absent-home")
		t.Setenv("HOME", absentHome)
		t.Setenv("USERPROFILE", absentHome)

		asFile := filepath.Join(t.TempDir(), "profile-file")
		if err := os.WriteFile(asFile, []byte("not a directory"), 0o644); err != nil {
			t.Fatalf("writing %q: %v", asFile, err)
		}
		t.Setenv("COPILOT_HOME", asFile)

		if c.IsInstalled() {
			t.Errorf("IsInstalled() = true with COPILOT_HOME=%q a regular file, want false", asFile)
		}
	})
}

// TestVerifyAssetsGlobalMCPUnresolvableCopilotHome asserts verify reports a
// global MCP asset as not installed, naming the resolution failure, when the
// Copilot config root cannot be resolved. Install and uninstall error in the
// same situation; verify must not report the asset present from the VS Code
// config alone.
func TestVerifyAssetsGlobalMCPUnresolvableCopilotHome(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	t.Setenv("COPILOT_HOME", "~/relocated")

	results := NewClient().VerifyAssets(t.Context(), []*lockfile.Asset{{
		Name:    "my-mcp",
		Version: "1.0.0",
		Type:    asset.TypeMCP,
	}}, &clients.InstallScope{Type: clients.ScopeGlobal})

	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Installed {
		t.Error("Installed = true, want false when the Copilot config root is unresolvable")
	}
	if !strings.Contains(results[0].Message, "cannot resolve Copilot config directory") {
		t.Errorf("Message = %q, want it to name the resolution failure", results[0].Message)
	}
}
