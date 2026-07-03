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
