package vault

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/sleuth-io/sx/internal/manifest"
	"github.com/sleuth-io/sx/internal/mgmt"
)

// assetEditReason returns "" if actor may edit/publish the named asset, or a
// human-readable denial reason. Per docs/rbac.md, editing is governed by the
// skill's team scope (not the org-admins state):
//
//   - A brand-new asset (absent from the manifest) → anyone may create it.
//   - Org-admins → may always edit anything.
//   - A skill scoped to a team → only a member of that team may edit it.
//     Scoped to several teams → a member of any of them.
//   - A skill not scoped to any team → anyone may edit it.
func assetEditReason(m *manifest.Manifest, name string, actor mgmt.Actor) string {
	a := m.FindAsset(name)
	if a == nil {
		return "" // new asset → anyone may create it
	}
	if m.IsOrgAdmin(actor.Email) {
		return "" // org-admins may always edit
	}
	var teamScopes []string
	for _, s := range a.Scopes {
		if s.Kind == manifest.ScopeKindTeam {
			teamScopes = append(teamScopes, s.Team)
		}
	}
	if len(teamScopes) == 0 {
		return "" // not team-scoped → anyone may edit
	}
	for _, tn := range teamScopes {
		// Editing is member-level: team admins are auto-added as members, so a
		// membership check alone suffices (admin status gates *scoping*, not edits).
		if team, err := m.FindTeam(tn); err == nil && team.IsMember(actor.Email) {
			return ""
		}
	}
	return fmt.Sprintf("permission denied: %q is scoped to team %s — only a member of that team (or an org-admin) may edit it",
		name, strings.Join(teamScopes, ", "))
}

// commonAssetEditPermission is the file-backed (git/path) edit gate. It reads
// the manifest and returns an error when actor may not publish a new version of
// the named asset.
func commonAssetEditPermission(vaultRoot string, actor mgmt.Actor, name string) error {
	m, err := loadManifest(vaultRoot)
	if err != nil {
		return err
	}
	if reason := assetEditReason(m, name, actor); reason != "" {
		return errors.New(reason)
	}
	return nil
}

// CheckAssetEditPermission enforces the file-backed edit gate for a path vault.
func (p *PathVault) CheckAssetEditPermission(ctx context.Context, name string) error {
	actor, err := p.CurrentActor(ctx)
	if err != nil {
		return err
	}
	return commonAssetEditPermission(p.repoPath, actor, name)
}

// CheckAssetEditPermission enforces the file-backed edit gate for a git vault.
func (g *GitVault) CheckAssetEditPermission(ctx context.Context, name string) error {
	if err := g.cloneOrUpdate(ctx); err != nil {
		return err
	}
	actor, err := g.CurrentActor(ctx)
	if err != nil {
		return err
	}
	return commonAssetEditPermission(g.repoPath, actor, name)
}
