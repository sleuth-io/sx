package main

import (
	"context"
	"os"
	"strings"
	"testing"

	vaultpkg "github.com/sleuth-io/sx/internal/vault"
)

// newExtensionVault initializes a git-backed path vault directory and
// publishes one extension into it via the same core the app uses.
func newExtensionVault(t *testing.T, a *App, id, name, description string) string {
	t.Helper()
	vdir := t.TempDir()
	runGitCmd(t, vdir, "init")
	runGitCmd(t, vdir, "config", "user.email", "alice@example.com")
	runGitCmd(t, vdir, "config", "user.name", "Alice")
	v, err := vaultpkg.NewPathVault("file://" + vdir)
	if err != nil {
		t.Fatalf("NewPathVault: %v", err)
	}

	src := t.TempDir()
	manifest := `{"id":"` + id + `","name":"` + name + `","version":"1.0.0","description":"` + description + `","author":"sx","permissions":["commands"]}`
	if err := os.WriteFile(src+"/plugin.json", []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src+"/main.js", []byte("export default class { onload(sx) {} }"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Publish through the app pointed temporarily at this vault.
	prev := a.vault
	a.vault = v
	if _, _, err := a.addExtensionFrom(src); err != nil {
		t.Fatalf("publish fixture extension: %v", err)
	}
	a.vault = prev
	return vdir
}

// A skills.new library can't store app-plugin assets until the server
// ships the type; every publish path must refuse with the friendly
// error, not the server's raw type-validation message.
func TestExtensionsUnsupportedOnSleuth(t *testing.T) {
	a := pluginTestApp(t)
	a.ctx = context.Background()
	if !a.VaultSupportsExtensions() {
		t.Fatalf("path vault should support extensions")
	}

	dir := t.TempDir()
	t.Setenv("SX_CONFIG_DIR", dir)
	cfg := `{"type":"sleuth","serverUrl":"https://example.test","defaultProfile":"default","activeProfiles":["default"],"profiles":{"default":{"type":"sleuth","serverUrl":"https://example.test"}}}`
	if err := writeFile(dir+"/config.json", cfg); err != nil {
		t.Fatal(err)
	}
	if a.VaultSupportsExtensions() {
		t.Fatalf("sleuth vault should not support extensions yet")
	}
	if _, _, err := a.addExtensionFrom(t.TempDir()); err == nil ||
		!strings.Contains(err.Error(), "doesn't support extensions") {
		t.Fatalf("addExtensionFrom error = %v", err)
	}
	if _, err := a.InstallMarketplaceExtension("anything", ExtensionScopeOrg); err == nil ||
		!strings.Contains(err.Error(), "doesn't support extensions") {
		t.Fatalf("install error = %v", err)
	}
}

func TestMarketplaceURLRoundTrip(t *testing.T) {
	a := pluginTestApp(t)

	if got := a.GetMarketplaceURL(); got != DefaultMarketplaceURL {
		t.Fatalf("default URL = %q", got)
	}
	if err := a.SetMarketplaceURL("/tmp/custom-marketplace"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if got := a.GetMarketplaceURL(); got != "/tmp/custom-marketplace" {
		t.Fatalf("custom URL = %q", got)
	}
	// Empty resets to the default.
	if err := a.SetMarketplaceURL("  "); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if got := a.GetMarketplaceURL(); got != DefaultMarketplaceURL {
		t.Fatalf("after reset = %q", got)
	}
}

func TestSearchAndInstallFromMarketplace(t *testing.T) {
	a := pluginTestApp(t)
	a.ctx = context.Background()

	// The marketplace: a separate vault holding two extensions.
	mktDir := newExtensionVault(t, a, "related-assets", "Related Assets", "Finds similar assets.")
	{
		// Second fixture in the same marketplace vault.
		src := t.TempDir()
		if err := os.WriteFile(src+"/plugin.json", []byte(`{"id":"library-search","name":"Library Search","version":"1.0.0","description":"Full-text search.","permissions":["assets:read","commands"]}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(src+"/main.js", []byte("export default class { onload(sx) {} }"), 0o644); err != nil {
			t.Fatal(err)
		}
		mv, err := vaultpkg.NewPathVault("file://" + mktDir)
		if err != nil {
			t.Fatal(err)
		}
		prev := a.vault
		a.vault = mv
		if _, _, err := a.addExtensionFrom(src); err != nil {
			t.Fatalf("publish second fixture: %v", err)
		}
		a.vault = prev
	}
	if err := a.SetMarketplaceURL(mktDir); err != nil {
		t.Fatalf("point at marketplace: %v", err)
	}

	// The user's own vault, initially empty.
	vdir := t.TempDir()
	runGitCmd(t, vdir, "init")
	runGitCmd(t, vdir, "config", "user.email", "bob@example.com")
	runGitCmd(t, vdir, "config", "user.name", "Bob")
	mine, err := vaultpkg.NewPathVault("file://" + vdir)
	if err != nil {
		t.Fatal(err)
	}
	a.vault = mine

	// Browse everything.
	all, err := a.SearchMarketplace("")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("marketplace entries = %d, want 2", len(all))
	}
	if all[0].Name != "Library Search" || all[1].Name != "Related Assets" {
		t.Fatalf("unexpected order/names: %+v", all)
	}
	if all[0].Installed || all[1].Installed {
		t.Fatalf("nothing should be installed yet: %+v", all)
	}
	if all[1].Version != "1.0.0" || len(all[1].Permissions) != 1 {
		t.Fatalf("manifest fields not parsed: %+v", all[1])
	}

	// Search narrows.
	found, err := a.SearchMarketplace("related")
	if err != nil || len(found) != 1 || found[0].ID != "related-assets" {
		t.Fatalf("filtered search = %+v, %v", found, err)
	}

	// Install copies into the current vault and flips the flag.
	name, err := a.InstallMarketplaceExtension("related-assets", ExtensionScopeOrg)
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if name != "related-assets" {
		t.Fatalf("installed name = %q", name)
	}
	plugins, err := a.ListVaultPlugins()
	if err != nil || len(plugins) != 1 || plugins[0].AssetName != "related-assets" {
		t.Fatalf("vault plugins after install = %+v, %v", plugins, err)
	}
	after, err := a.SearchMarketplace("")
	if err != nil {
		t.Fatalf("re-search: %v", err)
	}
	for _, e := range after {
		if e.ID == "related-assets" && !e.Installed {
			t.Fatalf("installed flag not set: %+v", e)
		}
		if e.ID == "library-search" && e.Installed {
			t.Fatalf("library-search wrongly marked installed")
		}
	}

	// Unknown asset fails cleanly.
	if _, err := a.InstallMarketplaceExtension("nope", ExtensionScopeOrg); err == nil {
		t.Fatalf("unknown asset accepted")
	}

	// A marketplace may name an asset independently of its plugin id
	// (the source URL is user-editable, so foreign conventions happen).
	// Install republishes under the plugin id, and the installed flag
	// must match on the id — not the marketplace's asset name — or the
	// entry never flips to installed.
	mkt, err := vaultpkg.NewPathVault("file://" + mktDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := mkt.RenameAsset(a.ctx, "library-search", "acme-search"); err != nil {
		t.Fatalf("rename in marketplace: %v", err)
	}
	installedName, err := a.InstallMarketplaceExtension("acme-search", ExtensionScopeOrg)
	if err != nil {
		t.Fatalf("install renamed entry: %v", err)
	}
	if installedName != "library-search" {
		t.Fatalf("republished name = %q, want the plugin id", installedName)
	}
	final, err := a.SearchMarketplace("")
	if err != nil {
		t.Fatalf("final search: %v", err)
	}
	for _, e := range final {
		if e.AssetName == "acme-search" && (!e.Installed || e.ID != "library-search") {
			t.Fatalf("renamed entry not matched by id: %+v", e)
		}
	}
}

// teams.list degrades to empty on vaults without team support instead of
// erroring — a metrics split must not take a widget down.
func TestPluginTeamsDegrades(t *testing.T) {
	a := pluginTestApp(t)
	a.ctx = context.Background()
	// No vault configured at all → empty, not error.
	teams, err := a.PluginTeams()
	if err != nil || len(teams) != 0 {
		t.Fatalf("PluginTeams = %+v, %v", teams, err)
	}
}

// writeMetadata publishes a metadata-only revision: content unchanged,
// descriptive fields updated, sharing inherited, app-plugins refused.
func TestPluginWriteMetadata(t *testing.T) {
	a := searchTestApp(t)
	desc := "Rewritten description"
	owner := "alice"
	if err := a.PluginWriteMetadata("playwright-guide", PluginMetadataPatch{
		Description: &desc, Owner: &owner, Keywords: []string{"testing"},
	}); err != nil {
		t.Fatalf("write: %v", err)
	}
	v, _ := a.currentVault()
	versions, _ := v.GetVersionList(a.ctx, "playwright-guide")
	if len(versions) != 2 {
		t.Fatalf("versions = %v, want a new revision", versions)
	}
	meta, err := v.GetMetadata(a.ctx, "playwright-guide", versions[len(versions)-1])
	if err != nil || meta.Asset.Description != desc {
		t.Fatalf("meta = %+v, %v", meta, err)
	}
	if meta.Custom["owner"] != "alice" || len(meta.Asset.Keywords) != 1 {
		t.Fatalf("custom fields not written: %+v", meta)
	}
	// No-op patch publishes nothing.
	if err := a.PluginWriteMetadata("playwright-guide", PluginMetadataPatch{Description: &desc}); err != nil {
		t.Fatal(err)
	}
	if vs, _ := v.GetVersionList(a.ctx, "playwright-guide"); len(vs) != 2 {
		t.Fatalf("no-op created revision: %v", vs)
	}
}
