package commands

import (
	"context"
	"os"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/assets"
	"github.com/sleuth-io/sx/internal/clients"
	"github.com/sleuth-io/sx/internal/lockfile"
	vaultpkg "github.com/sleuth-io/sx/internal/vault"
)

// existingAssetScopes returns the current repo/path scopes for the named
// asset in vault's manifest, or nil when the vault type does not expose a
// scope-lookup hook (e.g. the sleuth vault, where this view is supplied by
// the server rather than the client).
func existingAssetScopes(vault vaultpkg.Vault, name string) []lockfile.Scope {
	type scopeLookup interface {
		ExistingAssetScopes(name string) []lockfile.Scope
	}
	if v, ok := vault.(scopeLookup); ok {
		return v.ExistingAssetScopes(name)
	}
	return nil
}

// resolveCurrentScopes returns the scopes that describe where the named asset
// is currently installed, or nil if it isn't installed anywhere. It prefers
// the vault's authoritative view (existingAssetScopes), and falls back to the
// local install tracker when the vault can't answer — e.g. the sleuth vault,
// where user-scope installs live server-side and aren't surfaced to the
// client. A returned empty slice means "installed globally" (no repo/path
// scope). Callers downstream (promptForRepositories / displayCurrentInstallation)
// already treat nil vs. empty distinctly, so the tristate matters.
func resolveCurrentScopes(vault vaultpkg.Vault, name string) []lockfile.Scope {
	if s := existingAssetScopes(vault, name); s != nil {
		return s
	}
	tracker, err := assets.LoadTracker()
	if err != nil || tracker == nil {
		return nil
	}
	for _, a := range tracker.Assets {
		if a.Name == name {
			return []lockfile.Scope{}
		}
	}
	return nil
}

// resolveInstalledAssetPath returns the on-disk path of the named asset if it
// is currently installed on this machine, or "" if no installed copy can be
// found. It consults the install tracker for the asset's type and registered
// clients, then asks each client where it would place that asset under global
// scope. The first existing path wins.
//
// This is used so that `sx add <name>` can detect local edits made directly
// in the installed directory and treat them as a source-import — making the
// name and path forms of `sx add` behave equivalently when the user has an
// installed copy.
func resolveInstalledAssetPath(ctx context.Context, name string) string {
	tracker, err := assets.LoadTracker()
	if err != nil || tracker == nil {
		return ""
	}
	var entry *assets.InstalledAsset
	for i := range tracker.Assets {
		if tracker.Assets[i].Name == name {
			entry = &tracker.Assets[i]
			break
		}
	}
	if entry == nil || entry.Type == "" {
		return ""
	}
	assetType := asset.FromString(entry.Type)
	if !assetType.IsValid() {
		return ""
	}
	registry := clients.Global()
	globalScope := &clients.InstallScope{Type: clients.ScopeGlobal}
	for _, clientID := range entry.Clients {
		client, err := registry.Get(clientID)
		if err != nil {
			continue
		}
		path, err := client.GetAssetPath(ctx, name, assetType, globalScope)
		if err != nil || path == "" {
			continue
		}
		if info, statErr := os.Stat(path); statErr == nil && info.IsDir() {
			return path
		}
	}
	return ""
}
