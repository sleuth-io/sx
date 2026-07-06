package vault

import (
	"context"
	"os"
	"path/filepath"

	"github.com/sleuth-io/sx/internal/mgmt"
)

// The vault icon lives at .sx/vault-icon — raw image bytes, mime sniffed
// by consumers. It is shared vault data: one user sets it and everyone
// using the vault sees it (for git vaults it syncs like any other vault
// write). Sleuth vaults don't implement this; their icon is the
// organization's.
//
// The name deliberately avoids "icon" as the basename: the ubiquitous
// macOS `Icon`/`Icon?` global-gitignore rule matches it case-insensitively
// and would silently keep the file out of every commit.

const vaultIconRel = ".sx/vault-icon"

// VaultIconStore is implemented by file-backed vaults (git, path) whose
// icon is stored in the vault itself.
type VaultIconStore interface {
	GetVaultIcon(ctx context.Context) ([]byte, error)
	// SetVaultIcon stores the icon; empty data removes it.
	SetVaultIcon(ctx context.Context, data []byte) error
}

func vaultIconPath(vaultRoot string) string {
	return filepath.Join(vaultRoot, filepath.FromSlash(vaultIconRel))
}

func readVaultIcon(vaultRoot string) ([]byte, error) {
	data, err := os.ReadFile(vaultIconPath(vaultRoot))
	if os.IsNotExist(err) {
		return nil, nil
	}
	return data, err
}

func writeVaultIcon(vaultRoot string, data []byte) error {
	p := vaultIconPath(vaultRoot)
	if len(data) == 0 {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return err
	}
	return os.WriteFile(p, data, 0644)
}

// GetVaultIcon reads the shared icon, nil when none is set.
func (p *PathVault) GetVaultIcon(ctx context.Context) ([]byte, error) {
	fl, err := p.acquirePathReadLock(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = fl.Unlock() }()
	return readVaultIcon(p.repoPath)
}

// SetVaultIcon stores (or with empty data removes) the shared icon.
func (p *PathVault) SetVaultIcon(ctx context.Context, data []byte) error {
	fl, err := p.acquirePathLock(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = fl.Unlock() }()
	return writeVaultIcon(p.repoPath, data)
}

// GetVaultIcon reads the shared icon from the working clone. It never
// pulls: icon freshness rides along with every other vault operation's
// sync, and a library that has never been used yet simply has no icon to
// show until it is.
func (g *GitVault) GetVaultIcon(ctx context.Context) ([]byte, error) {
	if _, err := os.Stat(g.repoPath); os.IsNotExist(err) {
		return nil, nil
	}
	return readVaultIcon(g.repoPath)
}

// SetVaultIcon stores (or with empty data removes) the shared icon, as its
// own commit pushed to the vault repository.
func (g *GitVault) SetVaultIcon(ctx context.Context, data []byte) error {
	msg := "Set library icon"
	if len(data) == 0 {
		msg = "Remove library icon"
	}
	return g.runInVaultTx(ctx, msg, func(vaultRoot string, _ mgmt.Actor) error {
		return writeVaultIcon(vaultRoot, data)
	})
}
