package vault

import (
	"context"
	"fmt"
	"strings"

	"github.com/sleuth-io/sx/internal/manifest"
	"github.com/sleuth-io/sx/internal/mgmt"
)

// AssetEditPermissionError is returned when the actor may not publish a new
// version of an asset. It carries the asset name so callers (notably `sx add`)
// can recognize the denial via errors.As and offer a pull-request fallback
// instead of just failing. See docs/rbac.md.
//
// Teams is set only by the file-backed git/path gate, which reads the gating
// team scopes from the manifest. The Sleuth vault learns of the denial from the
// server (a 403 on upload) and doesn't get the teams, but it does get AssetGID —
// the skill's server GID — which it passes to createAssetPullRequest to open the
// PR. Either field may be empty depending on which vault produced the error.
type AssetEditPermissionError struct {
	Asset    string
	Teams    []string
	AssetGID string
}

func (e *AssetEditPermissionError) Error() string {
	if len(e.Teams) > 0 {
		return fmt.Sprintf("permission denied: %q is scoped to team %s — only a member of that team (or an org-admin) may edit it",
			e.Asset, strings.Join(e.Teams, ", "))
	}
	return fmt.Sprintf("permission denied: you may not publish %q directly", e.Asset)
}

// assetEditDenial returns nil if actor may edit/publish the named asset, or an
// *AssetEditPermissionError describing why not. Per docs/rbac.md, editing is
// governed by the skill's team scope (not the org-admins state):
//
//   - A brand-new asset (absent from the manifest) → anyone may create it.
//   - Org-admins → may always edit anything.
//   - A skill scoped to a team → only a member of that team may edit it.
//     Scoped to several teams → a member of any of them.
//   - A skill not scoped to any team → anyone may edit it.
func assetEditDenial(m *manifest.Manifest, name string, actor mgmt.Actor) *AssetEditPermissionError {
	a := m.FindAsset(name)
	if a == nil {
		return nil // new asset → anyone may create it
	}
	if m.IsOrgAdmin(actor.Email) {
		return nil // org-admins may always edit
	}
	var teamScopes []string
	for _, s := range a.Scopes {
		if s.Kind == manifest.ScopeKindTeam {
			teamScopes = append(teamScopes, s.Team)
		}
	}
	if len(teamScopes) == 0 {
		return nil // not team-scoped → anyone may edit
	}
	for _, tn := range teamScopes {
		// Editing is member-level: team admins are auto-added as members, so a
		// membership check alone suffices (admin status gates *scoping*, not edits).
		if team, err := m.FindTeam(tn); err == nil && team.IsMember(actor.Email) {
			return nil
		}
	}
	return &AssetEditPermissionError{Asset: name, Teams: teamScopes}
}

// commonAssetEditPermission is the file-backed (git/path) edit gate. It reads
// the manifest and returns an *AssetEditPermissionError when actor may not
// publish a new version of the named asset.
func commonAssetEditPermission(vaultRoot string, actor mgmt.Actor, name string) error {
	m, err := loadManifest(vaultRoot)
	if err != nil {
		return err
	}
	if denial := assetEditDenial(m, name, actor); denial != nil {
		return denial
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
