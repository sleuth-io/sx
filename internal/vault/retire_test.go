package vault

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/manifest"
)

// Retiring drops the asset from the manifest and the browsable root
// view but KEEPS the version archive — the recoverability contract
// consolidation depends on.
func TestPathVaultRetireAssetKeepsArchive(t *testing.T) {
	dir := t.TempDir()
	if err := manifest.Save(dir, &manifest.Manifest{
		SchemaVersion: manifest.CurrentSchemaVersion,
		Assets: []manifest.Asset{
			{
				Name: "dupe", Version: "1", Type: asset.TypeSkill,
				SourceHTTP: &manifest.SourceHTTP{URL: "https://example.com/dupe.zip"},
			},
		},
	}); err != nil {
		t.Fatalf("seed manifest: %v", err)
	}
	// Seed v2 storage: a root view and a version archive.
	l, err := detectLayout(dir)
	if err != nil {
		t.Fatal(err)
	}
	rootDir := filepath.Join(dir, l.AssetDir("dupe"))
	versionsDir := filepath.Join(dir, l.VersionsDir("dupe"))
	for _, d := range []string{rootDir, filepath.Join(versionsDir, "1")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(rootDir, "SKILL.md"), []byte("# dupe"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(versionsDir, "1", "SKILL.md"), []byte("# dupe"), 0o644); err != nil {
		t.Fatal(err)
	}

	v, err := NewPathVault("file://" + dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.RetireAsset(context.Background(), "dupe"); err != nil {
		t.Fatalf("retire: %v", err)
	}

	if _, err := os.Stat(rootDir); !os.IsNotExist(err) {
		t.Fatal("root view should be removed")
	}
	if _, err := os.Stat(filepath.Join(versionsDir, "1", "SKILL.md")); err != nil {
		t.Fatalf("version archive should survive: %v", err)
	}
	m, _, err := manifest.LoadOrMigrate(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range m.Assets {
		if a.Name == "dupe" {
			t.Fatal("manifest entry should be removed")
		}
	}

	if err := v.RetireAsset(context.Background(), "missing"); err == nil {
		t.Fatal("retiring an unknown asset should error")
	}
}
