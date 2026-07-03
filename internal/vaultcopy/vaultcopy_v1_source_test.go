package vaultcopy_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/manifest"
	"github.com/sleuth-io/sx/internal/mgmt"
	"github.com/sleuth-io/sx/internal/vault"
	"github.com/sleuth-io/sx/internal/vaultcopy"
)

// TestCopy_V1SourceToV2Destination copies a legacy v1-format vault into a
// fresh (v2-format) vault, without ever migrating the source. This is the
// alternative migration path documented in docs/v2-spec.md: reads work
// against the v1 layout, and the destination stores everything in its own
// (v2) layout.
func TestCopy_V1SourceToV2Destination(t *testing.T) {
	mgmt.ResetActorCache()
	ctx := context.Background()

	// Build the v1 source by hand: exploded version dirs + list.txt.
	srcDir := t.TempDir()
	gitInit(t, srcDir)
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(srcDir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	for _, v := range []string{"1.0.0", "1.1.0"} {
		write("assets/legacy-skill/"+v+"/SKILL.md", "# legacy "+v)
		write("assets/legacy-skill/"+v+"/metadata.toml",
			"metadata-version = \"1.0\"\n\n[asset]\nname = \"legacy-skill\"\nversion = \""+v+"\"\ntype = \"skill\"\n\n[skill]\n")
	}
	write("assets/legacy-skill/list.txt", "1.0.0\n1.1.0\n")
	if err := manifest.Save(srcDir, &manifest.Manifest{
		SchemaVersion: 1,
		Assets: []manifest.Asset{{
			Name: "legacy-skill", Version: "1.1.0", Type: asset.TypeSkill,
			SourcePath: &manifest.SourcePath{Path: "./assets/legacy-skill/1.1.0"},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	src, err := vault.NewPathVault("file://" + srcDir)
	if err != nil {
		t.Fatal(err)
	}

	dst := newEmptyVault(t)

	report, err := vaultcopy.Copy(ctx, src, dst, vaultcopy.DefaultOptions())
	if err != nil {
		t.Fatalf("Copy: %v (warnings: %v)", err, report.Warnings)
	}
	if report.Assets != 1 || report.Versions != 2 {
		t.Fatalf("report = %+v, want 1 asset / 2 versions", report)
	}

	// Source stays v1 — copying is a read.
	srcManifest, _, err := manifest.Load(srcDir)
	if err != nil {
		t.Fatal(err)
	}
	if srcManifest.SchemaVersion != 1 {
		t.Errorf("source schema_version = %d, want untouched v1", srcManifest.SchemaVersion)
	}
	if _, err := os.Stat(filepath.Join(srcDir, ".sx", "versions")); !os.IsNotExist(err) {
		t.Error("copy must not migrate the source vault")
	}

	// Destination stores in v2 shape.
	details, err := dst.GetAssetDetails(ctx, "legacy-skill")
	if err != nil {
		t.Fatalf("dst GetAssetDetails: %v", err)
	}
	if len(details.Versions) != 2 {
		t.Errorf("dst versions = %+v", details.Versions)
	}
	dstDir := filepath.Dir(dst.(*vault.PathVault).ManifestPath())
	for _, rel := range []string{
		".sx/versions/legacy-skill/1.0.0/SKILL.md",
		".sx/versions/legacy-skill/1.1.0/SKILL.md",
		"assets/legacy-skill/SKILL.md",
	} {
		if _, err := os.Stat(filepath.Join(dstDir, rel)); err != nil {
			t.Errorf("destination missing %s: %v", rel, err)
		}
	}
}
