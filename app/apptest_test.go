package main

import (
	"context"
	"testing"

	vaultpkg "github.com/sleuth-io/sx/internal/vault"
)

// newTestApp is the one way tests construct an App. It registers the
// event-writer drain with t.Cleanup: cleanups run LIFO, so the Wait always
// runs before any earlier t.TempDir is removed, and a still-running
// fire-and-forget audit/usage writer can never race a temp vault's
// cleanup (the flake that failed the v2.2.6 release). A bare &App{} in a
// test would quietly reintroduce that class.
func newTestApp(t *testing.T) *App {
	t.Helper()
	a := &App{}
	t.Cleanup(a.eventWrites.Wait)
	return a
}

// newTestAppWithVault is newTestApp for the common vault-backed shape.
func newTestAppWithVault(t *testing.T, v vaultpkg.Vault) *App {
	t.Helper()
	a := newTestApp(t)
	a.ctx = context.Background()
	a.vault = v
	return a
}
