package vault

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/sleuth-io/sx/internal/manifest"
	vaultgql "github.com/sleuth-io/sx/internal/vault/graphql"
)

// Collections on a Sleuth vault live server-side (skills.new). The
// CollectionStore methods translate between the manifest's name-based
// collection shape and the server's GID-based API: assets are referenced
// by slug locally and resolved to GIDs for mutations.

type sleuthCollection struct {
	gid         string
	name        string
	description string
	assetGIDs   map[string]bool
	assetSlugs  []string
}

// firstGqlError surfaces the first structured mutation error, if any.
// The generated error types expose GetMessages on pointer receivers.
func firstGqlError[T any, P interface {
	*T
	GetMessages() []string
}](errs []T) error {
	if len(errs) == 0 {
		return nil
	}
	if msgs := P(&errs[0]).GetMessages(); len(msgs) > 0 {
		return errors.New(msgs[0])
	}
	return errors.New("request failed")
}

// listServerCollectionsLean pages the collections without their (per-
// collection, per-page) memberships; callers fetch assets only for the
// collections they actually need.
func (s *SleuthVault) listServerCollectionsLean(ctx context.Context) ([]sleuthCollection, error) {
	out := []sleuthCollection{}
	pageSize := 50
	var after *string
	for {
		resp, err := vaultgql.ListCollections(ctx, s.gqlClient(), pageSize, after)
		if err != nil {
			return nil, err
		}
		conn := resp.Collections
		for i := range conn.Nodes {
			node := conn.Nodes[i]
			out = append(out, sleuthCollection{
				gid:         node.Id,
				name:        node.Name,
				description: node.Description,
			})
		}
		if !conn.PageInfo.HasNextPage || conn.PageInfo.EndCursor == nil {
			return out, nil
		}
		after = conn.PageInfo.EndCursor
	}
}

// fillCollectionAssets pages the collection's member assets (the server
// caps relay page sizes at 50, so membership is read page by page).
func (s *SleuthVault) fillCollectionAssets(ctx context.Context, col *sleuthCollection) error {
	col.assetGIDs = map[string]bool{}
	pageSize := 50
	var after *string
	for {
		resp, err := vaultgql.CollectionAssets(ctx, s.gqlClient(), col.gid, pageSize, after)
		if err != nil {
			return err
		}
		conn := resp.Collection.Assets
		for _, a := range conn.Nodes {
			col.assetGIDs[a.GetId()] = true
			col.assetSlugs = append(col.assetSlugs, a.GetSlug())
		}
		if !conn.PageInfo.HasNextPage || conn.PageInfo.EndCursor == nil {
			sort.Strings(col.assetSlugs)
			return nil
		}
		after = conn.PageInfo.EndCursor
	}
}

// errServerCollectionNotFound is internal: callers translate it into
// their own not-found semantics (create-on-save, ErrCollectionNotFound).
var errServerCollectionNotFound = errors.New("collection not found")

// findServerCollection resolves a collection by exact name, fetching
// membership for that one collection only.
func (s *SleuthVault) findServerCollection(ctx context.Context, name string) (*sleuthCollection, error) {
	all, err := s.listServerCollectionsLean(ctx)
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].name == name {
			if err := s.fillCollectionAssets(ctx, &all[i]); err != nil {
				return nil, err
			}
			return &all[i], nil
		}
	}
	return nil, errServerCollectionNotFound
}

// assetGIDsBySlugs resolves asset slugs to GIDs in one pass over the
// vault's asset pages. Order follows the input; unknown slugs are an error.
func (s *SleuthVault) assetGIDsBySlugs(ctx context.Context, slugs []string) ([]string, error) {
	if len(slugs) == 0 {
		return []string{}, nil
	}
	wanted := map[string]string{}
	for _, slug := range slugs {
		wanted[strings.TrimSpace(slug)] = ""
	}
	pageSize := 50
	var after *string
	for {
		resp, err := vaultgql.AssetGID(ctx, s.gqlClient(), &pageSize, after)
		if err != nil {
			return nil, err
		}
		conn := resp.Vault.Assets
		for _, n := range conn.Nodes {
			if _, ok := wanted[n.GetSlug()]; ok {
				wanted[n.GetSlug()] = n.GetId()
			}
		}
		if !conn.PageInfo.HasNextPage || conn.PageInfo.EndCursor == nil {
			break
		}
		after = conn.PageInfo.EndCursor
	}
	out := make([]string, 0, len(slugs))
	var missing []string
	for _, slug := range slugs {
		gid := wanted[strings.TrimSpace(slug)]
		if gid == "" {
			missing = append(missing, slug)
			continue
		}
		out = append(out, gid)
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("%w: %s", ErrAssetNotFound, strings.Join(missing, ", "))
	}
	return out, nil
}

