package vault

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/manifest"
	"github.com/sleuth-io/sx/internal/mgmt"
	"github.com/sleuth-io/sx/internal/vault/layout"
)

// seedV1Vault builds a v1-format vault: exploded version dirs + list.txt
// under assets/{name}, and a schema_version = 1 manifest whose source paths
// use the v1 locations (including the "./" prefix historically written by
// `sx add`).
func seedV1Vault(t *testing.T, dir string) {
	t.Helper()
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(dir, rel)
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
		write("assets/chat/"+v+"/references/notes.md", "notes "+v)
	}
	write("assets/chat/list.txt", "1.0\n2.0\n")

	write("assets/rules/1.0/RULE.md", "# rules")
	write("assets/rules/1.0/metadata.toml", "[asset]\nname = \"rules\"\nversion = \"1.0\"\ntype = \"rule\"\n")
	write("assets/rules/list.txt", "1.0\n")

	m := &manifest.Manifest{
		SchemaVersion: 1,
		Assets: []manifest.Asset{
			{
				Name: "chat", Version: "2.0", Type: asset.TypeSkill,
				SourcePath: &manifest.SourcePath{Path: "./assets/chat/2.0"},
			},
			{
				Name: "rules", Version: "1.0", Type: asset.TypeRule,
				SourcePath: &manifest.SourcePath{Path: "assets/rules/1.0"},
			},
			{
				Name: "remote-only", Version: "3.0", Type: asset.TypeSkill,
				SourceHTTP: &manifest.SourceHTTP{URL: "https://example.com/remote-only-3.0.zip"},
			},
		},
	}
	if err := manifest.Save(dir, m); err != nil {
		t.Fatal(err)
	}
}

func TestMigrateStorageToV2(t *testing.T) {
	dir := t.TempDir()
	seedV1Vault(t, dir)

	result, err := migrateStorageToV2(dir, "alice@example.com")
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if result == nil || result.Assets != 2 {
		t.Fatalf("result = %+v, want 2 assets migrated", result)
	}

	// Archive holds every version; list.txt moved with it.
	for _, rel := range []string{
		".sx/versions/chat/1.0/SKILL.md",
		".sx/versions/chat/2.0/SKILL.md",
		".sx/versions/chat/2.0/references/notes.md",
		".sx/versions/chat/list.txt",
		".sx/versions/rules/1.0/RULE.md",
		".sx/versions/rules/list.txt",
	} {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Errorf("missing after migration: %s (%v)", rel, err)
		}
	}

	// Root views hold exactly the latest version's files.
	if got := readFileString(t, filepath.Join(dir, "assets", "chat", "SKILL.md")); got != "# chat 2.0" {
		t.Errorf("chat root view = %q, want latest", got)
	}
	mustNotExist(t, filepath.Join(dir, "assets", "chat", "1.0"))
	mustNotExist(t, filepath.Join(dir, "assets", "chat", "list.txt"))
	if got := readFileString(t, filepath.Join(dir, "assets", "rules", "RULE.md")); got != "# rules" {
		t.Errorf("rules root view = %q", got)
	}

	// Manifest: schema bumped, vault-internal source paths rewritten
	// (including the "./" variant), external sources untouched.
	m, _, err := manifest.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m.SchemaVersion != 2 {
		t.Errorf("schema_version = %d, want 2", m.SchemaVersion)
	}
	for _, a := range m.Assets {
		switch a.Name {
		case "chat":
			if a.SourcePath == nil || a.SourcePath.Path != ".sx/versions/chat/2.0" {
				t.Errorf("chat source path = %+v", a.SourcePath)
			}
		case "rules":
			if a.SourcePath == nil || a.SourcePath.Path != ".sx/versions/rules/1.0" {
				t.Errorf("rules source path = %+v", a.SourcePath)
			}
		case "remote-only":
			if a.SourceHTTP == nil || a.SourceHTTP.URL != "https://example.com/remote-only-3.0.zip" {
				t.Errorf("remote-only source = %+v", a.SourceHTTP)
			}
		}
	}

	// Audit trail records the migration.
	events, err := mgmt.QueryAuditEvents(dir, mgmt.AuditFilter{EventPrefix: mgmt.EventVaultMigrated})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("audit events = %d, want 1", len(events))
	}
	ev := events[0]
	if ev.Actor != "alice@example.com" || ev.TargetType != mgmt.TargetTypeVault {
		t.Errorf("audit event = %+v", ev)
	}
	if ev.Data["assets"] != float64(2) && ev.Data["assets"] != 2 {
		t.Errorf("audit assets = %v", ev.Data["assets"])
	}

	// Second run reports up-to-date.
	if _, err := migrateStorageToV2(dir, "alice@example.com"); !errors.Is(err, ErrStorageUpToDate) {
		t.Errorf("second migration err = %v, want ErrStorageUpToDate", err)
	}
}

