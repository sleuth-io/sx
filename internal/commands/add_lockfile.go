package commands

import (
	"context"
	"fmt"

	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/vault"
)

// updateLockFile updates the repository's lock file with the asset using modern UI
func updateLockFile(ctx context.Context, out *outputHelper, repo vault.Vault, asset *lockfile.Asset) error {
	// SetInstallations updates the vault's lock file with the installation configuration
	// The user was already shown their choice in the prompt, so we don't need to show it again
	if err := repo.SetInstallations(ctx, asset); err != nil {
		return fmt.Errorf("failed to set installations: %w", err)
	}

	return nil
}
