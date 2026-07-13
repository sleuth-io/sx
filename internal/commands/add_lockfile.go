package commands

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/vault"
)

// writeLockFileForNoInstall writes the lock entry for an asset added with
// --no-install. Honors scope flags when set so batch flows ("sx add ...
// --no-install --repo X" in a loop, then a single sx install at the end) get
// the scope they asked for rather than silently global. Falls back to global
// when no scope flag is given, preserving the long-standing default for plain
// --no-install.
func writeLockFileForNoInstall(ctx context.Context, out *outputHelper, repo vault.Vault, asset *lockfile.Asset, opts addOptions) error {
	// Unified flags (--org/--repo/--path/--team/--user/--bot) go through the
	// same resolver the install path uses, so --no-install honors them instead
	// of dropping them and globalizing the asset. No confirmation prompt here:
	// --no-install is the non-interactive/batch path. Legacy --scope* flags and
	// the bare no-flag default stay on getScopes below.
	if opts.hasUnifiedScopeFlags() {
		change, err := resolveScopeFlags(opts.toScopeFlags())
		if err != nil {
			return err
		}
		result := &scopeResult{
			ApplyTargets: true,
			Targets:      change.Targets,
			Append:       change.Mode == scopeAdd,
		}
		if err := updateLockFile(ctx, out, repo, asset, result); err != nil {
			return fmt.Errorf("failed to update lock file: %w", err)
		}
		return nil
	}

	result, err := opts.getScopes()
	if err != nil {
		return err
	}
	// No scope flags at all (Inherit with --yes, Remove without): keep any
	// existing scopes. Republishing an already-scoped asset with a plain
	// `--no-install` must not silently re-scope it to global; a brand-new
	// asset has nothing to inherit and still lands global, preserving the
	// long-standing --no-install default.
	if result.Inherit || result.Remove {
		if err := inheritLockFile(ctx, out, repo, asset); err != nil {
			return fmt.Errorf("failed to update lock file: %w", err)
		}
		return nil
	}
	asset.Scopes = result.Scopes
	if asset.Scopes == nil {
		// --scope=<entity> produces nil Scopes: the entity is forwarded via
		// result.ScopeEntity to updateLockFile / SetInstallations; an empty
		// Scopes slice is the correct payload.
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
						return nil, "", errors.New("could not resolve 'me': your account has no email")
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
// instead of replacing them. The Sleuth/skills.new vault implements it against
// the server; file-backed vaults (git/path) implement it too via
// commonSetAssetInstallations, resolving identity targets against the manifest.
type installSetter interface {
	SetAssetInstallations(ctx context.Context, assetName string, targets []vault.InstallTarget, appendMode bool) (skipped []vault.SkippedTarget, err error)
}

// targetUninstaller is implemented by vaults that can remove specific
// installations by server GID in one best-effort call (the Sleuth/skills.new
// vault). It returns how many installs were removed and a per-target failure
// message for any the server couldn't remove.
type targetUninstaller interface {
	UninstallAssetTargets(ctx context.Context, assetName string, targets []vault.InstallTarget) (removed int, failures []string, err error)
}

// updateLockFile persists an asset's chosen scopes. Identity scopes
// (team/user/bot) — and any edit the user chose to append — are routed through
// the vault's bulk installer, which merges or replaces server-side; repo/path
// replaces keep the lock-file path (which also pins the asset version).
func updateLockFile(ctx context.Context, out *outputHelper, repo vault.Vault, asset *lockfile.Asset, result *scopeResult) error {
	if result.Edited {
		return applyScopeEdit(ctx, out, repo, asset.Name, result.Added, result.Removed)
	}
	if result.ApplyTargets || result.Append || hasIdentityScope(result.Targets) {
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
		return errors.New("this vault does not support team/user/bot scopes")
	}
	resolved, selfEmail, err := resolveSelfUserScopes(ctx, repo, targets)
	if err != nil {
		return err
	}
	skipped, err := setter.SetAssetInstallations(ctx, assetName, resolved, appendMode)
	if err != nil {
		return fmt.Errorf("failed to set installations: %w", err)
	}
	// If every requested target was skipped, nothing landed. Fail loudly
	// rather than report success with only a warning — otherwise a typo'd
	// team/user/bot name silently no-ops (the exact bug the singular install
	// path used to guard against). Partial skips (some targets applied) stay a
	// success with per-target warnings.
	if len(skipped) == len(resolved) && len(resolved) > 0 {
		reasons := make([]string, 0, len(skipped))
		for _, sk := range skipped {
			reasons = append(reasons, fmt.Sprintf("%s (%s)", formatTarget(sk.Target), sk.Reason))
		}
		return fmt.Errorf("no scopes applied: %s", strings.Join(reasons, "; "))
	}
	if selfEmail != "" {
		out.printf("Assigned to you (%s)\n", selfEmail)
	}
	for _, sk := range skipped {
		out.printf("⚠ Skipped %s: %s\n", formatTarget(sk.Target), sk.Reason)
	}
	return nil
}

// applyScopeEdit applies an interactive scope edit as a precise diff: removed
// installs are uninstalled by GID (so kept installs are never re-resolved or
// clobbered) and added installs are appended. Removals run first so that
// "remove X, add Y" can't transiently leave the asset in a wider state than
// intended. Removal is best-effort — per-target failures are reported but don't
// abort the additions.
func applyScopeEdit(ctx context.Context, out *outputHelper, repo vault.Vault, assetName string, added, removed []vault.InstallTarget) error {
	if len(removed) > 0 {
		uninstaller, ok := repo.(targetUninstaller)
		if !ok {
			return errors.New("this vault does not support removing individual scopes")
		}
		n, failures, err := uninstaller.UninstallAssetTargets(ctx, assetName, removed)
		if err != nil {
			return fmt.Errorf("failed to remove scopes: %w", err)
		}
		if n > 0 {
			out.printf("Removed %d scope(s)\n", n)
		}
		for _, f := range failures {
			out.printf("⚠ Could not remove %s\n", f)
		}
	}
	if len(added) > 0 {
		// Append the new targets; resolveSelfUserScopes ("me") and entity
		// resolution happen inside bulkSetInstallTargets.
		if err := bulkSetInstallTargets(ctx, out, repo, assetName, added, true); err != nil {
			return err
		}
	}
	return nil
}

// inheritLockFile preserves existing installation scopes for the asset.
// Used when --yes is provided without scope flags, so existing installations
// are not overwritten.
func inheritLockFile(ctx context.Context, out *outputHelper, repo vault.Vault, asset *lockfile.Asset) error {
	return repo.InheritInstallations(ctx, asset)
}
