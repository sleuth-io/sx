package commands

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/ui/components"
	vaultpkg "github.com/sleuth-io/sx/internal/vault"
)

// ErrAssetNotFound is returned when no asset is found in the lock file
var ErrAssetNotFound = errors.New("asset not found")

// loadVaultAndLockFile loads the vault and parses the lock file
func loadVaultAndLockFile(ctx context.Context, status *components.Status) (vaultpkg.Vault, *lockfile.LockFile, error) {
	vault, err := createVault()
	if err != nil {
		return nil, nil, err
	}

	status.Start("Syncing vault")
	lockFileContent, _, _, err := vault.GetLockFile(ctx, "")
	status.Clear()

	var lf *lockfile.LockFile
	if err != nil {
		// Lock file doesn't exist yet - create empty one
		lf = &lockfile.LockFile{Assets: []lockfile.Asset{}}
	} else {
		lf, err = lockfile.Parse(lockFileContent)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to parse lock file: %w", err)
		}
	}

	return vault, lf, nil
}

// findAssetsByName finds all assets with the given name in the lock file
func findAssetsByName(lf *lockfile.LockFile, assetName string) []*lockfile.Asset {
	var foundAssets []*lockfile.Asset
	for i := range lf.Assets {
		if lf.Assets[i].Name == assetName {
			foundAssets = append(foundAssets, &lf.Assets[i])
		}
	}
	return foundAssets
}

// selectAssetVersion prompts the user to select a version when multiple exist.
// Returns ErrAssetNotFound if no assets are found.
func selectAssetVersion(foundAssets []*lockfile.Asset, assetName string, out *outputHelper) (*lockfile.Asset, error) {
	if len(foundAssets) == 0 {
		return nil, ErrAssetNotFound
	}

	if len(foundAssets) == 1 {
		return foundAssets[0], nil
	}

	// Build options for version selection
	options := make([]components.Option, len(foundAssets))
	for i, art := range foundAssets {
		scopeDesc := "global"
		if len(art.Scopes) > 0 {
			scopeDesc = fmt.Sprintf("%d repositories", len(art.Scopes))
		}
		options[i] = components.Option{
			Label:       "v" + art.Version,
			Value:       art.Version,
			Description: "Currently installed: " + scopeDesc,
		}
	}

	out.println()
	out.printf("Multiple versions of %s found in lock file\n", assetName)
	selected, err := components.SelectWithIO("Which version would you like to configure?", options, out.cmd.InOrStdin(), out.cmd.OutOrStdout())
	if err != nil {
		return nil, fmt.Errorf("failed to select version: %w", err)
	}

	// Find the selected asset
	for _, art := range foundAssets {
		if art.Version == selected.Value {
			return art, nil
		}
	}

	// This shouldn't happen unless there's a bug in SelectWithIO
	return nil, ErrAssetNotFound
}

// handleNewAssetFromVault handles configuring an asset that exists in vault but not in lock file
func handleNewAssetFromVault(ctx context.Context, cmd *cobra.Command, out *outputHelper, status *components.Status, vault vaultpkg.Vault, assetName string, promptInstall bool, opts addOptions) error {
	status.Start("Checking for asset versions")
	versions, err := vault.GetVersionList(ctx, assetName)
	status.Clear()
	if err != nil || len(versions) == 0 {
		return fmt.Errorf("asset '%s' not found in vault", assetName)
	}

	latestVersion := versions[len(versions)-1]
	out.printf("Found asset: %s v%s in vault (not yet installed)\n", assetName, latestVersion)

	// Get scopes (from flags if non-interactive, otherwise prompt)
	var repositories []lockfile.Scope
	if opts.isNonInteractive() {
		repositories, err = opts.getScopes()
		if err != nil {
			return err
		}
	} else {
		repositories, err = promptForRepositories(out, assetName, latestVersion, nil)
		if err != nil {
			return fmt.Errorf("failed to configure repositories: %w", err)
		}
	}

	if repositories == nil {
		out.println()
		out.println("Asset available in vault only")
		return nil
	}

	newAsset := &lockfile.Asset{
		Name:    assetName,
		Type:    asset.TypeSkill,
		Version: latestVersion,
		SourcePath: &lockfile.SourcePath{
			Path: fmt.Sprintf("./assets/%s/%s", assetName, latestVersion),
		},
		Scopes: repositories,
	}

	if err := updateLockFile(ctx, out, vault, newAsset); err != nil {
		return fmt.Errorf("failed to update lock file: %w", err)
	}

	if promptInstall {
		promptRunInstall(cmd, ctx, out)
	}

	return nil
}

// handleAssetRemoval removes an asset from the lock file
func handleAssetRemoval(ctx context.Context, cmd *cobra.Command, out *outputHelper, vault vaultpkg.Vault, foundAsset *lockfile.Asset, promptInstall bool) error {
	if pathVault, ok := vault.(*vaultpkg.PathVault); ok {
		lockFilePath := pathVault.GetLockFilePath()
		if err := lockfile.RemoveAsset(lockFilePath, foundAsset.Name, foundAsset.Version); err != nil {
			return fmt.Errorf("failed to remove asset from lock file: %w", err)
		}
	} else if gitVault, ok := vault.(*vaultpkg.GitVault); ok {
		lockFilePath := gitVault.GetLockFilePath()
		if err := lockfile.RemoveAsset(lockFilePath, foundAsset.Name, foundAsset.Version); err != nil {
			return fmt.Errorf("failed to remove asset from lock file: %w", err)
		}
		if err := gitVault.CommitAndPush(ctx, foundAsset); err != nil {
			return fmt.Errorf("failed to push removal: %w", err)
		}
	}

	if promptInstall {
		out.println()
		confirmed, err := components.ConfirmWithIO("Run install now to remove the asset from clients?", true, cmd.InOrStdin(), cmd.OutOrStdout())
		if err != nil {
			return nil
		}

		if confirmed {
			out.println()
			if err := runInstall(cmd, nil, false, "", false, ""); err != nil {
				out.printfErr("Install failed: %v\n", err)
			}
		} else {
			out.println("Run 'sx install' when ready to clean up.")
		}
	}

	return nil
}
