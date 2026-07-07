package main

import (
	"context"
	"errors"

	"github.com/sleuth-io/sx/internal/mgmt"
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

// PluginCurrentUser returns the identity vault mutations are attributed
// to (sx.app.currentUser, API 1.5.0) — how team-shaped extensions know
// which rota entries, assignments, or verdicts are "mine". "" when the
// vault can't resolve one.
func (a *App) PluginCurrentUser() (string, error) {
	v, err := a.currentVault()
	if err != nil {
		return "", err
	}
	resolver, ok := v.(interface {
		CurrentActor(ctx context.Context) (mgmt.Actor, error)
	})
	if !ok {
		return "", nil
	}
	actor, err := resolver.CurrentActor(a.ctx)
	if err != nil {
		return "", nil
	}
	return actor.Email, nil
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
