package vault

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"

	"github.com/sleuth-io/sx/internal/manifest"
	"github.com/sleuth-io/sx/internal/mgmt"
	"github.com/sleuth-io/sx/internal/utils"
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

// ---- Shared extension state (API 1.5.0) ----
// One JSON document per extension, shared by everyone in the library:
// .sx/app-plugins/<id>.json. Rides the vault like everything else, so
// git history is the change log and sync is the distribution. Writes
// are last-writer-wins whole-document replaces — this is for rota
// state and shared settings, not a concurrent database.

// AppPluginSharedStore is implemented by vaults that can hold shared
// extension state.
type AppPluginSharedStore interface {
	// AppPluginSharedLoad returns the extension's shared JSON document
	// ("" when none exists).
	AppPluginSharedLoad(ctx context.Context, pluginID string) (string, error)

	// AppPluginSharedSave replaces the document. Empty data deletes it.
	AppPluginSharedSave(ctx context.Context, pluginID, data string) error
}

// ErrSharedStorageUnsupported is returned when the vault backend
// cannot store shared extension data (a server predating the surface).
var ErrSharedStorageUnsupported = errors.New(
	"this library's server doesn't support shared extension data yet")

// maxAppPluginSharedBytes bounds one extension's shared document.
const maxAppPluginSharedBytes = 256 << 10

var appPluginIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,63}$`)

func appPluginSharedPath(vaultRoot, pluginID string) (string, error) {
	if !appPluginIDPattern.MatchString(pluginID) {
		return "", fmt.Errorf("invalid extension id %q", pluginID)
	}
	return filepath.Join(vaultRoot, ".sx", "app-plugins", pluginID+".json"), nil
}

func commonAppPluginSharedLoad(vaultRoot, pluginID string) (string, error) {
	path, err := appPluginSharedPath(vaultRoot, pluginID)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path) // #nosec G304 -- path is under the vault root with a validated id
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// validateAppPluginSharedDoc is the one bounded-valid-JSON contract for
// shared documents, enforced identically by every vault backend before
// anything is written (or leaves the machine).
func validateAppPluginSharedDoc(data string) error {
	if len(data) > maxAppPluginSharedBytes {
		return fmt.Errorf("shared extension data exceeds %d bytes", maxAppPluginSharedBytes)
	}
	if !json.Valid([]byte(data)) {
		return errors.New("shared extension data must be valid JSON")
	}
	return nil
}

func commonAppPluginSharedSave(vaultRoot, pluginID, data string) error {
	path, err := appPluginSharedPath(vaultRoot, pluginID)
	if err != nil {
		return err
	}
	if data == "" {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := validateAppPluginSharedDoc(data); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	// Atomic write: path-vault readers take no lock on Load, and a synced
	// folder can replicate mid-write — neither may ever observe a
	// truncated document.
	return utils.WriteFileAtomic(path, []byte(data), 0o644)
}

// AppPluginSharedLoad reads the extension's shared document.
func (p *PathVault) AppPluginSharedLoad(ctx context.Context, pluginID string) (string, error) {
	return commonAppPluginSharedLoad(p.repoPath, pluginID)
}

// AppPluginSharedSave replaces the extension's shared document.
func (p *PathVault) AppPluginSharedSave(ctx context.Context, pluginID, data string) error {
	return p.withLock(ctx, func(actor mgmt.Actor) error {
		return commonAppPluginSharedSave(p.repoPath, pluginID, data)
	})
}

// AppPluginSharedLoad reads the extension's shared document from the
// synced clone.
func (g *GitVault) AppPluginSharedLoad(ctx context.Context, pluginID string) (string, error) {
	if err := g.cloneOrUpdate(ctx); err != nil {
		return "", err
	}
	return commonAppPluginSharedLoad(g.repoPath, pluginID)
}

// AppPluginSharedSave replaces the extension's shared document and
// pushes (the file lives under .sx/, which the vault tx stages).
func (g *GitVault) AppPluginSharedSave(ctx context.Context, pluginID, data string) error {
	return g.runInVaultTx(ctx, "Update "+pluginID+" extension shared data", func(root string, actor mgmt.Actor) error {
		return commonAppPluginSharedSave(root, pluginID, data)
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
