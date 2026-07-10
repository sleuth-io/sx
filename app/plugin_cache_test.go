package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	vaultpkg "github.com/sleuth-io/sx/internal/vault"
)

func TestExtensionCacheRoundTrip(t *testing.T) {
	a := pluginTestApp(t)

	// Empty cache reads as an empty list, never an error.
	if got := a.CachedVaultPlugins(); len(got) != 0 {
		t.Fatalf("empty cache = %+v", got)
	}

	one := VaultPlugin{
		AssetName: "hello-team",
		Version:   "1.0",
		Manifest:  `{"id":"hello-team"}`,
		Source:    "export default class {}",
		Scope:     ExtensionScope{Shared: true, Label: "Everyone"},
	}
	two := VaultPlugin{
		AssetName: "other",
		Version:   "3.0",
		Manifest:  `{"id":"other"}`,
		Source:    "export default class {}",
	}
	a.writeExtensionCache([]VaultPlugin{two, one})

	got := a.CachedVaultPlugins()
	if len(got) != 2 || got[0].AssetName != "hello-team" || got[1].AssetName != "other" {
		t.Fatalf("cache = %+v; want hello-team, other (sorted)", got)
	}
	if got[0].Version != "1.0" || !got[0].Scope.Shared || got[0].Scope.Label != "Everyone" {
		t.Fatalf("cached entry lost fields: %+v", got[0])
	}

	// A later listing without an extension prunes its cache file.
	a.writeExtensionCache([]VaultPlugin{one})
	if got := a.CachedVaultPlugins(); len(got) != 1 || got[0].AssetName != "hello-team" {
		t.Fatalf("after prune = %+v; want hello-team only", got)
	}

	// A path-shaped asset name must not become a path.
	a.writeExtensionCache([]VaultPlugin{
		{AssetName: "../evil", Version: "1", Manifest: "{}", Source: "x"},
	})
	if got := a.CachedVaultPlugins(); len(got) != 0 {
		t.Fatalf("path-shaped name cached: %+v", got)
	}
}

func TestCachedVaultPluginsSkipsBadEntries(t *testing.T) {
	a := pluginTestApp(t)
	dir, err := a.extensionCacheDir()
	if err != nil {
		t.Fatalf("cache dir: %v", err)
	}

	// Corrupt JSON, an incomplete entry, and an oversized source are all
	// skipped — the fast path degrades, it never breaks boot.
	if err := os.WriteFile(filepath.Join(dir, "corrupt.json"), []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "empty.json"), []byte(`{"assetName":"empty"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	huge := VaultPlugin{
		AssetName: "huge",
		Version:   "1",
		Manifest:  "{}",
		Source:    strings.Repeat("x", maxPluginSourceBytes+1),
	}
	a.writeExtensionCache([]VaultPlugin{huge})
	if got := a.CachedVaultPlugins(); len(got) != 0 {
		t.Fatalf("bad entries served: %d", len(got))
	}
}

func TestCachedPluginPolicy(t *testing.T) {
	a := pluginTestApp(t)

	// No cache: open, matching GetPluginPolicy's fallback.
	p := a.CachedPluginPolicy()
	if p.Mode != vaultpkg.AppPluginModeOpen || len(p.Allowed) != 0 {
		t.Fatalf("no-cache policy = %+v; want open", p)
	}

	a.cachePluginPolicy(PluginPolicy{Mode: "allowlist", Allowed: []string{"hello-team"}})
	p = a.CachedPluginPolicy()
	if p.Mode != "allowlist" || len(p.Allowed) != 1 || p.Allowed[0] != "hello-team" {
		t.Fatalf("cached policy = %+v", p)
	}
}

// A successful ListVaultPlugins IS the cache: the next boot serves the
// same extensions (revision included) without touching the vault.
func TestListVaultPluginsWritesCache(t *testing.T) {
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

	src := t.TempDir()
	if err := os.WriteFile(src+"/plugin.json", []byte(`{"id":"hello-team","name":"Hello Team","version":"1.0.0","description":"Demo.","permissions":["commands"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src+"/main.js", []byte("export default class { onload(sx) {} }"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.addExtensionFrom(src); err != nil {
		t.Fatalf("addExtensionFrom: %v", err)
	}

	listed, err := a.ListVaultPlugins()
	if err != nil || len(listed) != 1 {
		t.Fatalf("ListVaultPlugins = %+v err=%v", listed, err)
	}
	if listed[0].Version == "" {
		t.Fatalf("listing carries no revision: %+v", listed[0])
	}

	cached := a.CachedVaultPlugins()
	if len(cached) != 1 || cached[0].AssetName != "hello-team" {
		t.Fatalf("cache after listing = %+v", cached)
	}
	if cached[0].Version != listed[0].Version || cached[0].Source != listed[0].Source {
		t.Fatalf("cache diverges from listing: %+v vs %+v", cached[0], listed[0])
	}
}
