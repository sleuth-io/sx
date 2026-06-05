package commands

import (
	"context"
	"fmt"
	"strings"

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
	if err := updateLockFile(ctx, out, repo, asset, result); err != nil {
		return fmt.Errorf("failed to update lock file: %w", err)
	}
	return nil
}

// meScopeAlias is the magic user value that resolves to the caller's own
// account, so users can type "me" instead of their email.
const meScopeAlias = "me"

// resolveSelfUserScopes replaces the magic "me" user value with the caller's
// resolved email (looked up once via CurrentActor) and drops duplicate user
// targets, so "me" plus your own email don't produce two identical installs.
// The second return value is the resolved self-email when any "me" was present
// (empty otherwise), so callers can confirm who it landed on.
func resolveSelfUserScopes(ctx context.Context, repo vault.Vault, targets []vault.InstallTarget) ([]vault.InstallTarget, string, error) {
	var meEmail string
	seenUser := make(map[string]bool)
	resolved := make([]vault.InstallTarget, 0, len(targets))
	for _, t := range targets {
		if t.Kind == vault.InstallKindUser {
			if strings.EqualFold(strings.TrimSpace(t.User), meScopeAlias) {
				if meEmail == "" {
					actor, err := repo.CurrentActor(ctx)
					if err != nil {
						return nil, "", fmt.Errorf("could not resolve 'me' to your account: %w", err)
					}
					if actor.Email == "" {
						return nil, "", fmt.Errorf("could not resolve 'me': your account has no email")
					}
					meEmail = actor.Email
				}
				t.User = meEmail
			}
			key := strings.ToLower(strings.TrimSpace(t.User))
			if seenUser[key] {
				continue
			}
			seenUser[key] = true
		}
		resolved = append(resolved, t)
	}
	return resolved, meEmail, nil
}

// installSetter is implemented by vaults that can persist a full set of
// kind-aware install targets — team/user/bot in addition to repo/path — in one
// atomic call. appendMode merges with the asset's existing installations
// server-side instead of replacing them. The Sleuth/skills.io vault implements
// it; file-backed vaults don't, so they stay repo/path-only.
type installSetter interface {
	SetAssetInstallations(ctx context.Context, assetName string, targets []vault.InstallTarget, appendMode bool) (unresolved []vault.InstallTarget, err error)
}

// updateLockFile persists an asset's chosen scopes. Identity scopes
// (team/user/bot) — and any edit the user chose to append — are routed through
// the vault's bulk installer, which merges or replaces server-side; repo/path
// replaces keep the lock-file path (which also pins the asset version).
func updateLockFile(ctx context.Context, out *outputHelper, repo vault.Vault, asset *lockfile.Asset, result *scopeResult) error {
	if result.Append || hasIdentityScope(result.Targets) {
		return bulkSetInstallTargets(ctx, out, repo, asset.Name, result.Targets, result.Append)
	}

	// SetInstallations updates the vault's lock file with the installation configuration
	// The user was already shown their choice in the prompt, so we don't need to show it again
	if err := repo.SetInstallations(ctx, asset, result.ScopeEntity); err != nil {
		return fmt.Errorf("failed to set installations: %w", err)
	}

	return nil
}

// bulkSetInstallTargets resolves the "me" alias and persists targets via the
// vault's bulk installer. appendMode merges with the asset's existing
// installations server-side; otherwise the set is replaced.
func bulkSetInstallTargets(ctx context.Context, out *outputHelper, repo vault.Vault, assetName string, targets []vault.InstallTarget, appendMode bool) error {
	setter, ok := repo.(installSetter)
	if !ok {
		return fmt.Errorf("this vault does not support team/user/bot scopes")
	}
	resolved, selfEmail, err := resolveSelfUserScopes(ctx, repo, targets)
	if err != nil {
		return err
	}
	unresolved, err := setter.SetAssetInstallations(ctx, assetName, resolved, appendMode)
	if err != nil {
		return fmt.Errorf("failed to set installations: %w", err)
	}
	if selfEmail != "" {
		out.printf("Assigned to you (%s)\n", selfEmail)
	}
	for _, t := range unresolved {
		out.printf("⚠ Could not resolve %s — skipped\n", formatTarget(t))
	}
	return nil
}

// inheritLockFile preserves existing installation scopes for the asset.
// Used when --yes is provided without scope flags, so existing installations
// are not overwritten.
func inheritLockFile(ctx context.Context, out *outputHelper, repo vault.Vault, asset *lockfile.Asset) error {
	return repo.InheritInstallations(ctx, asset)
}