func TestMigrateStorageToV2ResumesInterrupted(t *testing.T) {
	dir := t.TempDir()
	seedV1Vault(t, dir)

	// Simulate an interrupted earlier run: "chat" was archived but the
	// process died before its root view was materialized and before the
	// manifest was updated.
	if err := os.MkdirAll(filepath.Join(dir, ".sx", "versions"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(
		filepath.Join(dir, "assets", "chat"),
		filepath.Join(dir, ".sx", "versions", "chat"),
	); err != nil {
		t.Fatal(err)
	}

	result, err := migrateStorageToV2(dir, "alice@example.com")
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// Only "rules" still counted as a v1 move, but chat must be repaired too.
	if result == nil || result.Assets != 1 {
		t.Fatalf("result = %+v, want 1 freshly moved asset", result)
	}
	if got := readFileString(t, filepath.Join(dir, "assets", "chat", "SKILL.md")); got != "# chat 2.0" {
		t.Errorf("chat root view after resume = %q", got)
	}
	if got := readFileString(t, filepath.Join(dir, "assets", "rules", "RULE.md")); got != "# rules" {
		t.Errorf("rules root view after resume = %q", got)
	}
	m, _, err := manifest.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m.SchemaVersion != 2 {
		t.Errorf("schema_version = %d, want 2", m.SchemaVersion)
	}
}

func TestMigrateStorageToV2SkipsUninitializedVault(t *testing.T) {
	dir := t.TempDir()
	// v1-shaped storage but no manifest: nothing to migrate — the format is
	// stamped when the manifest is first created.
	if err := os.MkdirAll(filepath.Join(dir, "assets", "chat", "1.0"), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := migrateStorageToV2(dir, "alice@example.com"); !errors.Is(err, ErrStorageUpToDate) {
		t.Errorf("err = %v, want ErrStorageUpToDate for uninitialized vault", err)
	}
	mustNotExist(t, filepath.Join(dir, ".sx", "versions"))
}

func TestPathVaultReadsAfterMigration(t *testing.T) {
	dir := t.TempDir()
	seedV1Vault(t, dir)
	if _, err := migrateStorageToV2(dir, "alice@example.com"); err != nil {
		t.Fatal(err)
	}

	v, err := NewPathVault("file://" + dir)
	if err != nil {
		t.Fatal(err)
	}
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

	zipData, err := v.GetAssetByVersion(ctx, "chat", "2.0")
	if err != nil {
		t.Fatalf("GetAssetByVersion: %v", err)
	}
	if len(zipData) == 0 {
		t.Error("empty asset zip")
	}

	list, err := v.ListAssets(ctx, ListAssetsOptions{})
	if err != nil {
		t.Fatalf("ListAssets: %v", err)
	}
	if len(list.Assets) != 2 {
		t.Errorf("ListAssets = %d assets, want 2 (%+v)", len(list.Assets), list.Assets)
	}
}

func TestMigrationPlan(t *testing.T) {
	dir := t.TempDir()
	seedV1Vault(t, dir)

	plan, err := planStorageMigration(dir)
	if err != nil {
		t.Fatal(err)
	}
	if plan == nil || plan.FromVersion != 1 || plan.ToVersion != 2 {
		t.Fatalf("plan = %+v", plan)
	}
	if strings.Join(plan.Assets, ",") != "chat,rules" {
		t.Errorf("plan assets = %v", plan.Assets)
	}

	// Plans are read-only.
	if _, err := os.Stat(filepath.Join(dir, ".sx", "versions")); !os.IsNotExist(err) {
		t.Error("plan must not modify the vault")
	}

	if _, err := migrateStorageToV2(dir, "alice@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := planStorageMigration(dir); !errors.Is(err, ErrStorageUpToDate) {
		t.Errorf("plan after migration err = %v, want ErrStorageUpToDate", err)
	}
}

func TestMigrateStorageToV2LeavesNonAssetDirsInPlace(t *testing.T) {
	dir := t.TempDir()
	seedV1Vault(t, dir)
	// A stray empty directory and a hand-made folder of loose files:
	// neither is a v1 asset (no version subdirectories).
	if err := os.MkdirAll(filepath.Join(dir, "assets", "empty-dir"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "assets", "loose-files"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "loose-files", "notes.md"), []byte("hi"), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := migrateStorageToV2(dir, "alice@example.com")
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if result == nil || result.Assets != 2 {
		t.Fatalf("result = %+v, want 2 assets migrated", result)
	}

	// The stray directories stay where they were and never enter the archive.
	for _, rel := range []string{"assets/empty-dir", "assets/loose-files/notes.md"} {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Errorf("stray dir was moved or lost: %s (%v)", rel, err)
		}
	}
	mustNotExist(t, filepath.Join(dir, ".sx", "versions", "empty-dir"))
	mustNotExist(t, filepath.Join(dir, ".sx", "versions", "loose-files"))

	// The vault is fully migrated: a rerun reports up-to-date, not a wedge.
	if _, err := migrateStorageToV2(dir, "alice@example.com"); !errors.Is(err, ErrStorageUpToDate) {
		t.Fatalf("second migrate err = %v, want ErrStorageUpToDate", err)
	}
}

// seedV1NamespacedVault builds a v1 vault whose manifest declares namespaced
// asset names (names with a "/", e.g. "opsx/apply") plus one top-level asset.
// There is no bare "opsx" asset: assets/opsx is only a namespace directory.
func seedV1NamespacedVault(t *testing.T, dir string) {
	t.Helper()
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	var assets []manifest.Asset
	for _, c := range []string{"apply", "archive", "explore", "propose"} {
		write("assets/opsx/"+c+"/1/COMMAND.md", "# opsx/"+c)
		write("assets/opsx/"+c+"/1/metadata.toml", "[asset]\nname = \"opsx/"+c+"\"\nversion = \"1\"\ntype = \"command\"\n")
		write("assets/opsx/"+c+"/list.txt", "1\n")
		assets = append(assets, manifest.Asset{
			Name: "opsx/" + c, Version: "1", Type: asset.TypeCommand,
			SourcePath: &manifest.SourcePath{Path: "./assets/opsx/" + c + "/1"},
		})
	}
	write("assets/plain/1/SKILL.md", "# plain")
	write("assets/plain/1/metadata.toml", "[asset]\nname = \"plain\"\nversion = \"1\"\ntype = \"skill\"\n")
	write("assets/plain/list.txt", "1\n")
	assets = append(assets, manifest.Asset{
		Name: "plain", Version: "1", Type: asset.TypeSkill,
		SourcePath: &manifest.SourcePath{Path: "./assets/plain/1"},
	})

	if err := manifest.Save(dir, &manifest.Manifest{SchemaVersion: 1, Assets: assets}); err != nil {
		t.Fatal(err)
	}
}

func TestMigrateStorageToV2NamespacedAssets(t *testing.T) {
	dir := t.TempDir()
	seedV1NamespacedVault(t, dir)

	plan, err := planStorageMigration(dir)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(plan.Assets, ",") != "opsx/apply,opsx/archive,opsx/explore,opsx/propose,plain" {
		t.Errorf("plan assets = %v", plan.Assets)
	}

	result, err := migrateStorageToV2(dir, "alice@example.com")
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if result == nil || result.Assets != 5 {
		t.Fatalf("result = %+v, want 5 assets migrated", result)
	}

	// Each namespaced asset is archived individually with its own list.txt,
	// and gets a root view like every top-level asset.
	for _, c := range []string{"apply", "archive", "explore", "propose"} {
		for _, rel := range []string{
			".sx/versions/opsx/" + c + "/1/COMMAND.md",
			".sx/versions/opsx/" + c + "/list.txt",
			"assets/opsx/" + c + "/COMMAND.md",
		} {
			if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
				t.Errorf("missing after migration: %s (%v)", rel, err)
			}
		}
	}

	// The namespace directory itself must not be treated as an asset: no
	// version list of asset names, no phantom root view.
	mustNotExist(t, filepath.Join(dir, ".sx", "versions", "opsx", "list.txt"))
	mustNotExist(t, filepath.Join(dir, "assets", "opsx", "list.txt"))
	mustNotExist(t, filepath.Join(dir, "assets", "opsx", "1"))

	// Manifest source paths follow the archive.
	m, _, err := manifest.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{"apply", "archive", "explore", "propose"} {
		a := m.FindAsset("opsx/" + c)
		if a == nil || a.SourcePath == nil || a.SourcePath.Path != ".sx/versions/opsx/"+c+"/1" {
			t.Errorf("opsx/%s source path = %+v", c, a)
		}
	}

	// A rerun (which re-refreshes all root views) reports up-to-date and
	// must not delete the namespaced root views.
	if _, err := migrateStorageToV2(dir, "alice@example.com"); !errors.Is(err, ErrStorageUpToDate) {
		t.Errorf("second migration err = %v, want ErrStorageUpToDate", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "assets", "opsx", "apply", "COMMAND.md")); err != nil {
		t.Errorf("namespaced root view lost on rerun: %v", err)
	}
}

func TestMigrateStorageToV2NamespacedResume(t *testing.T) {
	dir := t.TempDir()
	seedV1NamespacedVault(t, dir)

	// Simulate an interrupted earlier run: "opsx/apply" was archived but the
	// process died before its root view was materialized.
	if err := os.MkdirAll(filepath.Join(dir, ".sx", "versions", "opsx"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(
		filepath.Join(dir, "assets", "opsx", "apply"),
		filepath.Join(dir, ".sx", "versions", "opsx", "apply"),
	); err != nil {
		t.Fatal(err)
	}

	result, err := migrateStorageToV2(dir, "alice@example.com")
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// apply was already moved; the other four are fresh moves.
	if result == nil || result.Assets != 4 {
		t.Fatalf("result = %+v, want 4 freshly moved assets", result)
	}
	for _, c := range []string{"apply", "archive", "explore", "propose"} {
		if _, err := os.Stat(filepath.Join(dir, "assets", "opsx", c, "COMMAND.md")); err != nil {
			t.Errorf("opsx/%s root view missing after resume: %v", c, err)
		}
	}
	mustNotExist(t, filepath.Join(dir, ".sx", "versions", "opsx", "list.txt"))
}

func TestMigrateStorageToV2NamespaceByDiskShape(t *testing.T) {
	dir := t.TempDir()
	seedV1Vault(t, dir)

	// A namespaced asset present on disk but NOT declared in the manifest:
	// classification must fall back to shape (children holding list.txt).
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("assets/tools/fmt/1/COMMAND.md", "# tools/fmt")
	write("assets/tools/fmt/1/metadata.toml", "[asset]\nname = \"tools/fmt\"\nversion = \"1\"\ntype = \"command\"\n")
	write("assets/tools/fmt/list.txt", "1\n")

	result, err := migrateStorageToV2(dir, "alice@example.com")
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if result == nil || result.Assets != 3 {
		t.Fatalf("result = %+v, want 3 assets migrated", result)
	}
	for _, rel := range []string{
		".sx/versions/tools/fmt/1/COMMAND.md",
		".sx/versions/tools/fmt/list.txt",
		"assets/tools/fmt/COMMAND.md",
	} {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Errorf("missing after migration: %s (%v)", rel, err)
		}
	}
	mustNotExist(t, filepath.Join(dir, ".sx", "versions", "tools", "list.txt"))
}

func TestMigrateStorageToV2UndeclaredNamespaceWithoutListTxt(t *testing.T) {
	dir := t.TempDir()
	seedV1Vault(t, dir)

	// A namespaced asset neither declared in the manifest nor carrying any
	// list.txt: classification must fall back to the version-shape signal
	// (file-less children that hold subdirectories are asset-shaped, so the
	// parent is a namespace).
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("assets/bare/fmt/1/COMMAND.md", "# bare/fmt")
	write("assets/bare/lint/1/COMMAND.md", "# bare/lint")

	result, err := migrateStorageToV2(dir, "alice@example.com")
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if result == nil || result.Assets != 4 {
		t.Fatalf("result = %+v, want 4 assets migrated", result)
	}
	for _, rel := range []string{
		".sx/versions/bare/fmt/1/COMMAND.md",
		".sx/versions/bare/fmt/list.txt", // synthesized by ensureVersionList
		".sx/versions/bare/lint/1/COMMAND.md",
		"assets/bare/fmt/COMMAND.md",
		"assets/bare/lint/COMMAND.md",
	} {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Errorf("missing after migration: %s (%v)", rel, err)
		}
	}
	// "bare" itself must not have been archived as an asset with fmt/lint
	// as phantom versions.
	mustNotExist(t, filepath.Join(dir, ".sx", "versions", "bare", "list.txt"))
}

func TestMigrateStorageToV2AssetNamespaceConflictErrors(t *testing.T) {
	dir := t.TempDir()
	seedV1Vault(t, dir)

	// Declare a name that is both an asset and a namespace prefix. There is
	// no correct migration for this shape, so both the plan and the
	// migration must refuse with an actionable error.
	m, _, err := manifest.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	m.Assets = append(m.Assets,
		manifest.Asset{Name: "chat/sub", Version: "1", Type: asset.TypeCommand,
			SourcePath: &manifest.SourcePath{Path: "./assets/chat/sub/1"}},
	)
	if err := manifest.Save(dir, m); err != nil {
		t.Fatal(err)
	}

	wantErr := func(err error) {
		t.Helper()
		if err == nil || !strings.Contains(err.Error(), `"chat"`) {
			t.Fatalf("err = %v, want conflict error naming \"chat\"", err)
		}
	}
	_, err = planStorageMigration(dir)
	wantErr(err)
	_, err = migrateStorageToV2(dir, "alice@example.com")
	wantErr(err)
	// Refused means untouched: nothing may have been archived.
	mustNotExist(t, filepath.Join(dir, ".sx", "versions"))
}

func TestMigrateStorageToV2SkipsSyncConflictDirs(t *testing.T) {
	dir := t.TempDir()
	seedV1Vault(t, dir)
	// A conflicted-copy directory dropped by a sync client, shaped like a
	// v1 asset (has a version subdirectory) so only artifact filtering —
	// not the version-shape check — can exclude it.
	conflictDir := "chat (conflicted copy 2026-07-04)"
	if err := os.MkdirAll(filepath.Join(dir, "assets", conflictDir, "1.0"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", conflictDir, "1.0", "SKILL.md"), []byte("junk"), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := migrateStorageToV2(dir, "alice@example.com")
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if result == nil || result.Assets != 2 {
		t.Fatalf("result = %+v, want 2 assets migrated", result)
	}

	// The conflicted dir stays in place and never enters the archive.
	if _, err := os.Stat(filepath.Join(dir, "assets", conflictDir, "1.0", "SKILL.md")); err != nil {
		t.Errorf("conflicted dir was moved or lost: %v", err)
	}
	mustNotExist(t, filepath.Join(dir, ".sx", "versions", conflictDir))
}

func TestRefreshRootViewsSkipsNumberedConflictCopies(t *testing.T) {
	dir := t.TempDir()
	seedV1Vault(t, dir)
	if _, err := migrateStorageToV2(dir, "alice@example.com"); err != nil {
		t.Fatal(err)
	}

	// A numbered sync-conflict copy of an asset's archive ("chat (2)") left
	// by a cloud-sync client. The root-view refresh must skip it with the
	// same filtering the scan side uses — refreshing it would materialize
	// a phantom root view at assets/chat (2).
	if err := os.MkdirAll(filepath.Join(dir, ".sx", "versions", "chat (2)", "1.0"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".sx", "versions", "chat (2)", "list.txt"), []byte("1.0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".sx", "versions", "chat (2)", "1.0", "SKILL.md"), []byte("junk"), 0644); err != nil {
		t.Fatal(err)
	}

	v2, err := layout.ForVersion(layout.V2)
	if err != nil {
		t.Fatal(err)
	}
	if err := refreshAllRootViews(dir, v2); err != nil {
		t.Fatalf("refreshAllRootViews: %v", err)
	}
	mustNotExist(t, filepath.Join(dir, "assets", "chat (2)"))
	// Real assets still refresh normally.
	if _, err := os.Stat(filepath.Join(dir, "assets", "chat", "SKILL.md")); err != nil {
		t.Errorf("real root view lost: %v", err)
	}
}

func TestMigrateStorageToV2PayloadListTxtIsNotNamespaceSignal(t *testing.T) {
	dir := t.TempDir()
	seedV1Vault(t, dir)

	// An undeclared asset with no top-level list.txt whose version dir
	// bundles a payload file literally named list.txt. The lone list.txt
	// (no sibling subdirectories) must read as version-dir payload, so
	// "bundler" migrates as ONE asset — not as a namespace with "1" as a
	// phantom sub-asset.
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("assets/bundler/1/SKILL.md", "# bundler")
	write("assets/bundler/1/list.txt", "payload, not a version list")

	result, err := migrateStorageToV2(dir, "alice@example.com")
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if result == nil || result.Assets != 3 {
		t.Fatalf("result = %+v, want 3 assets migrated", result)
	}
	for _, rel := range []string{
		".sx/versions/bundler/1/SKILL.md",
		".sx/versions/bundler/1/list.txt",
		".sx/versions/bundler/list.txt",
		"assets/bundler/SKILL.md",
	} {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Errorf("missing after migration: %s (%v)", rel, err)
		}
	}
	// The version dir must not have become a phantom sub-asset with its
	// own root view.
	mustNotExist(t, filepath.Join(dir, ".sx", "versions", "bundler", "1", "1"))
	if _, err := os.Stat(filepath.Join(dir, "assets", "bundler", "1", "SKILL.md")); !os.IsNotExist(err) {
		t.Errorf("assets/bundler/1 root view should not exist (err=%v)", err)
	}
}
