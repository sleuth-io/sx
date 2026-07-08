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

// UserAssetLister reports which assets carry personal (user) scopes —
// the person-centric inverse of per-asset install targets, keyed by
// normalized email. Used by the desktop app's "My skills" view.
type UserAssetLister interface {
	ListUserAssets(ctx context.Context) (map[string][]string, error)
}

func manifestUserAssets(vaultRoot string) (map[string][]string, error) {
	m, err := loadManifest(vaultRoot)
	if err != nil {
		return nil, err
	}
	out := map[string][]string{}
	if m == nil {
		return out, nil
	}
	// One row exists per published version and rows for the same asset are
	// not necessarily adjacent — dedupe with a per-user seen set.
	seen := map[string]map[string]bool{}
	add := func(email, assetName string) {
		email = manifest.NormalizeEmail(email)
		if email == "" {
			return
		}
		if seen[email] == nil {
			seen[email] = map[string]bool{}
		}
		if seen[email][assetName] {
			return
		}
		seen[email][assetName] = true
		out[email] = append(out[email], assetName)
	}
	for _, a := range m.Assets {
		for _, s := range a.Scopes {
			if s.Kind != manifest.ScopeKindUser || s.User == "" {
				continue
			}
			add(s.User, a.Name)
		}
	}
	// Collection installs reach the user at resolve time, so the personal
	// view reports those member assets alongside directly-scoped ones.
	collectionUserAssets(m, add)
	return out, nil
}

// collectionUserAssets flattens collection-level user scopes onto member
// asset names — the collection-derived half of ListUserAssets for
// file-backed vaults.
func collectionUserAssets(m *manifest.Manifest, add func(email, assetName string)) {
	for i := range m.Collections {
		c := &m.Collections[i]
		for _, s := range c.Scopes {
			if s.Kind != manifest.ScopeKindUser || s.User == "" {
				continue
			}
			for _, assetName := range c.Assets {
				add(s.User, assetName)
			}
		}
	}
}

// ListUserAssets maps normalized email → asset names installed just for
// that person.
func (p *PathVault) ListUserAssets(ctx context.Context) (map[string][]string, error) {
	return manifestUserAssets(p.repoPath)
}

// ListUserAssets maps normalized email → asset names installed just for
// that person.
func (g *GitVault) ListUserAssets(ctx context.Context) (map[string][]string, error) {
	if err := g.cloneOrUpdate(ctx); err != nil {
		return nil, err
	}
	return manifestUserAssets(g.repoPath)
}

// ListUserAssets maps normalized email → asset names installed just for
// that person, read from the same paged installations query
// ListTeamAssets uses. For USER installations the email is entityRef —
// entityName is the person's display name (see schema.graphql:
// "Canonical reference used to install this entity … the email for USER
// installations"); entityName is only a fallback for servers that
// predate the ref field.
func (s *SleuthVault) ListUserAssets(ctx context.Context) (map[string][]string, error) {
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
				if !strings.EqualFold(string(inst.EntityType), "USER") {
					continue
				}
				ref := inst.EntityName
				if inst.EntityRef != nil && *inst.EntityRef != "" {
					ref = *inst.EntityRef
				}
				if email := manifest.NormalizeEmail(ref); email != "" {
					out[email] = append(out[email], name)
				}
			}
		}
		if !conn.PageInfo.HasNextPage || conn.PageInfo.EndCursor == nil {
			return out, nil
		}
		after = conn.PageInfo.EndCursor
	}
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
