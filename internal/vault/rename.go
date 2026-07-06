package vault

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/sleuth-io/sx/internal/manifest"
	"github.com/sleuth-io/sx/internal/mgmt"
	vaultgql "github.com/sleuth-io/sx/internal/vault/graphql"
)

// Renames for the two user-managed groupings. Teams and collections are
// referenced by name throughout the manifest (team scopes, bot
// memberships), so manifest-backed renames cascade in the same
// transaction; the sleuth vault keys both by server GID, so its rename is
// a single mutation.

// TeamRenamer is implemented by vaults whose teams can be renamed.
type TeamRenamer interface {
	RenameTeam(ctx context.Context, oldName, newName string) error
}

// CollectionRenamer is implemented by vaults whose collections can be
// renamed.
type CollectionRenamer interface {
	RenameCollection(ctx context.Context, oldName, newName string) error
}

func commonRenameTeam(vaultRoot string, actor mgmt.Actor, oldName, newName string) error {
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return errors.New("team name cannot be empty")
	}
	if newName == oldName {
		return nil
	}
	return withManifest(vaultRoot, actor, func(m *manifest.Manifest) (*mgmt.AuditEvent, error) {
		team, err := requireTeamAdminInTx(m, oldName, actor)
		if err != nil {
			return nil, err
		}
		if _, err := m.FindTeam(newName); err == nil {
			return nil, fmt.Errorf("a team named %q already exists", newName)
		}
		team.Name = newName
		// Every reference follows the rename in the same transaction:
		// team-scoped installations and bot memberships.
		for i := range m.Assets {
			for j := range m.Assets[i].Scopes {
				s := &m.Assets[i].Scopes[j]
				if s.Kind == manifest.ScopeKindTeam && s.Team == oldName {
					s.Team = newName
				}
			}
		}
		for i := range m.Bots {
			for j, t := range m.Bots[i].Teams {
				if t == oldName {
					m.Bots[i].Teams[j] = newName
				}
			}
		}
		return &mgmt.AuditEvent{
			Event:      mgmt.EventTeamUpdated,
			TargetType: mgmt.TargetTypeTeam,
			Target:     newName,
			Data:       map[string]any{"renamed_from": oldName},
		}, nil
	})
}

func commonRenameCollection(vaultRoot string, actor mgmt.Actor, oldName, newName string) error {
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return errors.New("collection name cannot be empty")
	}
	if newName == oldName {
		return nil
	}
	return withManifest(vaultRoot, actor, func(m *manifest.Manifest) (*mgmt.AuditEvent, error) {
		c, err := m.FindCollection(oldName)
		if err != nil {
			return nil, err
		}
		if _, err := m.FindCollection(newName); err == nil {
			return nil, fmt.Errorf("a collection named %q already exists", newName)
		}
		c.Name = newName
		return &mgmt.AuditEvent{
			Event:      mgmt.EventCollectionUpdated,
			TargetType: mgmt.TargetTypeCollection,
			Target:     newName,
			Data:       map[string]any{"renamed_from": oldName},
		}, nil
	})
}

// RenameTeam renames a team and every reference to it.
func (p *PathVault) RenameTeam(ctx context.Context, oldName, newName string) error {
	return p.withLock(ctx, func(actor mgmt.Actor) error {
		return commonRenameTeam(p.repoPath, actor, oldName, newName)
	})
}

// RenameCollection renames a collection.
func (p *PathVault) RenameCollection(ctx context.Context, oldName, newName string) error {
	return p.withLock(ctx, func(actor mgmt.Actor) error {
		return commonRenameCollection(p.repoPath, actor, oldName, newName)
	})
}

// RenameTeam renames a team and every reference to it.
func (g *GitVault) RenameTeam(ctx context.Context, oldName, newName string) error {
	return g.runInVaultTx(ctx, "Rename team "+oldName+" to "+newName, func(root string, actor mgmt.Actor) error {
		return commonRenameTeam(root, actor, oldName, newName)
	})
}

// RenameCollection renames a collection.
func (g *GitVault) RenameCollection(ctx context.Context, oldName, newName string) error {
	return g.runInVaultTx(ctx, "Rename collection "+oldName+" to "+newName, func(root string, actor mgmt.Actor) error {
		return commonRenameCollection(root, actor, oldName, newName)
	})
}

// RenameTeam renames a team on the server; installations follow the GID.
func (s *SleuthVault) RenameTeam(ctx context.Context, oldName, newName string) error {
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return errors.New("team name cannot be empty")
	}
	gid, err := s.teamGIDByName(ctx, oldName)
	if err != nil {
		return err
	}
	resp, err := vaultgql.UpdateTeam(ctx, s.gqlClient(), vaultgql.UpdateTeamInput{
		Id:   gid,
		Name: &newName,
	})
	if err != nil {
		return err
	}
	if resp.UpdateTeam == nil {
		return nil
	}
	return gqlMutationErrors(resp.UpdateTeam.Errors)
}

// RenameCollection renames a collection on the server; membership follows
// the GID.
func (s *SleuthVault) RenameCollection(ctx context.Context, oldName, newName string) error {
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return errors.New("collection name cannot be empty")
	}
	existing, err := s.findServerCollection(ctx, oldName)
	if err != nil {
		return err
	}
	resp, err := vaultgql.UpdateAssetCollection(ctx, s.gqlClient(), vaultgql.UpdateAssetCollectionInput{
		Gid:  existing.gid,
		Name: &newName,
	})
	if err != nil {
		return err
	}
	if resp.UpdateAssetCollection == nil {
		return nil
	}
	return firstGqlError(resp.UpdateAssetCollection.Errors)
}
