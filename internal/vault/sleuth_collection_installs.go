package vault

import (
	"context"
	"errors"
	"fmt"

	vaultgql "github.com/sleuth-io/sx/internal/vault/graphql"
)

// Collection installs on a Sleuth vault are server-side
// AssetCollectionInstallation rows (one per target), resolved at read
// time — the installCollection / uninstallCollection mutations, never a
// per-asset fan-out. Member assets' own installations are untouched, so a
// direct install always survives a collection uninstall.

// findServerCollectionLean resolves a collection by exact name WITHOUT
// fetching its membership — the install paths only need the GID.
func (s *SleuthVault) findServerCollectionLean(ctx context.Context, name string) (*sleuthCollection, error) {
	all, err := s.listServerCollectionsLean(ctx)
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].name == name {
			return &all[i], nil
		}
	}
	return nil, errServerCollectionNotFound
}

// collectionInstallationInput maps an install target to the GraphQL input
// installCollection expects. Unlike asset installs — which accept
// repository URLs directly — collection targets are GID-based, so
// repositories resolve through the org repo list.
func (s *SleuthVault) collectionInstallationInput(ctx context.Context, target InstallTarget) (vaultgql.AssetInstallationInput, error) {
	in := vaultgql.AssetInstallationInput{}
	et, ok := installKindToEntityType(target.Kind)
	if !ok {
		return in, fmt.Errorf("unknown install kind: %q", target.Kind)
	}
	in.EntityType = et
	switch target.Kind {
	case InstallKindOrg:
		return in, nil
	case InstallKindTeam:
		gid, err := s.teamGIDByName(ctx, target.Team)
		if err != nil {
			return in, err
		}
		in.EntityId = &gid
		return in, nil
	case InstallKindUser:
		gid, err := s.userGIDByEmail(ctx, target.User)
		if err != nil {
			return in, err
		}
		in.EntityId = &gid
		return in, nil
	case InstallKindBot:
		gid, err := s.botGIDByName(ctx, target.Bot)
		if err != nil {
			return in, err
		}
		in.EntityId = &gid
		return in, nil
	case InstallKindRepo:
		gid, err := s.repoGIDByIdentifier(ctx, target.Repo)
		if err != nil {
			return in, err
		}
		in.EntityId = &gid
		return in, nil
	case InstallKindPath:
		// A path target needs a mono-repo config GID; sx has no resolver
		// from raw paths to a config yet. Scope the whole repo instead.
		return in, errors.New("path-scoped collection installs aren't supported yet — install to the whole repository instead")
	}
	return in, fmt.Errorf("unknown install kind: %q", target.Kind)
}

// repoGIDByIdentifier resolves a repository URL or owner/name slug to its
// server GID via the org repository list.
func (s *SleuthVault) repoGIDByIdentifier(ctx context.Context, identifier string) (string, error) {
	repos, err := s.orgRepoGIDsByOwnerName(ctx)
	if err != nil {
		return "", err
	}
	if gid, ok := repos[trailingOwnerName(identifier)]; ok {
		return gid, nil
	}
	return "", fmt.Errorf("repository %q not found in this vault's organization", identifier)
}

// SetCollectionInstallation installs the collection to one target via the
// installCollection mutation.
func (s *SleuthVault) SetCollectionInstallation(ctx context.Context, name string, target InstallTarget) error {
	col, err := s.findServerCollectionLean(ctx, name)
	if err != nil {
		return err
	}
	input, err := s.collectionInstallationInput(ctx, target)
	if err != nil {
		return err
	}
	resp, err := vaultgql.InstallCollection(ctx, s.gqlClient(), vaultgql.InstallCollectionInput{
		Gid:           col.gid,
		Installations: []vaultgql.AssetInstallationInput{input},
	})
	if err != nil {
		return err
	}
	if resp.InstallCollection == nil {
		return errors.New("missing installCollection payload in response")
	}
	if err := firstGqlError(resp.InstallCollection.Errors); err != nil {
		return err
	}
	s.refreshLockFileCache()
	return nil
}

// RemoveCollectionInstallation removes the collection's installation row
// for one target via the uninstallCollection mutation.
func (s *SleuthVault) RemoveCollectionInstallation(ctx context.Context, name string, target InstallTarget) error {
	col, err := s.findServerCollectionLean(ctx, name)
	if err != nil {
		return err
	}
	input, err := s.collectionInstallationInput(ctx, target)
	if err != nil {
		return err
	}
	resp, err := vaultgql.UninstallCollection(ctx, s.gqlClient(), vaultgql.UninstallCollectionInput{
		Gid:        col.gid,
		EntityType: input.EntityType,
		EntityId:   input.EntityId,
	})
	if err != nil {
		return err
	}
	if resp.UninstallCollection == nil {
		return errors.New("missing uninstallCollection payload in response")
	}
	if err := firstGqlError(resp.UninstallCollection.Errors); err != nil {
		// Removing an absent row is a no-op on file vaults; match that
		// here rather than surfacing the server's "Installation not found".
		if err.Error() == "Installation not found" {
			return nil
		}
		return err
	}
	s.refreshLockFileCache()
	return nil
}

// CurrentCollectionInstallTargets reads the collection's own installation
// rows. Unlike assets — where org-wide collapses to an empty target list —
// an org row is reported as an explicit org target, matching the
// collection semantics (no rows = grants nothing).
func (s *SleuthVault) CurrentCollectionInstallTargets(ctx context.Context, name string) ([]InstallTarget, bool, error) {
	col, err := s.findServerCollectionLean(ctx, name)
	if err != nil {
		if errors.Is(err, errServerCollectionNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}
	resp, err := vaultgql.CollectionInstallations(ctx, s.gqlClient(), col.gid)
	if err != nil {
		return nil, false, err
	}
	targets := make([]InstallTarget, 0, len(resp.Collection.Installations))
	for _, inst := range resp.Collection.Installations {
		entityID := derefStr(inst.EntityId)
		switch inst.EntityType {
		case vaultgql.VaultAssetInstallationEntityTypeOrganization:
			targets = append(targets, InstallTarget{Kind: InstallKindOrg})
		case vaultgql.VaultAssetInstallationEntityTypeRepository:
			targets = append(targets, InstallTarget{
				Kind: InstallKindRepo, Repo: inst.EntityName,
				EntityID: entityID, MonoRepoConfigID: derefStr(inst.MonoRepoConfigId),
			})
		case vaultgql.VaultAssetInstallationEntityTypeTeam:
			targets = append(targets, InstallTarget{Kind: InstallKindTeam, Team: inst.EntityName, EntityID: entityID})
		case vaultgql.VaultAssetInstallationEntityTypeUser:
			user := inst.EntityName
			if inst.EntityRef != nil && *inst.EntityRef != "" {
				user = *inst.EntityRef
			}
			targets = append(targets, InstallTarget{Kind: InstallKindUser, User: user, EntityID: entityID})
		case vaultgql.VaultAssetInstallationEntityTypeBot:
			targets = append(targets, InstallTarget{Kind: InstallKindBot, Bot: inst.EntityName, EntityID: entityID})
		}
	}
	return targets, true, nil
}
