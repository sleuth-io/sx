package vault

import (
	"context"
	"strings"

	"github.com/sleuth-io/sx/internal/manifest"
	vaultgql "github.com/sleuth-io/sx/internal/vault/graphql"
)

// TeamAssetLister reports which assets are shared with each team — the
// team-centric inverse of per-asset install targets. Used by the desktop
// app's team views.
type TeamAssetLister interface {
	ListTeamAssets(ctx context.Context) (map[string][]string, error)
}

func manifestTeamAssets(vaultRoot string) (map[string][]string, error) {
	m, err := loadManifest(vaultRoot)
	if err != nil {
		return nil, err
	}
	out := map[string][]string{}
	if m == nil {
		return out, nil
	}
	// One row exists per published version and rows for the same asset are
	// not necessarily adjacent — dedupe with a per-team seen set.
	seen := map[string]map[string]bool{}
	add := func(team, assetName string) {
		if seen[team] == nil {
			seen[team] = map[string]bool{}
		}
		if seen[team][assetName] {
			return
		}
		seen[team][assetName] = true
		out[team] = append(out[team], assetName)
	}
	for _, a := range m.Assets {
		for _, s := range a.Scopes {
			if s.Kind != manifest.ScopeKindTeam || s.Team == "" {
				continue
			}
			add(s.Team, a.Name)
		}
	}
	// Collection installs reach the team's members at resolve time, so the
	// team view reports those member assets alongside directly-scoped ones.
	collectionTeamAssets(m, add)
	return out, nil
}

// ListTeamAssets maps team name → asset names shared with that team.
func (p *PathVault) ListTeamAssets(ctx context.Context) (map[string][]string, error) {
	return manifestTeamAssets(p.repoPath)
}

// ListTeamAssets maps team name → asset names shared with that team.
func (g *GitVault) ListTeamAssets(ctx context.Context) (map[string][]string, error) {
	if err := g.cloneOrUpdate(ctx); err != nil {
		return nil, err
	}
	return manifestTeamAssets(g.repoPath)
}

// ListTeamAssets maps team name → asset names shared with that team, read
// in one paged query over the vault's assets and their installations.
func (s *SleuthVault) ListTeamAssets(ctx context.Context) (map[string][]string, error) {
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
				if strings.EqualFold(string(inst.EntityType), "TEAM") && inst.EntityName != "" {
					out[inst.EntityName] = append(out[inst.EntityName], name)
				}
			}
		}
		if !conn.PageInfo.HasNextPage || conn.PageInfo.EndCursor == nil {
			return out, nil
		}
		after = conn.PageInfo.EndCursor
	}
}
