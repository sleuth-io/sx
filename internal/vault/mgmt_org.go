package vault

import (
	"context"
	"errors"
	"fmt"

	"github.com/sleuth-io/sx/internal/manifest"
	"github.com/sleuth-io/sx/internal/mgmt"
)

// commonAddOrgAdmins adds emails to the org-admin list. While the list is empty
// anyone may seed it (bootstrap); once set, only an org-admin may change it.
func commonAddOrgAdmins(vaultRoot string, actor mgmt.Actor, emails []string) (int, error) {
	added := 0
	err := withManifest(vaultRoot, actor, func(m *manifest.Manifest) (*mgmt.AuditEvent, error) {
		if m.HasOrgAdmins() && !m.IsOrgAdmin(actor.Email) {
			return nil, errors.New("permission denied: only an org-admin may change the org-admin list")
		}
		added = m.AddOrgAdmins(emails...)
		if added == 0 {
			return nil, nil //nolint:nilnil // no-op: nothing added, no event, no error
		}
		return &mgmt.AuditEvent{
			Event: mgmt.EventOrgAdminAdded, TargetType: mgmt.TargetTypeOrg, Target: "org",
			Data: map[string]any{"added": emails},
		}, nil
	})
	return added, err
}

// commonRemoveOrgAdmin removes an email from the org-admin list (org-admins only).
func commonRemoveOrgAdmin(vaultRoot string, actor mgmt.Actor, email string) error {
	return withManifest(vaultRoot, actor, func(m *manifest.Manifest) (*mgmt.AuditEvent, error) {
		if !m.IsOrgAdmin(actor.Email) {
			return nil, errors.New("permission denied: only an org-admin may change the org-admin list")
		}
		if !m.RemoveOrgAdmin(email) {
			return nil, fmt.Errorf("%q is not an org-admin", email)
		}
		return &mgmt.AuditEvent{
			Event: mgmt.EventOrgAdminRemoved, TargetType: mgmt.TargetTypeOrg, Target: "org",
			Data: map[string]any{"removed": manifest.NormalizeEmail(email)},
		}, nil
	})
}

// commonListOrgAdmins returns the configured org-admin emails.
func commonListOrgAdmins(vaultRoot string) ([]string, error) {
	m, err := loadManifest(vaultRoot)
	if err != nil {
		return nil, err
	}
	return m.OrgAdmins(), nil
}

// ---- PathVault org methods ----

func (p *PathVault) AddOrgAdmins(ctx context.Context, emails []string) (added int, err error) {
	err = p.withLock(ctx, func(actor mgmt.Actor) error {
		added, err = commonAddOrgAdmins(p.repoPath, actor, emails)
		return err
	})
	return added, err
}

func (p *PathVault) RemoveOrgAdmin(ctx context.Context, email string) error {
	return p.withLock(ctx, func(actor mgmt.Actor) error {
		return commonRemoveOrgAdmin(p.repoPath, actor, email)
	})
}

func (p *PathVault) ListOrgAdmins(ctx context.Context) ([]string, error) {
	return commonListOrgAdmins(p.repoPath)
}

// ---- GitVault org methods ----

func (g *GitVault) AddOrgAdmins(ctx context.Context, emails []string) (added int, err error) {
	err = g.runInVaultTx(ctx, "Add org-admins", func(root string, actor mgmt.Actor) error {
		added, err = commonAddOrgAdmins(root, actor, emails)
		return err
	})
	return added, err
}

func (g *GitVault) RemoveOrgAdmin(ctx context.Context, email string) error {
	return g.runInVaultTx(ctx, "Remove org-admin", func(root string, actor mgmt.Actor) error {
		return commonRemoveOrgAdmin(root, actor, email)
	})
}

func (g *GitVault) ListOrgAdmins(ctx context.Context) ([]string, error) {
	if err := g.cloneOrUpdate(ctx); err != nil {
		return nil, err
	}
	return commonListOrgAdmins(g.repoPath)
}
