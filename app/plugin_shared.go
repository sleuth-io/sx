package main

import (
	"errors"

	vaultpkg "github.com/sleuth-io/sx/internal/vault"
)

// Shared extension storage (API 1.5.0, docs/app-plugins-spec.md): one
// JSON document per extension, stored IN the vault
// (.sx/app-plugins/<id>.json) so the whole team reads and writes the
// same state — review rotas, shared settings. Contrast with
// PluginLoad/SaveData, which is per user, per profile, app-side.

var errSharedStorageUnsupported = errors.New(
	"this library's backend can't store shared extension data yet")

// PluginSharedLoad returns the extension's shared document ("" when
// none exists).
func (a *App) PluginSharedLoad(id string) (string, error) {
	if err := validatePluginID(id); err != nil {
		return "", err
	}
	v, err := a.currentVault()
	if err != nil {
		return "", err
	}
	store, ok := v.(vaultpkg.AppPluginSharedStore)
	if !ok {
		return "", errSharedStorageUnsupported
	}
	return store.AppPluginSharedLoad(a.ctx, id)
}

// PluginSharedSave replaces the extension's shared document (empty
// data deletes it). On a git vault this commits and pushes — writes
// should be user-action-shaped, not keystroke-shaped.
func (a *App) PluginSharedSave(id, data string) error {
	if err := validatePluginID(id); err != nil {
		return err
	}
	v, err := a.currentVault()
	if err != nil {
		return err
	}
	store, ok := v.(vaultpkg.AppPluginSharedStore)
	if !ok {
		return errSharedStorageUnsupported
	}
	return store.AppPluginSharedSave(a.ctx, id, data)
}
