package vault

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sleuth-io/sx/internal/vault/layout"
)

// Asset retirement — the recoverable removal consolidation needs
// (docs/skill-dedupe-spec.md). RemoveAsset's two modes don't fit:
// delete=false leaves the root view in place so the asset still lists
// (ListAssets scans the assets/ directory), and delete=true destroys
// the version archive too, making the removal permanent. RetireAsset
// removes the manifest entry AND the browsable root copy while keeping
// .sx/versions/<name>/ — gone from the library, recoverable from the
// archive. File vaults only; skills.new vaults get the same semantics
// from the server's non-delete removal.

// retireAssetFiles drops the root view and keeps the version archive.
// Runs on a migrated (v2) vault, where the archive is separate storage.
func retireAssetFiles(vaultRoot, assetName string) error {
	l, err := detectLayout(vaultRoot)
	if err != nil {
		return err
	}
	if l.Version() != layout.V2 {
		// v1 keeps versions inside the asset dir — deleting it would
		// destroy the archive. Migration runs before this on both
		// vault types, so this is a defensive stop, not a real path.
		return errors.New("retire requires a migrated vault")
	}
	return os.RemoveAll(filepath.Join(vaultRoot, l.AssetDir(assetName)))
}

// RetireAsset removes the asset from the manifest and root view,
// keeping its version archive.
func (p *PathVault) RetireAsset(ctx context.Context, assetName string) error {
	if err := p.ensureMigrated(ctx); err != nil {
		return err
	}
	removed, err := removeAssetFromManifest(p.repoPath, assetName, "")
	if err != nil {
		return err
	}
	if removed == 0 {
		return ErrAssetNotFound
	}
	return retireAssetFiles(p.repoPath, assetName)
}

// RetireAsset removes the asset from the manifest and root view,
// keeping its version archive, and pushes the change.
func (g *GitVault) RetireAsset(ctx context.Context, assetName string) error {
	fileLock, err := g.acquireFileLock(ctx)
	if err != nil {
		return fmt.Errorf("failed to acquire lock: %w", err)
	}
	defer func() { _ = fileLock.Unlock() }()

	if err := g.cloneOrUpdate(ctx); err != nil {
		return fmt.Errorf("failed to clone/update repository: %w", err)
	}
	if err := g.ensureMigratedLocked(ctx); err != nil {
		return err
	}

	removed, err := removeAssetFromManifest(g.repoPath, assetName, "")
	if err != nil {
		return fmt.Errorf("failed to remove asset from manifest: %w", err)
	}
	if removed == 0 {
		return ErrAssetNotFound
	}
	if err := retireAssetFiles(g.repoPath, assetName); err != nil {
		return err
	}

	if err := g.gitClient.Add(ctx, g.repoPath, "."); err != nil {
		return fmt.Errorf("failed to stage changes: %w", err)
	}
	hasChanges, err := g.gitClient.HasStagedChanges(ctx, g.repoPath)
	if err != nil {
		return err
	}
	if !hasChanges {
		return nil
	}
	if err := g.gitClient.Commit(ctx, g.repoPath, "Retire "+assetName); err != nil {
		return fmt.Errorf("failed to commit retirement: %w", err)
	}
	if err := g.gitClient.Push(ctx, g.repoPath); err != nil {
		return fmt.Errorf("failed to push retirement: %w", err)
	}
	return nil
}
