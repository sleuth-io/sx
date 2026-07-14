package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	vaultpkg "github.com/sleuth-io/sx/internal/vault"
)

// seedConfigWithIdentity is seedConfig plus an identity email, which
// personal installs and reach filtering key on.
func seedConfigWithIdentity(t *testing.T, dir, email string) {
	t.Helper()
	cfg := `{"type":"path","repositoryUrl":"file:///tmp/x","identity":"` + email + `",` +
		`"defaultProfile":"default","activeProfiles":["default"],` +
		`"profiles":{"default":{"type":"path","repositoryUrl":"file:///tmp/x","identity":"` + email + `"}}}`
	if err := writeFile(dir+"/config.json", cfg); err != nil {
		t.Fatalf("seed config: %v", err)
	}
}

// scopedExtensionApp builds an app whose config identity AND vault git
// identity are the given email, with an empty path vault to install into.
// Returns the app, the config dir (for identity swaps), and the vault dir.
func scopedExtensionApp(t *testing.T, email string) (*App, string, string) {
	t.Helper()
	cfgDir := t.TempDir()
	t.Setenv("SX_CONFIG_DIR", cfgDir)
	seedConfigWithIdentity(t, cfgDir, email)

	vdir := t.TempDir()
	runGitCmd(t, vdir, "init")
	runGitCmd(t, vdir, "config", "user.email", email)
	runGitCmd(t, vdir, "config", "user.name", strings.Split(email, "@")[0])
	v, err := vaultpkg.NewPathVault("file://" + vdir)
	if err != nil {
		t.Fatalf("NewPathVault: %v", err)
	}
	a := newTestAppWithVault(t, v)
	return a, cfgDir, vdir
}

// marketplaceWith publishes one extension fixture into a fresh marketplace
// vault and points the app's marketplace URL at it.
func marketplaceWith(t *testing.T, a *App, id string) {
	t.Helper()
	mktDir := newExtensionVault(t, a, id, strings.ToUpper(id), "Fixture extension.")
	if err := a.SetMarketplaceURL(mktDir); err != nil {
		t.Fatalf("point at marketplace: %v", err)
	}
}

func installTargets(t *testing.T, a *App, id string) ([]vaultpkg.InstallTarget, bool) {
	t.Helper()
	r, err := a.sharingVault()
	if err != nil {
		t.Fatalf("sharingVault: %v", err)
	}
	targets, present, err := r.CurrentInstallTargets(a.ctx, id)
	if err != nil {
		t.Fatalf("CurrentInstallTargets: %v", err)
	}
	return targets, present
}

