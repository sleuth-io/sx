package vault

import (
	"context"
	"errors"

	"github.com/sleuth-io/sx/internal/manifest"
	"github.com/sleuth-io/sx/internal/mgmt"
)

// Collection installs are collection-level scope rows dereferenced at
// resolve time (manifest.Resolve unions them onto member assets; the
// Sleuth vault stores them server-side as AssetCollectionInstallation
// rows). Installing a collection never rewrites its members' scopes:
// adding an asset to an installed collection reaches the collection's
// targets immediately, and uninstalling the collection can't remove a
// member's direct install.

// CollectionInstaller is implemented by vaults that support
// collection-level installs. Callers feature-detect via this interface.
type CollectionInstaller interface {
	// SetCollectionInstallation adds one install target to the collection,
	// deduped against existing rows. Unlike asset installs, an org target
	// is stored as an explicit kind=org row (a collection with no rows
	// grants nothing), so org-wide is both addable and removable.
	SetCollectionInstallation(ctx context.Context, name string, target InstallTarget) error

	// RemoveCollectionInstallation removes one matching install target.
	// Member assets' own scopes are never touched. Removing a row that
	// isn't present is a no-op.
	RemoveCollectionInstallation(ctx context.Context, name string, target InstallTarget) error

	// CurrentCollectionInstallTargets reports the collection's own install
	// targets. The bool reports whether the collection exists; an existing
	// collection with no targets returns an empty list (it grants nothing).
	CurrentCollectionInstallTargets(ctx context.Context, name string) ([]InstallTarget, bool, error)
}

// collectionTargetScope converts an install target into the manifest scope
// row stored on a collection. It differs from installTargetScope in one
// deliberate way: org becomes an explicit kind=org row instead of an error
// or a cleared list, because collection rows are additive grants.
func collectionTargetScope(target InstallTarget, actor mgmt.Actor) (manifest.Scope, error) {
	if target.Kind == InstallKindOrg {
		return manifest.Scope{Kind: manifest.ScopeKindOrg}, nil
	}
	return installTargetScope(target, actor)
}

func commonSetCollectionInstallation(vaultRoot string, actor mgmt.Actor, name string, target InstallTarget) error {
	s, err := collectionTargetScope(target, actor)
	if err != nil {
		return err
	}
	return withManifest(vaultRoot, actor, func(m *manifest.Manifest) (*mgmt.AuditEvent, error) {
		c, err := m.FindCollection(name)
		if err != nil {
			return nil, err
		}
		// Re-check referenced entities inside the transaction, mirroring
		// commonSetAssetInstallation's TOCTOU guard.
		if target.Kind == InstallKindTeam {
			if err := teamExistsInTx(m, target.Team); err != nil {
				return nil, err
			}
		}
		if target.Kind == InstallKindBot {
			if _, err := findBotForMgmt(m, target.Bot); err != nil {
				return nil, err
			}
		}
		if scopeExistsOnAsset(c.Scopes, s) {
			return nil, nil
		}
		c.Scopes = append(c.Scopes, s)
		return &mgmt.AuditEvent{
			Event:      mgmt.EventCollectionInstalled,
			TargetType: mgmt.TargetTypeCollection,
			Target:     name,
			Data:       target.AuditData(),
		}, nil
	})
}

func commonRemoveCollectionInstallation(vaultRoot string, actor mgmt.Actor, name string, target InstallTarget) error {
	needle, err := collectionTargetScope(target, actor)
	if err != nil {
		return err
	}
	return withManifest(vaultRoot, actor, func(m *manifest.Manifest) (*mgmt.AuditEvent, error) {
		c, err := m.FindCollection(name)
		if err != nil {
			return nil, err
		}
		if target.Kind == InstallKindTeam {
			if err := teamExistsInTx(m, target.Team); err != nil {
				return nil, err
			}
		}
		changed := false
		kept := c.Scopes[:0]
		for _, s := range c.Scopes {
			if collectionScopeMatches(s, needle) {
				changed = true
				continue
			}
			kept = append(kept, s)
		}
		if !changed {
			return nil, nil
		}
		c.Scopes = kept
		return &mgmt.AuditEvent{
			Event:      mgmt.EventCollectionUninstalled,
			TargetType: mgmt.TargetTypeCollection,
			Target:     name,
			Data:       target.AuditData(),
		}, nil
	})
}

