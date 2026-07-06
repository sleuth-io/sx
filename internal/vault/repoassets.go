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
	for _, a := range m.Assets {
		for _, s := range a.Scopes {
			if (s.Kind != manifest.ScopeKindRepo && s.Kind != manifest.ScopeKindPath) || s.Repo == "" {
				continue
			}
			if seen[s.Repo] == nil {
				seen[s.Repo] = map[string]bool{}
			}
			if seen[s.Repo][a.Name] {
				continue
			}
			seen[s.Repo][a.Name] = true
			out[s.Repo] = append(out[s.Repo], a.Name)
		}
	}
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
				if repo != "" {
					out[repo] = append(out[repo], name)
				}
			}
		}
		if !conn.PageInfo.HasNextPage || conn.PageInfo.EndCursor == nil {
			return out, nil
		}
		after = conn.PageInfo.EndCursor
	}
}
