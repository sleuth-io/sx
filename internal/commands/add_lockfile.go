package commands

import (
	"context"
	"fmt"

	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/vault"
)

// writeLockFileForNoInstall writes the lock entry for an asset added with
// --no-install. Honors --scope-repo / --scope-global / --scope when set so
// batch flows ("sx add ... --no-install --scope-repo X" in a loop, then a
// single sx install at the end) get the scope they asked for rather than
// silently global. Falls back to global when no scope flag is given,
// preserving the long-standing default for plain --no-install.
func writeLockFileForNoInstall(ctx context.Context, out *outputHelper, repo vault.Vault, asset *lockfile.Asset, opts addOptions) error {
	result, err := opts.getScopes()
	if err != nil {
		return err
	}
	asset.Scopes = result.Scopes
	if asset.Scopes == nil {
		// Three getScopes branches produce nil Scopes:
		//   - Inherit / Remove (no scope flag set) — fall back to global
		//     to match pre-fix behavior; strict inherit semantics under
		//     --no-install would be a broader change.
		//   - --scope=<entity> — the entity is forwarded via
		//     result.ScopeEntity to updateLockFile / SetInstallations;
		//     an empty Scopes slice is the correct payload.
		asset.Scopes = []lockfile.Scope{}
	}
	if err := updateLockFile(ctx, out, repo, asset, result.ScopeEntity); err != nil {
		return fmt.Errorf("failed to update lock file: %w", err)
	}
	return nil
}

// updateLockFile updates the repository's lock file with the asset using modern UI
func updateLockFile(ctx context.Context, out *outputHelper, repo vault.Vault, asset *lockfile.Asset, scopeEntity string) error {
	// SetInstallations updates the vault's lock file with the installation configuration
	// The user was already shown their choice in the prompt, so we don't need to show it again
	if err := repo.SetInstallations(ctx, asset, scopeEntity); err != nil {
		return fmt.Errorf("failed to set installations: %w", err)
	}

	return nil
}

// inheritLockFile preserves existing installation scopes for the asset.
// Used when --yes is provided without scope flags, so existing installations
// are not overwritten.
func inheritLockFile(ctx context.Context, out *outputHelper, repo vault.Vault, asset *lockfile.Asset) error {
	return repo.InheritInstallations(ctx, asset)
}
