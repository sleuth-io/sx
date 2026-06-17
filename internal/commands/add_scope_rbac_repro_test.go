package commands

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/manifest"
	"github.com/sleuth-io/sx/internal/mgmt"
	vaultpkg "github.com/sleuth-io/sx/internal/vault"
)

// TestRepro_NonAdminCannotAddTeamScope reproduces the user report: re-scoping a
// skill in the editor (Edited diff) must NOT let a non-team-admin ADD a team
// scope. This drives the real command apply path (updateLockFile ->
// applyScopeEdit -> bulkSetInstallTargets) against a path vault whose git
// identity is a non-admin.
func TestRepro_NonAdminCannotAddTeamScope(t *testing.T) {
	mgmt.ResetActorCache()
	dir := t.TempDir()
	reproGit(t, dir, "init")
	reproGit(t, dir, "config", "user.email", "mallory@example.com")
	reproGit(t, dir, "config", "user.name", "Mallory")

	// platform is admined by alice; mallory is not a member or admin.
	m := &manifest.Manifest{
		SchemaVersion: manifest.CurrentSchemaVersion,
		Assets: []manifest.Asset{{
			Name: "my-skill", Version: "1.0.0", Type: asset.TypeSkill,
			SourceHTTP: &manifest.SourceHTTP{URL: "https://example.com/s.zip"},
			Scopes:     []manifest.Scope{{Kind: manifest.ScopeKindUser, User: "mallory@example.com"}},
		}},
		Teams: []manifest.Team{{Name: "platform", Members: []string{"alice@example.com"}, Admins: []string{"alice@example.com"}}},
		Org:   &manifest.Org{Admins: []string{"boss@example.com"}},
	}
	if err := manifest.Save(dir, m); err != nil {
		t.Fatalf("seed manifest: %v", err)
	}

	v, err := vaultpkg.NewPathVault("file://" + dir)
	if err != nil {
		t.Fatalf("NewPathVault: %v", err)
	}

	cmd := &cobra.Command{}
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	out := newOutputHelper(cmd)

	lockAsset := &lockfile.Asset{Name: "my-skill", Version: "1.0.0", Type: asset.TypeSkill}
	// This is exactly what the file-backed editor returns when the user picks
	// "Add a team scope" for platform (Edited diff: add team, remove nothing).
	result := &scopeResult{
		Edited: true,
		Added:  []vaultpkg.InstallTarget{{Kind: vaultpkg.InstallKindTeam, Team: "platform"}},
	}

	applyErr := updateLockFile(context.Background(), out, v, lockAsset, result)
	t.Logf("updateLockFile err = %v\noutput:\n%s", applyErr, buf.String())

	// The team scope must NOT have been written.
	loaded, ok, lerr := manifest.Load(dir)
	if lerr != nil || !ok {
		t.Fatalf("load manifest: ok=%v err=%v", ok, lerr)
	}
	a := loaded.FindAsset("my-skill")
	for _, s := range a.Scopes {
		if s.Kind == manifest.ScopeKindTeam && s.Team == "platform" {
			t.Fatalf("BUG REPRODUCED: non-admin added team scope; scopes=%+v", a.Scopes)
		}
	}
}

// TestRepro_NonAdminCannotAddTeamScope_GitVault is the git-vault counterpart —
// the actual setup in the report: a cached clone + runInVaultTx. The seed scopes
// my-skill to user:mallory; mallory (non-admin) then tries to add team platform.
func TestRepro_NonAdminCannotAddTeamScope_GitVault(t *testing.T) {
	mgmt.ResetActorCache()
	base := t.TempDir()
	t.Setenv("HOME", base)
	t.Setenv("XDG_CONFIG_HOME", base+"/.config")
	t.Setenv("XDG_CACHE_HOME", base+"/.cache")
	t.Setenv("SX_CONFIG_DIR", base+"/.config/sx")
	t.Setenv("SX_CACHE_DIR", base+"/.cache/sx")

	bare := base + "/remote.git"
	if err := mkdirAllRepro(bare); err != nil {
		t.Fatal(err)
	}
	reproGit(t, "", "init", "--bare", "-b", "main", bare)
	seed := base + "/seed"
	if err := mkdirAllRepro(seed); err != nil {
		t.Fatal(err)
	}
	reproGit(t, "", "init", "-b", "main", seed)
	reproGit(t, seed, "config", "user.email", "mallory@example.com")
	reproGit(t, seed, "config", "user.name", "Mallory")
	if err := manifest.Save(seed, &manifest.Manifest{
		SchemaVersion: manifest.CurrentSchemaVersion,
		Assets: []manifest.Asset{{
			Name: "my-skill", Version: "1.0.0", Type: asset.TypeSkill,
			SourcePath: &manifest.SourcePath{Path: "./assets/my-skill/1.0.0"},
			Scopes:     []manifest.Scope{{Kind: manifest.ScopeKindUser, User: "mallory@example.com"}},
		}},
		Teams: []manifest.Team{{Name: "platform", Members: []string{"alice@example.com"}, Admins: []string{"alice@example.com"}}},
		Org:   &manifest.Org{Admins: []string{"boss@example.com"}},
	}); err != nil {
		t.Fatalf("seed manifest: %v", err)
	}
	reproGit(t, seed, "add", ".")
	reproGit(t, seed, "commit", "-m", "seed")
	reproGit(t, seed, "remote", "add", "origin", bare)
	reproGit(t, seed, "push", "origin", "main")

	// Global git identity for the clone the vault makes (no per-clone config set).
	reproGit(t, "", "config", "--global", "user.email", "mallory@example.com")
	reproGit(t, "", "config", "--global", "user.name", "Mallory")

	v, err := vaultpkg.NewGitVault("file://" + bare)
	if err != nil {
		t.Fatalf("NewGitVault: %v", err)
	}

	cmd := &cobra.Command{}
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	out := newOutputHelper(cmd)

	lockAsset := &lockfile.Asset{Name: "my-skill", Version: "1.0.0", Type: asset.TypeSkill}
	result := &scopeResult{
		Edited: true,
		Added:  []vaultpkg.InstallTarget{{Kind: vaultpkg.InstallKindTeam, Team: "platform"}},
	}

	applyErr := updateLockFile(context.Background(), out, v, lockAsset, result)
	t.Logf("updateLockFile err = %v\noutput:\n%s", applyErr, buf.String())

	remoteToml := gitOutRepro(t, bare, "show", "main:sx.toml")
	if strings.Contains(remoteToml, "platform") &&
		strings.Contains(remoteToml, `kind = "team"`) {
		t.Fatalf("BUG REPRODUCED: non-admin added team scope on git vault; remote sx.toml:\n%s", remoteToml)
	}
}

func mkdirAllRepro(dir string) error {
	return execMkdir(dir)
}

func execMkdir(dir string) error {
	c := exec.Command("mkdir", "-p", dir)
	return c.Run()
}

func gitOutRepro(t *testing.T, bare string, args ...string) string {
	t.Helper()
	full := append([]string{"--git-dir", bare}, args...)
	c := exec.Command("git", full...)
	outBytes, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", full, err, outBytes)
	}
	return string(outBytes)
}

func reproGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	if outBytes, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, outBytes)
	}
}
