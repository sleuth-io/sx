package vault

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"

	"github.com/sleuth-io/sx/internal/manifest"
	"github.com/sleuth-io/sx/internal/mgmt"
)

// Desktop-app extension policy (docs/app-plugins-spec.md): stored in the
// manifest's [app-plugins] table on file-backed vaults, org-admin gated,
// audited. The app reads it at boot and refuses to enable anything the
// policy blocks; skills.new parity is the spec's P5.

// AppPluginPolicyStore is implemented by vaults that carry the policy.
type AppPluginPolicyStore interface {
	// AppPluginPolicy returns the current policy. A nil policy means
	// "open" — no restrictions.
	AppPluginPolicy(ctx context.Context) (*manifest.AppPluginPolicy, error)

	// SetAppPluginPolicy replaces the policy. Only org-admins may call
	// this on a governed vault; on an ungoverned vault anyone can (the
	// same rule as every other governance surface, see docs/rbac.md).
	// A nil policy clears the table back to open.
	SetAppPluginPolicy(ctx context.Context, policy *manifest.AppPluginPolicy) error
}

const (
	AppPluginModeOpen      = "open"
	AppPluginModeAllowlist = "allowlist"
	AppPluginModeDisabled  = "disabled"
)

func validateAppPluginPolicy(p *manifest.AppPluginPolicy) error {
	if p == nil {
		return nil
	}
	switch p.Mode {
	case AppPluginModeOpen, AppPluginModeAllowlist, AppPluginModeDisabled:
		return nil
	}
	return fmt.Errorf("invalid app-plugins mode %q (open|allowlist|disabled)", p.Mode)
}

func commonAppPluginPolicy(vaultRoot string) (*manifest.AppPluginPolicy, error) {
	m, err := loadManifest(vaultRoot)
	if err != nil || m == nil {
		return nil, err
	}
	return m.AppPlugins, nil
}

func commonSetAppPluginPolicy(vaultRoot string, actor mgmt.Actor, policy *manifest.AppPluginPolicy) error {
	if err := validateAppPluginPolicy(policy); err != nil {
		return err
	}
	if policy != nil {
		sort.Strings(policy.Allowed)
		policy.Allowed = slices.Compact(policy.Allowed)
	}
	return withManifest(vaultRoot, actor, func(m *manifest.Manifest) (*mgmt.AuditEvent, error) {
		// Same governance rule as org-admin changes: a governed vault
		// restricts policy writes to org-admins; ungoverned vaults are
		// open by definition.
		if m.HasOrgAdmins() && !m.IsOrgAdmin(actor.Email) {
			return nil, errors.New("only org-admins can change the extension policy")
		}
		m.AppPlugins = policy
		data := map[string]any{"mode": AppPluginModeOpen}
		if policy != nil {
			data["mode"] = policy.Mode
			if len(policy.Allowed) > 0 {
				data["allowed"] = policy.Allowed
			}
		}
		return &mgmt.AuditEvent{
			Event:      mgmt.EventPluginPolicyChanged,
			TargetType: mgmt.TargetTypePlugin,
			Target:     "policy",
			Data:       data,
		}, nil
	})
}

// AppPluginPolicy returns the vault's extension policy.
func (p *PathVault) AppPluginPolicy(ctx context.Context) (*manifest.AppPluginPolicy, error) {
	return commonAppPluginPolicy(p.repoPath)
}

// SetAppPluginPolicy replaces the vault's extension policy.
func (p *PathVault) SetAppPluginPolicy(ctx context.Context, policy *manifest.AppPluginPolicy) error {
	return p.withLock(ctx, func(actor mgmt.Actor) error {
		return commonSetAppPluginPolicy(p.repoPath, actor, policy)
	})
}

// AppPluginPolicy returns the vault's extension policy.
func (g *GitVault) AppPluginPolicy(ctx context.Context) (*manifest.AppPluginPolicy, error) {
	if err := g.cloneOrUpdate(ctx); err != nil {
		return nil, err
	}
	return commonAppPluginPolicy(g.repoPath)
}

// SetAppPluginPolicy replaces the vault's extension policy and pushes.
func (g *GitVault) SetAppPluginPolicy(ctx context.Context, policy *manifest.AppPluginPolicy) error {
	return g.runInVaultTx(ctx, "Set extension policy", func(root string, actor mgmt.Actor) error {
		return commonSetAppPluginPolicy(root, actor, policy)
	})
}