// waitForEvent polls the vault's audit/usage streams for a marker written
// by the fire-and-forget event goroutines.
func waitForEvent(t *testing.T, vaultRoot, stream, marker string) {
	t.Helper()
	dir := filepath.Join(vaultRoot, ".sx", stream)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			data, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err == nil && strings.Contains(string(data), marker) {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no %s event containing %q", stream, marker)
}

func TestPersonalInstallScopesToCaller(t *testing.T) {
	a, cfgDir, vdir := scopedExtensionApp(t, "alice@example.com")
	marketplaceWith(t, a, "solo-tool")

	name, err := a.InstallMarketplaceExtension("solo-tool", ExtensionScopeMe)
	if err != nil {
		t.Fatalf("personal install: %v", err)
	}
	if name != "solo-tool" {
		t.Fatalf("installed name = %q", name)
	}

	targets, present := installTargets(t, a, "solo-tool")
	if !present || len(targets) != 1 ||
		targets[0].Kind != vaultpkg.InstallKindUser ||
		targets[0].User != "alice@example.com" {
		t.Fatalf("targets = %+v (present=%v), want one alice user scope", targets, present)
	}

	plugins, err := a.ListVaultPlugins()
	if err != nil || len(plugins) != 1 {
		t.Fatalf("alice's plugins = %+v, %v", plugins, err)
	}
	scope := plugins[0].Scope
	if !scope.Personal || scope.Shared || scope.Label != "Just you" {
		t.Fatalf("scope = %+v, want personal-only 'Just you'", scope)
	}

	// The install and its scope land in the audit and usage streams.
	waitForEvent(t, vdir, "audit", `"plugin.installed"`)
	waitForEvent(t, vdir, "audit", `"scope":"personal"`)
	waitForEvent(t, vdir, "usage", `"solo-tool"`)

	// A different user doesn't receive a personal install.
	seedConfigWithIdentity(t, cfgDir, "bob@example.com")
	plugins, err = a.ListVaultPlugins()
	if err != nil || len(plugins) != 0 {
		t.Fatalf("bob's plugins = %+v, %v — alice's personal install leaked", plugins, err)
	}
}

func TestOrgInstallIsLibraryWide(t *testing.T) {
	a, cfgDir, _ := scopedExtensionApp(t, "alice@example.com")
	marketplaceWith(t, a, "team-tool")

	if _, err := a.InstallMarketplaceExtension("team-tool", ExtensionScopeOrg); err != nil {
		t.Fatalf("org install: %v", err)
	}
	targets, present := installTargets(t, a, "team-tool")
	if !present || len(targets) != 0 {
		t.Fatalf("targets = %+v, want none (library-wide)", targets)
	}
	// Everyone sees it — including a different identity.
	seedConfigWithIdentity(t, cfgDir, "bob@example.com")
	plugins, err := a.ListVaultPlugins()
	if err != nil || len(plugins) != 1 {
		t.Fatalf("bob's plugins = %+v, %v", plugins, err)
	}
	if s := plugins[0].Scope; !s.Shared || s.Personal || s.Label != "Everyone" {
		t.Fatalf("scope = %+v, want shared 'Everyone'", s)
	}
}

func TestUnknownScopeRejected(t *testing.T) {
	a, _, _ := scopedExtensionApp(t, "alice@example.com")
	marketplaceWith(t, a, "any-tool")
	if _, err := a.InstallMarketplaceExtension("any-tool", "team"); err == nil ||
		!strings.Contains(err.Error(), "unknown install scope") {
		t.Fatalf("err = %v, want unknown scope", err)
	}
}

// Reinstalling an extension the vault already has is an update: its
// sharing must survive regardless of the scope argument.
func TestUpdateKeepsExistingSharing(t *testing.T) {
	a, _, vdir := scopedExtensionApp(t, "alice@example.com")
	marketplaceWith(t, a, "keeper")

	if _, err := a.InstallMarketplaceExtension("keeper", ExtensionScopeMe); err != nil {
		t.Fatalf("personal install: %v", err)
	}
	if _, err := a.InstallMarketplaceExtension("keeper", ExtensionScopeOrg); err != nil {
		t.Fatalf("reinstall: %v", err)
	}
	targets, _ := installTargets(t, a, "keeper")
	if len(targets) != 1 || targets[0].Kind != vaultpkg.InstallKindUser {
		t.Fatalf("targets after update = %+v, personal scope lost", targets)
	}
	waitForEvent(t, vdir, "audit", `"plugin.updated"`)
}

func TestRemovePersonalOnlyDeletesAsset(t *testing.T) {
	a, _, vdir := scopedExtensionApp(t, "alice@example.com")
	marketplaceWith(t, a, "mine-only")

	if _, err := a.InstallMarketplaceExtension("mine-only", ExtensionScopeMe); err != nil {
		t.Fatalf("install: %v", err)
	}
	msg, err := a.RemoveExtensionAsset("mine-only")
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if !strings.Contains(msg, "only installed for you") {
		t.Fatalf("msg = %q", msg)
	}
	if _, present := installTargets(t, a, "mine-only"); present {
		t.Fatalf("asset still in manifest after personal-only remove")
	}
	plugins, _ := a.ListVaultPlugins()
	if len(plugins) != 0 {
		t.Fatalf("plugins after remove = %+v", plugins)
	}
	waitForEvent(t, vdir, "audit", `"plugin.uninstalled"`)
}

// Removing a personal install while a team also shares the extension with
// the caller drops only the caller's scope; the asset stays.
func TestRemovePersonalKeepsTeamShare(t *testing.T) {
	a, _, _ := scopedExtensionApp(t, "alice@example.com")
	marketplaceWith(t, a, "both-ways")

	if _, err := a.InstallMarketplaceExtension("both-ways", ExtensionScopeMe); err != nil {
		t.Fatalf("install: %v", err)
	}
	// Create the team through the app API (caller becomes member+admin),
	// then share the extension with it.
	if _, err := a.CreateTeam("platform"); err != nil {
		t.Fatalf("create team: %v", err)
	}
	r, err := a.sharingVault()
	if err != nil {
		t.Fatal(err)
	}
	if err := r.SetAssetInstallation(a.ctx, "both-ways", vaultpkg.InstallTarget{Kind: vaultpkg.InstallKindTeam, Team: "platform"}); err != nil {
		t.Fatalf("team share: %v", err)
	}

	msg, err := a.RemoveExtensionAsset("both-ways")
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if !strings.Contains(msg, "still shares") {
		t.Fatalf("msg = %q", msg)
	}
	targets, present := installTargets(t, a, "both-ways")
	if !present || len(targets) != 1 || targets[0].Kind != vaultpkg.InstallKindTeam {
		t.Fatalf("targets = %+v (present=%v), want just the team scope", targets, present)
	}
	// Still reaches alice through the team.
	plugins, _ := a.ListVaultPlugins()
	if len(plugins) != 1 || !plugins[0].Scope.Shared || plugins[0].Scope.Personal {
		t.Fatalf("plugins = %+v, want team-shared row", plugins)
	}
}

func TestShareExtensionWithLibrary(t *testing.T) {
	a, _, vdir := scopedExtensionApp(t, "alice@example.com")
	marketplaceWith(t, a, "promoted")

	if _, err := a.InstallMarketplaceExtension("promoted", ExtensionScopeMe); err != nil {
		t.Fatalf("install: %v", err)
	}
	if err := a.ShareExtensionWithLibrary("promoted"); err != nil {
		t.Fatalf("share: %v", err)
	}
	targets, present := installTargets(t, a, "promoted")
	if !present || len(targets) != 0 {
		t.Fatalf("targets = %+v, want none (library-wide)", targets)
	}
	waitForEvent(t, vdir, "audit", `"plugin.shared"`)
}

func TestCanInstallForEveryone(t *testing.T) {
	a, _, _ := scopedExtensionApp(t, "alice@example.com")
	// Ungoverned: anyone may install library-wide.
	if !a.CanInstallForEveryone() {
		t.Fatalf("ungoverned vault should allow org installs")
	}
	// Governed by someone else: alice may not.
	admins, ok := a.vault.(interface {
		AddOrgAdmins(ctx context.Context, emails []string) (int, error)
	})
	if !ok {
		t.Fatalf("vault has no AddOrgAdmins")
	}
	if _, err := admins.AddOrgAdmins(a.ctx, []string{"boss@example.com"}); err != nil {
		t.Fatalf("add admin: %v", err)
	}
	if a.CanInstallForEveryone() {
		t.Fatalf("governed vault must gate org installs to admins")
	}
}

func TestExtensionScopeLabel(t *testing.T) {
	user := func(email string) vaultpkg.InstallTarget {
		return vaultpkg.InstallTarget{Kind: vaultpkg.InstallKindUser, User: email}
	}
	team := func(name string) vaultpkg.InstallTarget {
		return vaultpkg.InstallTarget{Kind: vaultpkg.InstallKindTeam, Team: name}
	}
	cases := []struct {
		name    string
		targets []vaultpkg.InstallTarget
		mine    bool
		want    string
	}{
		{"empty is everyone", nil, false, "Everyone"},
		{"org row is everyone", []vaultpkg.InstallTarget{{Kind: vaultpkg.InstallKindOrg}}, false, "Everyone"},
		{"just me", []vaultpkg.InstallTarget{user("a@x.com")}, true, "Just you"},
		{"me and another", []vaultpkg.InstallTarget{user("a@x.com"), user("b@x.com")}, true, "you + 1 more"},
		{"team only", []vaultpkg.InstallTarget{team("platform")}, false, "Team platform"},
		{"team plus me", []vaultpkg.InstallTarget{team("platform"), user("a@x.com")}, true, "Team platform · you"},
		{"others only", []vaultpkg.InstallTarget{user("b@x.com"), user("c@x.com")}, false, "2 people"},
		{"repo scope alone reads everyone", []vaultpkg.InstallTarget{{Kind: vaultpkg.InstallKindRepo, Repo: "https://x"}}, false, "Everyone"},
	}
	for _, tc := range cases {
		if got := extensionScopeLabel(tc.targets, tc.mine); got != tc.want {
			t.Errorf("%s: label = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// A marketplace with a CI-generated catalog.json serves the browse list
// from that one file — no bundle unpacking — and merges stats.json install
// counts. The catalog path must also honor the query filter.
func TestMarketplaceCatalogAndStats(t *testing.T) {
	a, _, _ := scopedExtensionApp(t, "alice@example.com")

	mktDir := t.TempDir()
	catalog := `{"extensions":[
		{"assetName":"collection-doctor","id":"collection-doctor","name":"Collection Doctor","version":"1.1.0","description":"Health score.","author":"sx","permissions":["assets:read"]},
		{"assetName":"table-tidy","id":"table-tidy","name":"Table Tidy","version":"2.0.0","description":"Formats tables.","author":"sx","permissions":[]}
	]}`
	stats := `{"collection-doctor":{"installs":1234}}`
	if err := os.WriteFile(filepath.Join(mktDir, "catalog.json"), []byte(catalog), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mktDir, "stats.json"), []byte(stats), 0o644); err != nil {
		t.Fatal(err)
	}
	// Note: no assets/ directory at all — proof the list never touches
	// bundles when a catalog exists.
	if err := a.SetMarketplaceURL(mktDir); err != nil {
		t.Fatal(err)
	}

	all, err := a.SearchMarketplace("")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("entries = %+v, want 2", all)
	}
	if all[0].ID != "collection-doctor" || all[0].Installs != 1234 {
		t.Fatalf("doctor entry = %+v, want installs 1234", all[0])
	}
	if all[1].Installs != 0 {
		t.Fatalf("tidy entry = %+v, want no installs", all[1])
	}

	found, err := a.SearchMarketplace("table")
	if err != nil || len(found) != 1 || found[0].ID != "table-tidy" {
		t.Fatalf("filtered = %+v, %v", found, err)
	}
}

// A malformed catalog.json must fall back to bundle scanning, not blank
// the marketplace.
func TestMarketplaceCatalogMalformedFallsBack(t *testing.T) {
	a, _, _ := scopedExtensionApp(t, "alice@example.com")
	mktDir := newExtensionVault(t, a, "fallback-tool", "Fallback Tool", "Still browsable.")
	if err := os.WriteFile(filepath.Join(mktDir, "catalog.json"), []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.SetMarketplaceURL(mktDir); err != nil {
		t.Fatal(err)
	}
	all, err := a.SearchMarketplace("")
	if err != nil || len(all) != 1 || all[0].ID != "fallback-tool" {
		t.Fatalf("fallback entries = %+v, %v", all, err)
	}
}

// failingListVault errors on ListAssets; every other Vault method panics
// via the nil embed, which this test never reaches.
type failingListVault struct{ vaultpkg.Vault }

func (failingListVault) ListAssets(ctx context.Context, opts vaultpkg.ListAssetsOptions) (*vaultpkg.ListAssetsResult, error) {
	return nil, errors.New("backend hiccup")
}

// A listing failure must surface as an error, never as an empty result:
// the frontend prunes plugins missing from a successful listing, so
// empty-on-error would tear down every running extension on a transient
// backend problem.
func TestListVaultPluginsSurfacesListError(t *testing.T) {
	a, _, _ := scopedExtensionApp(t, "alice@example.com")
	a.vault = failingListVault{}
	if _, err := a.ListVaultPlugins(); err == nil {
		t.Fatalf("ListVaultPlugins swallowed the listing error")
	}
	// No configured vault stays a genuinely empty listing, not an error.
	a.vault = nil
	t.Setenv("SX_CONFIG_DIR", t.TempDir())
	plugins, err := a.ListVaultPlugins()
	if err != nil || len(plugins) != 0 {
		t.Fatalf("no-vault listing = %+v, %v; want empty and nil", plugins, err)
	}
}

// Publish validation must accept the 1.7.0 view permissions, or a
// team/repo-view extension can never land in a vault.
func TestPublishAcceptsTeamRepoViewPermissions(t *testing.T) {
	a, _, _ := scopedExtensionApp(t, "alice@example.com")
	src := t.TempDir()
	manifest := `{"id":"team-tools","name":"Team Tools","version":"1.0.0",` +
		`"permissions":["views:team","views:repo","assets:read"]}`
	if err := os.WriteFile(src+"/plugin.json", []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src+"/main.js", []byte("export default class { onload(sx) {} }"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.addExtensionFrom(src); err != nil {
		t.Fatalf("publish with team/repo view permissions: %v", err)
	}
}

// PluginUsageEventsSince surfaces the vault's precise `since` filter to
// extensions: a bad timestamp errors, and the cutoff actually bounds the
// result (the incremental-refresh primitive).
func TestPluginUsageEventsSince(t *testing.T) {
	a, _, vdir := scopedExtensionApp(t, "alice@example.com")

	if _, err := a.PluginUsageEventsSince("not-a-timestamp"); err == nil {
		t.Fatalf("bad timestamp should error")
	}

	// Installing emits a usage event (asset_type app-plugin).
	marketplaceWith(t, a, "widget")
	if _, err := a.InstallMarketplaceExtension("widget", ExtensionScopeMe); err != nil {
		t.Fatalf("install: %v", err)
	}
	waitForEvent(t, vdir, "usage", `"widget"`)

	past := time.Now().Add(-time.Hour).Format(time.RFC3339)
	future := time.Now().Add(time.Hour).Format(time.RFC3339)

	recent, err := a.PluginUsageEventsSince(past)
	if err != nil {
		t.Fatalf("since past: %v", err)
	}
	found := false
	for _, e := range recent {
		if e.AssetName == "widget" {
			found = true
		}
	}
	if !found {
		t.Fatalf("since-past must include the widget event: %+v", recent)
	}

	none, err := a.PluginUsageEventsSince(future)
	if err != nil {
		t.Fatalf("since future: %v", err)
	}
	for _, e := range none {
		if e.AssetName == "widget" {
			t.Fatalf("since-future must exclude the widget event")
		}
	}
}

// A far-past `since` must be clamped to the one-year cap — the
// incremental variants may only narrow the window, never force the
// unbounded history scan usageCutoff guards against.
func TestPluginUsageEventsSinceClampsFarPast(t *testing.T) {
	a, _, _ := scopedExtensionApp(t, "alice@example.com")
	// Doesn't error and doesn't attempt an epoch-wide scan; on an empty
	// vault it simply returns nothing, having clamped internally.
	if _, err := a.PluginUsageEventsSince("0001-01-01T00:00:00Z"); err != nil {
		t.Fatalf("far-past since should be clamped, not error: %v", err)
	}
	if _, err := a.PluginAuditEventsSince("0001-01-01T00:00:00Z"); err != nil {
		t.Fatalf("far-past audit since should be clamped, not error: %v", err)
	}
	// The clamp helper is the invariant: anything older than ~a year
	// snaps to the year floor; anything newer passes through.
	floor := usageCutoff(365)
	if got := clampUsageSince(floor.AddDate(0, 0, -30)); got.Before(floor) {
		t.Fatalf("far-past not clamped: got %v, floor %v", got, floor)
	}
	recent := time.Now().AddDate(0, 0, -7)
	if got := clampUsageSince(recent); !got.Equal(recent) {
		t.Fatalf("recent since should pass through unchanged: got %v", got)
	}
}
