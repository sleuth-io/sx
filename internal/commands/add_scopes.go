package commands

import (
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
