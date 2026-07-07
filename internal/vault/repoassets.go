package vault

import (
	"context"
	"strings"

	"github.com/sleuth-io/sx/internal/manifest"
	vaultgql "github.com/sleuth-io/sx/internal/vault/graphql"
)

// RepoAssetLister reports which assets are scoped to each repository — the
// repository-centric inverse of per-asset install targets. Used by the
// desktop app's repository views (per-library opt-in).
type RepoAssetLister interface {
	ListRepoAssets(ctx context.Context) (map[string][]string, error)
}

func manifestRepoAssets(vaultRoot string) (map[string][]string, error) {
	m, err := loadManifest(vaultRoot)
	if err != nil {
		return nil, err
	}
	out := map[string][]string{}
	if m == nil {
		return out, nil
	}
	// One row exists per published version and rows for the same asset are
	// not necessarily adjacent — dedupe with a per-repo seen set. Path
	// scopes name a repository too; the asset still belongs to that repo.
	seen := map[string]map[string]bool{}
	add := func(repoURL, assetName string) {
		if repoURL == "" {
			return
		}
		if seen[repoURL] == nil {
			seen[repoURL] = map[string]bool{}
		}
		if seen[repoURL][assetName] {
			return
		}
		seen[repoURL][assetName] = true
		out[repoURL] = append(out[repoURL], assetName)
	}
	for _, a := range m.Assets {
		for _, s := range a.Scopes {
			if (s.Kind != manifest.ScopeKindRepo && s.Kind != manifest.ScopeKindPath) || s.Repo == "" {
				continue
			}
			add(s.Repo, a.Name)
		}
	}
	// Collection installs scope their member assets to the repo at resolve
	// time, so the repo view reports those alongside directly-scoped ones.
	collectionRepoAssets(m, add)
	return out, nil
}

// ListRepoAssets maps repository URL → asset names scoped to it.
func (p *PathVault) ListRepoAssets(ctx context.Context) (map[string][]string, error) {
	return manifestRepoAssets(p.repoPath)
}

// ListRepoAssets maps repository URL → asset names scoped to it.
func (g *GitVault) ListRepoAssets(ctx context.Context) (map[string][]string, error) {
	if err := g.cloneOrUpdate(ctx); err != nil {
		return nil, err
	}
	return manifestRepoAssets(g.repoPath)
}

// ListRepoAssets maps repository → asset names scoped to it, read in one
// paged query over the vault's assets and their installations.
func (s *SleuthVault) ListRepoAssets(ctx context.Context) (map[string][]string, error) {
	out := map[string][]string{}
	// An asset can reach the same repository through several installation
	// records (e.g. two path scopes under one repo) — dedupe like the
	// manifest path does, or sidebar counts inflate.
	seen := map[string]map[string]bool{}
	pageSize := 50
	var after *string
	for {
		resp, err := vaultgql.AssetInstallations(ctx, s.gqlClient(), &pageSize, after)
		if err != nil {
			return nil, err
		}
		conn := resp.Vault.Assets
		for i := range conn.Nodes {
			node := conn.Nodes[i]
			name := node.GetSlug()
			if name == "" {
				name = node.GetName()
			}
			for _, inst := range node.GetInstallations() {
				if !strings.EqualFold(string(inst.EntityType), "REPOSITORY") {
					continue
				}
				// EntityRef carries the repository URL; EntityName the
				// display name. Prefer the URL to match manifest vaults.
				repo := ""
				if inst.EntityRef != nil {
					repo = *inst.EntityRef
				}
				if repo == "" {
					repo = inst.EntityName
				}
				if repo == "" {
					continue
				}
				if seen[repo] == nil {
					seen[repo] = map[string]bool{}
				}
				if seen[repo][name] {
					continue
				}
				seen[repo][name] = true
				out[repo] = append(out[repo], name)
			}
		}
		if !conn.PageInfo.HasNextPage || conn.PageInfo.EndCursor == nil {
			return out, nil
		}
		after = conn.PageInfo.EndCursor
	}
}
