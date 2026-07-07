package vault

import (
	"context"

	"github.com/sleuth-io/sx/internal/manifest"
	"github.com/sleuth-io/sx/internal/mgmt"
)

// Collections are named asset groupings. File-backed vaults store them in
// the manifest (schema v2) and implement CRUD here; the Sleuth vault stores
// them server-side (see sleuth_collections.go). Callers feature-detect via
// the CollectionStore interface.

// CollectionStore is implemented by vaults that support collections.
type CollectionStore interface {
	ListCollections(ctx context.Context) ([]manifest.Collection, error)
	SaveCollection(ctx context.Context, c manifest.Collection) error
	DeleteCollection(ctx context.Context, name string) error
}

func commonListCollections(vaultRoot string) ([]manifest.Collection, error) {
	m, err := loadManifest(vaultRoot)
	if err != nil {
		return nil, err
	}
	if m == nil {
		return []manifest.Collection{}, nil
	}
	out := make([]manifest.Collection, len(m.Collections))
	copy(out, m.Collections)
	return out, nil
}

func saveCollectionInManifest(vaultRoot string, actor mgmt.Actor, c manifest.Collection) error {
	// Normalize first so the create-vs-update decision (audit event,
	// CreatedBy stamping) matches what UpsertCollection actually does.
	c, err := manifest.NormalizeCollection(c)
	if err != nil {
		return err
	}
	return withManifest(vaultRoot, actor, func(m *manifest.Manifest) (*mgmt.AuditEvent, error) {
		event := mgmt.EventCollectionUpdated
		if _, err := m.FindCollection(c.Name); err != nil {
			event = mgmt.EventCollectionCreated
			if c.CreatedBy == "" {
				c.CreatedBy = actor.Email
			}
		}
		saved, err := m.UpsertCollection(c)
		if err != nil {
			return nil, err
		}
		return &mgmt.AuditEvent{
			Event:      event,
			TargetType: mgmt.TargetTypeCollection,
			Target:     saved.Name,
			Data:       map[string]any{"assets": len(saved.Assets)},
		}, nil
	})
}

func deleteCollectionInManifest(vaultRoot string, actor mgmt.Actor, name string) error {
	return withManifest(vaultRoot, actor, func(m *manifest.Manifest) (*mgmt.AuditEvent, error) {
		if err := m.DeleteCollection(name); err != nil {
			return nil, err
		}
		return &mgmt.AuditEvent{
			Event:      mgmt.EventCollectionDeleted,
			TargetType: mgmt.TargetTypeCollection,
			Target:     name,
		}, nil
	})
}

// ListCollections returns the vault's collections.
func (p *PathVault) ListCollections(ctx context.Context) ([]manifest.Collection, error) {
	return commonListCollections(p.repoPath)
}

// SaveCollection creates or replaces a collection.
func (p *PathVault) SaveCollection(ctx context.Context, c manifest.Collection) error {
	return p.withLock(ctx, func(actor mgmt.Actor) error {
		return saveCollectionInManifest(p.repoPath, actor, c)
	})
}

// DeleteCollection removes a collection by name.
func (p *PathVault) DeleteCollection(ctx context.Context, name string) error {
	return p.withLock(ctx, func(actor mgmt.Actor) error {
		return deleteCollectionInManifest(p.repoPath, actor, name)
	})
}

// ListCollections returns the vault's collections.
func (g *GitVault) ListCollections(ctx context.Context) ([]manifest.Collection, error) {
	if err := g.cloneOrUpdate(ctx); err != nil {
		return nil, err
	}
	return commonListCollections(g.repoPath)
}

// SaveCollection creates or replaces a collection and pushes.
func (g *GitVault) SaveCollection(ctx context.Context, c manifest.Collection) error {
	return g.runInVaultTx(ctx, "Save collection "+c.Name, func(root string, actor mgmt.Actor) error {
		return saveCollectionInManifest(root, actor, c)
	})
}

// DeleteCollection removes a collection by name and pushes.
func (g *GitVault) DeleteCollection(ctx context.Context, name string) error {
	return g.runInVaultTx(ctx, "Delete collection "+name, func(root string, actor mgmt.Actor) error {
		return deleteCollectionInManifest(root, actor, name)
	})
}
