package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	vaultpkg "github.com/sleuth-io/sx/internal/vault"
)

// Extension cache: local copies of the vault's extensions (manifest +
// code + scope, keyed by asset name and vault revision) so boot serves
// extension UI before any network round trip — on a remote vault the
// listing is the chattiest call in startup and extensions otherwise
// arrive last, reflowing the UI. Extension bundles are immutable per
// revision, so "same revision → same bytes" is exact, and revalidation
// (a fresh ListVaultPlugins) rewrites the cache on every successful
// listing. Per profile, beside decisions/consents/policy-cache; the
// accepted staleness window matches policy-cache.json: a revoked
// extension can run for the seconds until revalidation prunes it.

// extensionCacheDir is <config>/app-plugins/<profile>/extensions-cache.
func (a *App) extensionCacheDir() (string, error) {
	dir, err := a.pluginDataDir()
	if err != nil {
		return "", err
	}
	cacheDir := filepath.Join(dir, "extensions-cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", err
	}
	return cacheDir, nil
}

// CachedVaultPlugins returns the last successful ListVaultPlugins result
// from the local cache — no vault I/O. Corrupt or oversized entries are
// skipped; an empty result just means the fresh listing is the first
// paint. Never errors: the fast path degrades to "no cache", it doesn't
// break boot.
func (a *App) CachedVaultPlugins() []VaultPlugin {
	out := []VaultPlugin{}
	dir, err := a.extensionCacheDir()
	if err != nil {
		return out
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var p VaultPlugin
		if err := json.Unmarshal(data, &p); err != nil {
			continue
		}
		// The same gates the live listing applies: a cache file must not
		// smuggle in what ListVaultPlugins would have rejected.
		if p.AssetName == "" || p.Manifest == "" || p.Source == "" {
			continue
		}
		if len(p.Source) > maxPluginSourceBytes {
			continue
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AssetName < out[j].AssetName })
	return out
}

// writeExtensionCache replaces the cache with a successful listing:
// upserts every entry and prunes files for extensions no longer listed.
// Best-effort — a cache write failure must never fail the listing.
func (a *App) writeExtensionCache(plugins []VaultPlugin) {
	dir, err := a.extensionCacheDir()
	if err != nil {
		return
	}
	keep := map[string]bool{}
	for _, p := range plugins {
		// The asset name becomes a filename; the same guard every asset
		// entry point uses keeps a hostile name from becoming a path.
		if err := validateAssetRef(p.AssetName, ""); err != nil {
			continue
		}
		data, err := json.Marshal(p)
		if err != nil {
			continue
		}
		file := p.AssetName + ".json"
		if atomicWriteFile(filepath.Join(dir, file), data) == nil {
			keep[file] = true
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") && !keep[e.Name()] {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// CachedPluginPolicy returns the last successfully read extension policy
// without touching the vault — the boot fast path pairs it with
// CachedVaultPlugins so enablement gates run before any network I/O.
// No cache falls back to open, matching GetPluginPolicy's fallback.
func (a *App) CachedPluginPolicy() PluginPolicy {
	open := PluginPolicy{Mode: vaultpkg.AppPluginModeOpen, Allowed: []string{}}
	return a.cachedPluginPolicy(open)
}
