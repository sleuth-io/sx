package manifest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/lockfile"
)

func TestLoadOrMigrate_NoFiles(t *testing.T) {
	dir := t.TempDir()
	m, migrated, err := LoadOrMigrate(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if migrated {
		t.Error("migrated=true when no legacy files existed")
	}
	if m == nil {
		t.Fatal("manifest is nil")
		return
	}
	if m.SchemaVersion != CurrentSchemaVersion {
		t.Errorf("schema version: got %d", m.SchemaVersion)
	}

	if _, err := os.Stat(filepath.Join(dir, FileName)); err != nil {
		t.Errorf("sx.toml not written: %v", err)
	}
}

func TestLoadOrMigrate_ExistingManifestWins(t *testing.T) {
	dir := t.TempDir()

	seed := &Manifest{
		SchemaVersion: CurrentSchemaVersion,
		CreatedBy:     "seed",
		Teams:         []Team{{Name: "seeded"}},
	}
	if err := Save(dir, seed); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Drop a legacy sx.lock too to prove it is ignored.
	seedLegacyLockFile(t, dir, []lockfile.Asset{
		{Name: "legacy", Version: "1.0.0", Type: asset.TypeSkill},
	})

	m, migrated, err := LoadOrMigrate(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if migrated {
		t.Error("migrated=true when sx.toml was already present")
	}
	if len(m.Teams) != 1 || m.Teams[0].Name != "seeded" {
		t.Errorf("unexpected teams: %+v", m.Teams)
	}
	if m.FindAsset("legacy") != nil {
		t.Error("legacy asset leaked into manifest despite existing sx.toml")
	}
}

func TestLoadOrMigrate_FromLockFile(t *testing.T) {
	dir := t.TempDir()

	seedLegacyLockFile(t, dir, []lockfile.Asset{
		{
			Name:    "global-skill",
			Version: "1.0.0",
			Type:    asset.TypeSkill,
		},
		{
			Name:    "repo-skill",
			Version: "2.0.0",
			Type:    asset.TypeSkill,
			Scopes: []lockfile.Scope{
				{Repo: "github.com/acme/infra"},
			},
		},
		{
			Name:    "path-skill",
			Version: "3.0.0",
			Type:    asset.TypeSkill,
			Scopes: []lockfile.Scope{
				{Repo: "github.com/acme/docs", Paths: []string{"README.md"}},
			},
		},
	})

	m, migrated, err := LoadOrMigrate(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !migrated {
		t.Error("migrated=false but legacy lock file existed")
	}

	if len(m.Assets) != 3 {
		t.Fatalf("assets: got %d want 3", len(m.Assets))
	}

	if scoped := m.FindAsset("repo-skill"); scoped == nil {
		t.Fatal("repo-skill missing")
	} else if len(scoped.Scopes) != 1 || scoped.Scopes[0].Kind != ScopeKindRepo {
		t.Errorf("repo-skill scope: %+v", scoped.Scopes)
	}

	if scoped := m.FindAsset("path-skill"); scoped == nil {
		t.Fatal("path-skill missing")
	} else if scoped.Scopes[0].Kind != ScopeKindPath || len(scoped.Scopes[0].Paths) != 1 {
		t.Errorf("path-skill scope: %+v", scoped.Scopes)
	}

	if _, err := os.Stat(filepath.Join(dir, "sx.lock.migrated")); err != nil {
		t.Errorf("sx.lock.migrated not created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "sx.lock")); !os.IsNotExist(err) {
		t.Errorf("sx.lock should have been renamed, stat err=%v", err)
	}
}

func TestLoadOrMigrate_Idempotent(t *testing.T) {
	dir := t.TempDir()

	seedLegacyLockFile(t, dir, []lockfile.Asset{
		{Name: "foo", Version: "1.0.0", Type: asset.TypeSkill},
	})

	if _, migrated, err := LoadOrMigrate(dir); err != nil || !migrated {
		t.Fatalf("first call: migrated=%v err=%v", migrated, err)
	}

	if _, migrated, err := LoadOrMigrate(dir); err != nil {
		t.Fatalf("second call err: %v", err)
	} else if migrated {
		t.Error("second call should not migrate — sx.toml already exists")
	}
}

func seedLegacyLockFile(t *testing.T, dir string, assets []lockfile.Asset) {
	t.Helper()
	lf := &lockfile.LockFile{
		LockVersion: "1.0",
		Version:     "1",
		CreatedBy:   "test",
		Assets:      assets,
	}
	if err := lockfile.Write(lf, filepath.Join(dir, "sx.lock")); err != nil {
		t.Fatalf("seed lock: %v", err)
	}
}