// collectionScopeMatches extends installScopeMatches with the org kind,
// which exists as a stored row only on collections.
func collectionScopeMatches(row, needle manifest.Scope) bool {
	if needle.Kind == manifest.ScopeKindOrg {
		return row.Kind == manifest.ScopeKindOrg
	}
	return installScopeMatches(row, needle)
}

func commonCurrentCollectionInstallTargets(vaultRoot, name string) ([]InstallTarget, bool, error) {
	m, err := loadManifest(vaultRoot)
	if err != nil {
		return nil, false, err
	}
	if m == nil {
		return nil, false, nil
	}
	c, err := m.FindCollection(name)
	if err != nil {
		if errors.Is(err, manifest.ErrCollectionNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}
	targets := make([]InstallTarget, 0, len(c.Scopes))
	for _, s := range c.Scopes {
		if s.Kind == manifest.ScopeKindOrg {
			targets = append(targets, InstallTarget{Kind: InstallKindOrg})
			continue
		}
		if t, ok := manifestScopeToTarget(s); ok {
			targets = append(targets, t)
		}
	}
	return targets, true, nil
}

// SetCollectionInstallation adds an install target to a collection.
func (p *PathVault) SetCollectionInstallation(ctx context.Context, name string, target InstallTarget) error {
	return p.withLock(ctx, func(actor mgmt.Actor) error {
		return commonSetCollectionInstallation(p.repoPath, actor, name, target)
	})
}

// RemoveCollectionInstallation removes an install target from a collection.
func (p *PathVault) RemoveCollectionInstallation(ctx context.Context, name string, target InstallTarget) error {
	return p.withLock(ctx, func(actor mgmt.Actor) error {
		return commonRemoveCollectionInstallation(p.repoPath, actor, name, target)
	})
}

// CurrentCollectionInstallTargets reports a collection's install targets.
func (p *PathVault) CurrentCollectionInstallTargets(ctx context.Context, name string) ([]InstallTarget, bool, error) {
	return commonCurrentCollectionInstallTargets(p.repoPath, name)
}

// SetCollectionInstallation adds an install target to a collection and pushes.
func (g *GitVault) SetCollectionInstallation(ctx context.Context, name string, target InstallTarget) error {
	return g.runInVaultTx(ctx, "Install collection "+name, func(root string, actor mgmt.Actor) error {
		return commonSetCollectionInstallation(root, actor, name, target)
	})
}

// RemoveCollectionInstallation removes an install target and pushes.
func (g *GitVault) RemoveCollectionInstallation(ctx context.Context, name string, target InstallTarget) error {
	return g.runInVaultTx(ctx, "Uninstall collection "+name, func(root string, actor mgmt.Actor) error {
		return commonRemoveCollectionInstallation(root, actor, name, target)
	})
}

// CurrentCollectionInstallTargets reports a collection's install targets.
func (g *GitVault) CurrentCollectionInstallTargets(ctx context.Context, name string) ([]InstallTarget, bool, error) {
	if err := g.cloneOrUpdate(ctx); err != nil {
		return nil, false, err
	}
	return commonCurrentCollectionInstallTargets(g.repoPath, name)
}

// collectionRepoAssets flattens collection-level repo/path scopes onto
// member asset names — the collection-derived half of ListRepoAssets for
// file-backed vaults. Repo URLs are stored normalized (the write path runs
// them through installTargetScope), so they key consistently with the
// asset-scope rows.
func collectionRepoAssets(m *manifest.Manifest, add func(repoURL, assetName string)) {
	for i := range m.Collections {
		c := &m.Collections[i]
		for _, s := range c.Scopes {
			if (s.Kind != manifest.ScopeKindRepo && s.Kind != manifest.ScopeKindPath) || s.Repo == "" {
				continue
			}
			for _, assetName := range c.Assets {
				add(s.Repo, assetName)
			}
		}
	}
}

// collectionTeamAssets flattens collection-level team scopes onto member
// asset names — the collection-derived half of ListTeamAssets for
// file-backed vaults.
func collectionTeamAssets(m *manifest.Manifest, add func(team, assetName string)) {
	for i := range m.Collections {
		c := &m.Collections[i]
		for _, s := range c.Scopes {
			if s.Kind != manifest.ScopeKindTeam || s.Team == "" {
				continue
			}
			for _, assetName := range c.Assets {
				add(s.Team, assetName)
			}
		}
	}
}
