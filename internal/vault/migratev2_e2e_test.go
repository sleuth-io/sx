package vault

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/manifest"
	"github.com/sleuth-io/sx/internal/mgmt"
)

// seedV1GitVault creates a bare "remote" holding a v1-format vault (exploded
// version dirs + list.txt under assets/, schema_version = 1 manifest) and
// returns a GitVault cloned from it plus the bare remote path.
func seedV1GitVault(t *testing.T) (*GitVault, string) {
	t.Helper()
	mgmt.ResetActorCache()
	t.Setenv("SX_CACHE_DIR", t.TempDir())

	remoteDir := filepath.Join(t.TempDir(), "vault.git")
	gitRun(t, "", "init", "--bare", "-b", "main", remoteDir)

	seedDir := filepath.Join(t.TempDir(), "seed")
	gitRun(t, "", "init", "-b", "main", seedDir)
	gitRun(t, seedDir, "config", "user.email", "seed@example.com")
	gitRun(t, seedDir, "config", "user.name", "Seed")

	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(seedDir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	for _, v := range []string{"1.0", "2.0"} {
		write("assets/chat/"+v+"/SKILL.md", "# chat "+v)
		write("assets/chat/"+v+"/metadata.toml", "[asset]\nname = \"chat\"\nversion = \""+v+"\"\ntype = \"skill\"\n")
	}
	write("assets/chat/list.txt", "1.0\n2.0\n")

	if err := manifest.Save(seedDir, &manifest.Manifest{
		SchemaVersion: 1,
		Assets: []manifest.Asset{
			{
				Name: "chat", Version: "2.0", Type: asset.TypeSkill,
				SourcePath: &manifest.SourcePath{Path: "./assets/chat/2.0"},
			},
		},
	}); err != nil {
		t.Fatalf("seed manifest: %v", err)
	}
	gitRun(t, seedDir, "add", ".")
	gitRun(t, seedDir, "commit", "-m", "seed v1 vault")
	gitRun(t, seedDir, "remote", "add", "origin", remoteDir)
	gitRun(t, seedDir, "push", "origin", "main")

	v, err := NewGitVault("file://" + remoteDir)
	if err != nil {
		t.Fatalf("NewGitVault: %v", err)
	}
	if _, _, _, err := v.GetLockFile(context.Background(), ""); err != nil {
		t.Fatalf("GetLockFile (forces clone): %v", err)
	}
	gitRun(t, v.repoPath, "config", "user.email", "alice@example.com")
	gitRun(t, v.repoPath, "config", "user.name", "Alice")
	// The clone-forcing GetLockFile above resolved (and cached) an actor
	// before alice's identity was pinned; drop it so writes see alice.
	mgmt.ResetActorCache()
	return v, remoteDir
}

// TestGitVaultReadsDoNotMigrate: a v2 build must read a v1 git vault through
// the fallback indefinitely — reads never rewrite the vault.
func TestGitVaultReadsDoNotMigrate(t *testing.T) {
	v, remoteDir := seedV1GitVault(t)
	ctx := context.Background()

	versions, err := v.GetVersionList(ctx, "chat")
	if err != nil {
		t.Fatalf("GetVersionList: %v", err)
	}
	if strings.Join(versions, ",") != "1.0,2.0" {
		t.Errorf("versions = %v", versions)
	}
	meta, err := v.GetMetadata(ctx, "chat", "1.0")
	if err != nil {
		t.Fatalf("GetMetadata: %v", err)
	}
	if meta.Asset.Name != "chat" {
		t.Errorf("metadata name = %q", meta.Asset.Name)
	}
	list, err := v.ListAssets(ctx, ListAssetsOptions{})
	if err != nil {
		t.Fatalf("ListAssets: %v", err)
	}
	if len(list.Assets) != 1 || list.Assets[0].LatestVersion != "2.0" {
		t.Errorf("ListAssets = %+v", list.Assets)
	}

	// The remote must be untouched: still v1, no migration commit.
	log := gitOut(t, "", "--git-dir", remoteDir, "log", "--format=%s", "main")
	if strings.Contains(log, "Migrate vault storage") {
		t.Errorf("reads must not migrate; remote log:\n%s", log)
	}
}

// TestGitVaultMigratesOnFirstWrite: the first direct write to a v1 git vault
// produces a standalone migration commit followed by the write's own commit,
// both pushed; the remote ends up in v2 shape with rewritten source paths and
// a vault.migrated audit event.
func TestGitVaultMigratesOnFirstWrite(t *testing.T) {
	v, remoteDir := seedV1GitVault(t)
	ctx := context.Background()

	newAsset := &lockfile.Asset{Name: "linter", Version: "1.0", Type: asset.TypeSkill}
	zipData := storageZip(t, map[string]string{
		"SKILL.md":      "# linter",
		"metadata.toml": "[asset]\nname = \"linter\"\nversion = \"1.0\"\ntype = \"skill\"\n",
	})
	if err := v.AddAsset(ctx, newAsset, zipData); err != nil {
		t.Fatalf("AddAsset: %v", err)
	}

	// Verify against a FRESH clone of the remote — proving both commits
	// were pushed, not just applied locally.
	verify := filepath.Join(t.TempDir(), "verify")
	gitRun(t, "", "clone", remoteDir, verify)

	m, _, err := manifest.Load(verify)
	if err != nil {
		t.Fatalf("load manifest from fresh clone: %v", err)
	}
	if m.SchemaVersion != 2 {
		t.Fatalf("schema_version = %d, want 2", m.SchemaVersion)
	}
	chat := m.FindAsset("chat")
	if chat == nil || chat.SourcePath == nil || chat.SourcePath.Path != ".sx/versions/chat/2.0" {
		t.Errorf("chat source path = %+v", chat)
	}

	for _, rel := range []string{
		".sx/versions/chat/1.0/SKILL.md",
		".sx/versions/chat/2.0/SKILL.md",
		".sx/versions/chat/list.txt",
		".sx/versions/linter/1.0/SKILL.md",
		"assets/chat/SKILL.md",
		"assets/linter/SKILL.md",
	} {
		if _, err := os.Stat(filepath.Join(verify, rel)); err != nil {
			t.Errorf("missing on remote after write: %s", rel)
		}
	}
	if _, err := os.Stat(filepath.Join(verify, "assets", "chat", "1.0")); !os.IsNotExist(err) {
		t.Error("v1 version dir must not remain under assets/chat")
	}

	// Migration is its own commit, before the write's commit.
	log := gitOut(t, verify, "log", "--format=%s")
	lines := strings.Split(strings.TrimSpace(log), "\n")
	if len(lines) < 3 ||
		!strings.Contains(lines[0], "Add linter 1.0") ||
		!strings.Contains(lines[1], "Migrate vault storage to format v2") {
		t.Errorf("commit log = %v, want write commit atop migration commit", lines)
	}

	// git log --follow traces a file's history through the move.
	followLog := gitOut(t, verify, "log", "--follow", "--format=%s", "--", ".sx/versions/chat/1.0/SKILL.md")
	if !strings.Contains(followLog, "seed v1 vault") {
		t.Errorf("history not preserved through migration; --follow log:\n%s", followLog)
	}

	// Audit trail records the migration.
	events, err := mgmt.QueryAuditEvents(verify, mgmt.AuditFilter{EventPrefix: mgmt.EventVaultMigrated})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Actor != "alice@example.com" {
		t.Errorf("vault.migrated events = %+v", events)
	}

	// The resolved lock file now points installs at the archive.
	lockBytes, _, _, err := v.GetLockFile(ctx, "")
	if err != nil {
		t.Fatalf("GetLockFile: %v", err)
	}
	if !strings.Contains(string(lockBytes), ".sx/versions/chat/2.0") {
		t.Errorf("lock file does not reference archive paths:\n%s", lockBytes)
	}
}

// TestGitVaultMigrationRace: when another client migrates and pushes first,
// the loser discards its local migration, adopts the remote's, and completes
// its write against the migrated vault.
func TestGitVaultMigrationRace(t *testing.T) {
	v, remoteDir := seedV1GitVault(t)
	ctx := context.Background()

	// Simulate the winner: migrate the remote out-of-band AFTER our vault
	// has already cloned the v1 state (cloneOrUpdate memoizes per process,
	// so our client won't see this until it tries to push).
	winner := filepath.Join(t.TempDir(), "winner")
	gitRun(t, "", "clone", remoteDir, winner)
	gitRun(t, winner, "config", "user.email", "bob@example.com")
	gitRun(t, winner, "config", "user.name", "Bob")
	if _, err := migrateStorageToV2(winner, "bob@example.com"); err != nil {
		t.Fatalf("winner migrate: %v", err)
	}
	gitRun(t, winner, "add", ".")
	gitRun(t, winner, "commit", "-m", "Migrate vault storage to format v2")
	gitRun(t, winner, "push", "origin", "main")

	// The loser's write triggers its own migration attempt, loses the
	// push race, recovers by adopting the remote migration, and the write
	// itself still succeeds.
	newAsset := &lockfile.Asset{Name: "linter", Version: "1.0", Type: asset.TypeSkill}
	zipData := storageZip(t, map[string]string{
		"SKILL.md":      "# linter",
		"metadata.toml": "[asset]\nname = \"linter\"\nversion = \"1.0\"\ntype = \"skill\"\n",
	})
	if err := v.AddAsset(ctx, newAsset, zipData); err != nil {
		t.Fatalf("AddAsset after race: %v", err)
	}

	verify := filepath.Join(t.TempDir(), "verify")
	gitRun(t, "", "clone", remoteDir, verify)
	m, _, err := manifest.Load(verify)
	if err != nil {
		t.Fatal(err)
	}
	if m.SchemaVersion != 2 {
		t.Errorf("schema_version = %d, want 2", m.SchemaVersion)
	}
	if _, err := os.Stat(filepath.Join(verify, ".sx", "versions", "linter", "1.0", "SKILL.md")); err != nil {
		t.Errorf("loser's write missing after race recovery: %v", err)
	}
	// Exactly one migration commit on the remote.
	log := gitOut(t, verify, "log", "--format=%s")
	if strings.Count(log, "Migrate vault storage to format v2") != 1 {
		t.Errorf("want exactly one migration commit, log:\n%s", log)
	}
}

// TestGitVaultExplicitMigrate covers the `sx vault migrate` entry points.
func TestGitVaultExplicitMigrate(t *testing.T) {
	v, remoteDir := seedV1GitVault(t)
	ctx := context.Background()

	plan, err := v.PlanStorageMigration(ctx)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if plan.FromVersion != 1 || strings.Join(plan.Assets, ",") != "chat" {
		t.Errorf("plan = %+v", plan)
	}

	result, err := v.MigrateStorage(ctx)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if result.Assets != 1 {
		t.Errorf("result = %+v", result)
	}

	if _, err := v.PlanStorageMigration(ctx); !errors.Is(err, ErrStorageUpToDate) {
		t.Errorf("plan after migrate err = %v, want ErrStorageUpToDate", err)
	}

	verify := filepath.Join(t.TempDir(), "verify")
	gitRun(t, "", "clone", remoteDir, verify)
	if _, err := os.Stat(filepath.Join(verify, ".sx", "versions", "chat", "2.0", "SKILL.md")); err != nil {
		t.Errorf("migration not pushed: %v", err)
	}
}
