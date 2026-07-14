package main

import (
	"fmt"
	"sort"

	"github.com/sleuth-io/sx/internal/config"
	vaultpkg "github.com/sleuth-io/sx/internal/vault"
)

// Repository views are a per-library opt-in (Profile.TrackRepos): technical
// users see which repositories assets are scoped to; everyone else never
// pays for the concept. Bots ride the same opt-in (app/bots.go).

// RepoAssets maps repository URL → asset names scoped to it, for the
// sidebar's REPOSITORIES section. Vaults that can't report this return an
// empty map rather than an error.
func (a *App) RepoAssets() (map[string][]string, error) {
	v, err := a.currentVault()
	if err != nil {
		return nil, err
	}
	lister, ok := v.(vaultpkg.RepoAssetLister)
	if !ok {
		return map[string][]string{}, nil
	}
	out, err := lister.ListRepoAssets(a.ctx)
	if err != nil {
		return nil, friendlyVaultError(err)
	}
	if out == nil {
		out = map[string][]string{}
	}
	for repo := range out {
		sort.Strings(out[repo])
	}
	return out, nil
}

// SetLibraryRepoTracking turns repository and bot views on or off for one
// library. An empty name means the active library.
func (a *App) SetLibraryRepoTracking(name string, enabled bool) (VaultInfo, error) {
	mpc, err := config.LoadMultiProfile()
	if err != nil {
		return VaultInfo{}, err
	}
	if name == "" {
		name = config.GetActiveProfileName(mpc)
	}
	profile, ok := mpc.GetProfile(name)
	if !ok {
		return VaultInfo{}, fmt.Errorf("library %q not found", name)
	}
	profile.TrackRepos = enabled
	mpc.SetProfile(name, profile)
	if err := config.SaveMultiProfile(mpc); err != nil {
		return VaultInfo{}, err
	}
	return a.GetVaultInfo(), nil
}
