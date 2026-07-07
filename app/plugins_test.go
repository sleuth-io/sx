package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sleuth-io/sx/internal/mgmt"
	vaultpkg "github.com/sleuth-io/sx/internal/vault"
)

func pluginTestApp(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SX_CONFIG_DIR", dir)
	// pluginDataDir needs a loadable config; seed a minimal path profile.
	seedConfig(t, dir)
	return &App{}
}

func seedConfig(t *testing.T, dir string) {
	t.Helper()
	cfg := `{"type":"path","repositoryUrl":"file:///tmp/x","defaultProfile":"default","activeProfiles":["default"],"profiles":{"default":{"type":"path","repositoryUrl":"file:///tmp/x"}}}`
	if err := writeFile(dir+"/config.json", cfg); err != nil {
		t.Fatalf("seed config: %v", err)
	}
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

func TestPluginStorageRoundTrip(t *testing.T) {
	a := pluginTestApp(t)

	// Nothing saved yet.
	if got, err := a.PluginLoadData("library-dashboard"); err != nil || got != "" {
		t.Fatalf("load empty = %q, %v", got, err)
	}
	if err := a.PluginSaveData("library-dashboard", `{"pins":["a"]}`); err != nil {
		t.Fatalf("save: %v", err)
	}
	if got, err := a.PluginLoadData("library-dashboard"); err != nil || got != `{"pins":["a"]}` {
		t.Fatalf("load = %q, %v", got, err)
	}

	// Ids are validated on every entry point — a path-shaped id must not
	// become a path.
	if _, err := a.PluginLoadData("../evil"); err == nil {
		t.Fatalf("path-shaped id accepted on load")
	}
	if err := a.PluginSaveData("Evil_ID", "{}"); err == nil {
		t.Fatalf("invalid id accepted on save")
	}
	if err := a.PluginSaveData("big", strings.Repeat("x", maxPluginDataBytes+1)); err == nil {
		t.Fatalf("oversized data accepted")
	}
}

func TestPluginDecisions(t *testing.T) {
	a := pluginTestApp(t)

	// No decisions yet: unknown ids fall back to their default frontend-
	// side (built-ins on), so the map starts empty rather than seeded.
	decisions, err := a.PluginDecisions()
	if err != nil || len(decisions) != 0 {
		t.Fatalf("initial decisions = %v, %v; want empty", decisions, err)
	}

	// Intent persists per id; other ids stay undecided (their defaults
	// keep applying — how a future built-in auto-enables for existing
	// users).
	if err := a.SetPluginDecision("publish-doctor", false); err != nil {
		t.Fatalf("set: %v", err)
	}
	decisions, err = a.PluginDecisions()
	if err != nil || len(decisions) != 1 || decisions["publish-doctor"] != false {
		t.Fatalf("decisions = %v, %v; want publish-doctor:false only", decisions, err)
	}
	if err := a.SetPluginDecision("../evil", true); err == nil {
		t.Fatalf("invalid id accepted")
	}
}

func TestPluginConsents(t *testing.T) {
	a := pluginTestApp(t)
	consents, err := a.PluginConsents()
	if err != nil || len(consents) != 0 {
		t.Fatalf("initial consents = %v, %v", consents, err)
	}
	if err := a.SetPluginConsent("publish-doctor", []string{"events", "assets:read"}); err != nil {
		t.Fatalf("consent: %v", err)
	}
	consents, _ = a.PluginConsents()
	got := consents["publish-doctor"]
	if len(got) != 2 || got[0] != "assets:read" {
		t.Fatalf("consents = %v, want sorted permission set", consents)
	}
}

func TestAppVersionNonEmpty(t *testing.T) {
	a := &App{}
	if a.AppVersion() == "" {
		t.Fatalf("AppVersion must never be empty (sx.app.version contract)")
	}
}

func TestUsageCutoffCaps(t *testing.T) {
	now := time.Now()
	if c := usageCutoff(0); now.Sub(c) < 29*24*time.Hour || now.Sub(c) > 31*24*time.Hour {
		t.Fatalf("default cutoff = %v, want ~30d", now.Sub(c))
	}
	if c := usageCutoff(10_000); now.Sub(c) > 366*24*time.Hour {
		t.Fatalf("cutoff uncapped: %v", now.Sub(c))
	}
}

// The Importer's scan: skill folders (dirs carrying markdown) and loose
// top-level markdown become drafts; dot-entries and markdown-less dirs
// are skipped.
func TestImportDraftsFromFolder(t *testing.T) {
	a := pluginTestApp(t)
	src := t.TempDir()
	mustWrite := func(rel, content string) {
		t.Helper()
		path := src + "/" + rel
		if err := os.MkdirAll(path[:strings.LastIndex(path, "/")], 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("code-review/SKILL.md", "---\nname: code-review\ndescription: Reviews.\n---\n\n# code-review\n")
	mustWrite("loose-prompt.md", "# A loose prompt\n")
	mustWrite("no-markdown/data.json", "{}")
	mustWrite(".hidden/SKILL.md", "# hidden\n")

	res, err := a.importDraftsFrom(src)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if len(res.Created) != 2 {
		t.Fatalf("created = %v, want 2 drafts", res.Created)
	}
	if res.Skipped != 1 {
		t.Fatalf("skipped = %d, want 1 (the markdown-less dir)", res.Skipped)
	}

	drafts, err := a.ListDrafts()
	if err != nil || len(drafts) != 2 {
		t.Fatalf("drafts = %v err=%v, want the 2 imports", drafts, err)
	}
}

// drafts.create must never clobber: same-name creates uniquify.
func TestCreateDraftFromFilesUniquifies(t *testing.T) {
	a := pluginTestApp(t)
	files := []AssetFile{{Path: "SKILL.md", Content: "---\nname: capture\ndescription: x.\n---\n\n# capture\n"}}
	first, err := a.CreateDraftFromFiles("capture", files)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := a.CreateDraftFromFiles("capture", files)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if first.ID == second.ID {
		t.Fatalf("ids collide: %s", first.ID)
	}
	drafts, _ := a.ListDrafts()
	if len(drafts) != 2 {
		t.Fatalf("drafts = %d, want 2 (no clobber)", len(drafts))
	}
}

// The "Add extension…" publish core: a plugin folder (even without
// metadata.toml) becomes a published app-plugin asset, listable by the
// vault-plugin loader.
func TestAddExtensionFromFolder(t *testing.T) {
	a := pluginTestApp(t)
	// A real path vault so publish works end-to-end.
	vdir := t.TempDir()
	runGitCmd(t, vdir, "init")
	runGitCmd(t, vdir, "config", "user.email", "alice@example.com")
	runGitCmd(t, vdir, "config", "user.name", "Alice")
	v, err := vaultpkg.NewPathVault("file://" + vdir)
	if err != nil {
		t.Fatalf("NewPathVault: %v", err)
	}
	a.vault = v
	a.ctx = context.Background()

	src := t.TempDir()
	if err := os.WriteFile(src+"/plugin.json", []byte(`{"id":"hello-team","name":"Hello Team","version":"1.0.0","description":"Demo.","permissions":["commands"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src+"/main.js", []byte("export default class { onload(sx) {} }"), 0o644); err != nil {
		t.Fatal(err)
	}

	name, err := a.addExtensionFrom(src)
	if err != nil {
		t.Fatalf("addExtensionFrom: %v", err)
	}
	if name != "hello-team" {
		t.Fatalf("name = %q", name)
	}
	plugins, err := a.ListVaultPlugins()
	if err != nil || len(plugins) != 1 || plugins[0].AssetName != "hello-team" {
		t.Fatalf("ListVaultPlugins = %+v err=%v", plugins, err)
	}
	if !strings.Contains(plugins[0].Source, "onload") {
		t.Fatalf("bundle source missing")
	}

	// Rejects folders that aren't extensions.
	empty := t.TempDir()
	if _, err := a.addExtensionFrom(empty); err == nil {
		t.Fatalf("folder without plugin.json accepted")
	}
}

// PluginUserStats: known users union team rosters with usage actors
// (normalized, so case differences can't double-count), and activity
// aggregates per actor.
func TestPluginUserStats(t *testing.T) {
	a := pluginTestApp(t)
	vdir := t.TempDir()
	runGitCmd(t, vdir, "init")
	runGitCmd(t, vdir, "config", "user.email", "alice@example.com")
	runGitCmd(t, vdir, "config", "user.name", "Alice")
	v, err := vaultpkg.NewPathVault("file://" + vdir)
	if err != nil {
		t.Fatalf("NewPathVault: %v", err)
	}
	a.vault = v
	a.ctx = context.Background()

	if err := v.CreateTeam(a.ctx, mgmt.Team{
		Name:    "eng",
		Members: []string{"Alice@Example.com", "quiet@example.com"},
		Admins:  []string{"alice@example.com"},
	}); err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	now := time.Now()
	if err := v.RecordUsageEvents(a.ctx, []mgmt.UsageEvent{
		{Timestamp: now.Add(-24 * time.Hour), Actor: "ALICE@example.com", AssetName: "linear", AssetVersion: "1", AssetType: "skill"},
		{Timestamp: now.Add(-48 * time.Hour), Actor: "alice@example.com", AssetName: "code-review", AssetVersion: "1", AssetType: "skill"},
		{Timestamp: now.Add(-24 * time.Hour), Actor: "bob@example.com", AssetName: "linear", AssetVersion: "1", AssetType: "skill"},
	}); err != nil {
		t.Fatalf("RecordUsageEvents: %v", err)
	}

	stats, err := a.PluginUserStats(30)
	if err != nil {
		t.Fatalf("PluginUserStats: %v", err)
	}
	// alice (cased three ways across team, usage, and the vault identity)
	// counts once; quiet has no usage; bob comes from usage only.
	if len(stats.KnownUsers) != 3 {
		t.Fatalf("knownUsers = %v, want 3 distinct", stats.KnownUsers)
	}
	if len(stats.Active) != 2 {
		t.Fatalf("active = %+v, want alice+bob", stats.Active)
	}
	if stats.Active[0].Actor != "alice@example.com" || stats.Active[0].DistinctAssets != 2 || stats.Active[0].Events != 2 {
		t.Fatalf("top user = %+v, want alice with 2 assets / 2 events", stats.Active[0])
	}
}
