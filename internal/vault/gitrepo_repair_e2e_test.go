package vault

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/manifest"
	"github.com/sleuth-io/sx/internal/mgmt"
)

// seedGitVaultWithUserScope creates a bare "remote" whose sx.toml scopes
// personal-skill to a single user, clones it through a GitVault, and pins a git
// identity in the clone. Returns the vault and the bare remote path.
func seedGitVaultWithUserScope(t *testing.T) (*GitVault, string) {
	t.Helper()
	mgmt.ResetActorCache()
	t.Setenv("SX_CACHE_DIR", t.TempDir())

	remoteDir := filepath.Join(t.TempDir(), "vault.git")
	gitRun(t, "", "init", "--bare", "-b", "main", remoteDir)

	seedDir := filepath.Join(t.TempDir(), "seed")
	gitRun(t, "", "init", "-b", "main", seedDir)
	gitRun(t, seedDir, "config", "user.email", "seed@example.com")
	gitRun(t, seedDir, "config", "user.name", "Seed")
	if err := manifest.Save(seedDir, &manifest.Manifest{
		SchemaVersion: manifest.CurrentSchemaVersion,
		Assets: []manifest.Asset{
			{
				Name:       "personal-skill",
				Version:    "1",
				Type:       asset.TypeSkill,
				SourceHTTP: &manifest.SourceHTTP{URL: "https://example.com/personal-skill.zip"},
				Scopes: []manifest.Scope{
					{Kind: manifest.ScopeKindUser, User: "ipanker@sleuth.io"},
				},
			},
		},
	}); err != nil {
		t.Fatalf("seed manifest: %v", err)
	}
	gitRun(t, seedDir, "add", ".")
	gitRun(t, seedDir, "commit", "-m", "seed personal-skill scoped to user")
	gitRun(t, seedDir, "remote", "add", "origin", remoteDir)
	gitRun(t, seedDir, "push", "origin", "main")

	v, err := NewGitVault("file://" + remoteDir)
	if err != nil {
		t.Fatalf("NewGitVault: %v", err)
	}
	// Force the clone, then pin a git identity inside it.
	if _, _, _, err := v.GetLockFile(context.Background(), ""); err != nil {
		t.Fatalf("GetLockFile (forces clone): %v", err)
	}
	gitRun(t, v.repoPath, "config", "user.email", "alice@example.com")
	gitRun(t, v.repoPath, "config", "user.name", "Alice")
	return v, remoteDir
}

func personalSkillUserScope(t *testing.T, vaultRoot string) string {
	t.Helper()
	m, _, err := manifest.LoadOrMigrate(vaultRoot)
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	a := m.FindAsset("personal-skill")
	if a == nil {
		return ""
	}
	for _, s := range a.Scopes {
		if s.Kind == manifest.ScopeKindUser {
			return s.User
		}
	}
	return ""
}

// TestGitVault_RepairVaultClone_DiscardsUnpushedManifestCommit is the SD-10170
// regression: an interrupted `sx add` can leave the git vault cache with an
// unpushed commit that strips an asset's scope, so the CLI reads the asset as
// global while the remote still has the correct user scope. RepairVaultClone
// (what `sx install --repair` calls) must discard that local commit and restore
// the cache to the remote's authoritative state.
func TestGitVault_RepairVaultClone_DiscardsUnpushedManifestCommit(t *testing.T) {
	v, remoteDir := seedGitVaultWithUserScope(t)
	ctx := context.Background()

	// Sanity: the freshly cloned cache has the user scope.
	if got := personalSkillUserScope(t, v.repoPath); got != "ipanker@sleuth.io" {
		t.Fatalf("precondition: expected user scope in clone, got %q", got)
	}
	remoteTip := gitOut(t, v.repoPath, "rev-parse", "origin/main")

	// Simulate the bug: locally strip the scope and commit, but never push.
	m, _, err := manifest.LoadOrMigrate(v.repoPath)
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	m.FindAsset("personal-skill").Scopes = nil
	if err := manifest.Save(v.repoPath, m); err != nil {
		t.Fatalf("save stripped manifest: %v", err)
	}
	gitRun(t, v.repoPath, "add", ".")
	gitRun(t, v.repoPath, "commit", "-m", "Add personal-skill 1")
	badTip := gitOut(t, v.repoPath, "rev-parse", "HEAD")
	if got := personalSkillUserScope(t, v.repoPath); got != "" {
		t.Fatalf("precondition: expected scope stripped locally, got %q", got)
	}

	// Repair must report the discarded commit and restore the remote state.
	discarded, err := v.RepairVaultClone(ctx)
	if err != nil {
		t.Fatalf("RepairVaultClone: %v", err)
	}
	if discarded == "" || !strings.HasPrefix(badTip, discarded) {
		t.Errorf("expected discarded tip to be the bad commit %s, got %q", badTip, discarded)
	}
	if got := personalSkillUserScope(t, v.repoPath); got != "ipanker@sleuth.io" {
		t.Errorf("after repair, expected user scope restored from remote, got %q", got)
	}
	if head := gitOut(t, v.repoPath, "rev-parse", "HEAD"); head != remoteTip {
		t.Errorf("after repair, HEAD = %s, want remote tip %s", head, remoteTip)
	}
	// The remote must be untouched by the repair (it was already correct).
	if got := remoteCommitCount(t, remoteDir); got != 1 {
		t.Errorf("repair should not have pushed anything; remote commit count = %d, want 1", got)
	}
}

// TestGitVault_RepairVaultClone_PreservesUsageCommit verifies repair keeps (and
// pushes) queued usage-event appends while still discarding the manifest
// divergence layered on top of them.
func TestGitVault_RepairVaultClone_PreservesUsageCommit(t *testing.T) {
	v, remoteDir := seedGitVaultWithUserScope(t)
	ctx := context.Background()

	// A legit, unpushed usage commit (the kind maybePushUsage defers).
	usageFile := filepath.Join(v.repoPath, mgmt.UsageDirName, "2026-06.jsonl")
	if err := os.MkdirAll(filepath.Dir(usageFile), 0755); err != nil {
		t.Fatalf("mkdir usage: %v", err)
	}
	if err := os.WriteFile(usageFile, []byte(`{"actor":"alice@example.com","asset":"personal-skill"}`+"\n"), 0644); err != nil {
		t.Fatalf("write usage: %v", err)
	}
	gitRun(t, v.repoPath, "add", ".")
	gitRun(t, v.repoPath, "commit", "-m", "Record usage events")

	// A bad manifest-strip commit layered on top, unpushed.
	m, _, err := manifest.LoadOrMigrate(v.repoPath)
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	m.FindAsset("personal-skill").Scopes = nil
	if err := manifest.Save(v.repoPath, m); err != nil {
		t.Fatalf("save stripped manifest: %v", err)
	}
	gitRun(t, v.repoPath, "add", ".")
	gitRun(t, v.repoPath, "commit", "-m", "Add personal-skill 1")

	if _, err := v.RepairVaultClone(ctx); err != nil {
		t.Fatalf("RepairVaultClone: %v", err)
	}

	// Manifest scope restored from remote...
	if got := personalSkillUserScope(t, v.repoPath); got != "ipanker@sleuth.io" {
		t.Errorf("after repair, expected user scope restored, got %q", got)
	}
	// ...but the usage append survived and was pushed to the remote.
	if _, statErr := os.Stat(usageFile); statErr != nil {
		t.Errorf("usage file should survive repair: %v", statErr)
	}
	if !remoteHasUsageCommit(t, remoteDir) {
		t.Errorf("repair should have pushed the preserved usage commit to the remote")
	}
}