// ListCollections returns the vault's collections with memberships.
func (s *SleuthVault) ListCollections(ctx context.Context) ([]manifest.Collection, error) {
	server, err := s.listServerCollectionsLean(ctx)
	if err != nil {
		return nil, err
	}
	for i := range server {
		if err := s.fillCollectionAssets(ctx, &server[i]); err != nil {
			return nil, err
		}
	}
	out := make([]manifest.Collection, 0, len(server))
	for _, col := range server {
		assets := col.assetSlugs
		if assets == nil {
			assets = []string{}
		}
		out = append(out, manifest.Collection{
			Name:        col.name,
			Description: col.description,
			Assets:      assets,
		})
	}
	return out, nil
}

// SaveCollection creates or replaces a collection: the server copy ends up
// with exactly the given description and membership.
func (s *SleuthVault) SaveCollection(ctx context.Context, c manifest.Collection) error {
	// Same normalization the manifest-backed stores apply: trimmed name and
	// description, deduped sorted assets, blank names rejected.
	c, err := manifest.NormalizeCollection(c)
	if err != nil {
		return err
	}
	existing, err := s.findServerCollection(ctx, c.Name)
	if err != nil && !errors.Is(err, errServerCollectionNotFound) {
		return err
	}
	gids, err := s.assetGIDsBySlugs(ctx, c.Assets)
	if err != nil {
		return err
	}

	if existing == nil {
		resp, err := vaultgql.CreateAssetCollection(ctx, s.gqlClient(), vaultgql.CreateAssetCollectionInput{
			Name:        c.Name,
			Description: &c.Description,
			AssetGids:   gids,
		})
		if err != nil {
			return err
		}
		return firstGqlError(resp.CreateAssetCollection.Errors)
	}

	if existing.description != c.Description {
		resp, err := vaultgql.UpdateAssetCollection(ctx, s.gqlClient(), vaultgql.UpdateAssetCollectionInput{
			Gid:         existing.gid,
			Description: &c.Description,
		})
		if err != nil {
			return err
		}
		if err := firstGqlError(resp.UpdateAssetCollection.Errors); err != nil {
			return err
		}
	}

	desired := map[string]bool{}
	var toAdd []string
	for _, gid := range gids {
		desired[gid] = true
		if !existing.assetGIDs[gid] {
			toAdd = append(toAdd, gid)
		}
	}
	var toRemove []string
	for gid := range existing.assetGIDs {
		if !desired[gid] {
			toRemove = append(toRemove, gid)
		}
	}
	sort.Strings(toRemove)

	if len(toAdd) > 0 {
		resp, err := vaultgql.AddAssetsToCollection(ctx, s.gqlClient(), vaultgql.ModifyAssetCollectionAssetsInput{
			CollectionGid: existing.gid,
			AssetGids:     toAdd,
		})
		if err != nil {
			return err
		}
		if err := firstGqlError(resp.AddAssetsToCollection.Errors); err != nil {
			return err
		}
	}
	if len(toRemove) > 0 {
		resp, err := vaultgql.RemoveAssetsFromCollection(ctx, s.gqlClient(), vaultgql.ModifyAssetCollectionAssetsInput{
			CollectionGid: existing.gid,
			AssetGids:     toRemove,
		})
		if err != nil {
			return err
		}
		if err := firstGqlError(resp.RemoveAssetsFromCollection.Errors); err != nil {
			return err
		}
	}
	return nil
}

// DeleteCollection removes a collection. Member assets are untouched.
func (s *SleuthVault) DeleteCollection(ctx context.Context, name string) error {
	all, err := s.listServerCollectionsLean(ctx)
	if err != nil {
		return err
	}
	var existing *sleuthCollection
	for i := range all {
		if all[i].name == name {
			existing = &all[i]
			break
		}
	}
	if existing == nil {
		return manifest.ErrCollectionNotFound
	}
	resp, err := vaultgql.DeleteAssetCollection(ctx, s.gqlClient(), existing.gid)
	if err != nil {
		return err
	}
	return firstGqlError(resp.DeleteAssetCollection.Errors)
}
