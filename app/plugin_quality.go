package main

import (
	"errors"

	vaultpkg "github.com/sleuth-io/sx/internal/vault"
)

// Quality storage for extensions (SxAPI 1.12.0, docs/quality-spec.md):
// one capped, newest-first record list per asset, unified across vault
// types — .sx/quality/<asset>.json on file vaults, the server's own
// evaluation document on skills.new.

var errQualityUnsupported = errors.New(
	"this library's backend doesn't support quality storage yet")

func (a *App) qualityStore() (vaultpkg.QualityStore, error) {
	v, err := a.currentVault()
	if err != nil {
		return nil, err
	}
	store, ok := v.(vaultpkg.QualityStore)
	if !ok {
		return nil, errQualityUnsupported
	}
	return store, nil
}

// PluginQualityGet returns an asset's quality wrapper doc as JSON:
// {"evaluating": bool, "records": [...]} with records newest first.
func (a *App) PluginQualityGet(asset string) (string, error) {
	store, err := a.qualityStore()
	if err != nil {
		return "", err
	}
	return store.GetQuality(a.ctx, asset)
}

// PluginQualityAdd records one quality evaluation for an asset.
func (a *App) PluginQualityAdd(asset, record string) error {
	store, err := a.qualityStore()
	if err != nil {
		return err
	}
	return store.AddQuality(a.ctx, asset, record)
}

// PluginQualityLatest returns the newest record per asset as a JSON
// object ("" when nothing is evaluated yet).
func (a *App) PluginQualityLatest() (string, error) {
	store, err := a.qualityStore()
	if err != nil {
		return "", err
	}
	return store.LatestQuality(a.ctx)
}

// PluginQualityReevaluate requests a fresh evaluation and reports who
// runs it: "server" (poll PluginQualityGet until evaluating flips false)
// or "local" (the extension evaluates and stores via PluginQualityAdd).
func (a *App) PluginQualityReevaluate(asset string) (string, error) {
	store, err := a.qualityStore()
	if err != nil {
		return "", err
	}
	return store.ReevaluateQuality(a.ctx, asset)
}
