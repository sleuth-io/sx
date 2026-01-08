package commands

import (
	"context"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/clients"
	"github.com/sleuth-io/sx/internal/config"
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/ui"
	"github.com/sleuth-io/sx/internal/ui/components"
	vaultpkg "github.com/sleuth-io/sx/internal/vault"
)

// importableAsset holds an asset and its source client for import
type importableAsset struct {
	client clients.Client
	asset  clients.InstalledAsset
}

// promptImportAssets detects existing assets in clients and offers to import them to vault
func promptImportAssets(cmd *cobra.Command, ctx context.Context, enabledClients []string) {
	styledOut := ui.NewOutput(cmd.OutOrStdout(), cmd.ErrOrStderr())
	status := components.NewStatus(cmd.OutOrStdout())

	// Load config to get vault
	cfg, err := config.Load()
	if err != nil {
		return
	}

	vault, err := vaultpkg.NewFromConfig(cfg)
	if err != nil {
		return
	}

	// Get managed asset names from lock file
	status.Start("Checking for existing assets")
	managedNames := make(map[string]bool)
	lockFileContent, _, _, err := vault.GetLockFile(ctx, "")
	if err == nil {
		lf, err := lockfile.Parse(lockFileContent)
		if err == nil {
			for _, a := range lf.Assets {
				managedNames[a.Name] = true
			}
		}
	}

	// Scan all enabled clients for installed assets
	clientRegistry := clients.Global()
	var unmanagedAssets []importableAsset
	globalScope := &clients.InstallScope{Type: clients.ScopeGlobal}

	for _, clientID := range enabledClients {
		client, err := clientRegistry.Get(clientID)
		if err != nil {
			continue
		}

		installed, err := client.ScanInstalledAssets(ctx, globalScope)
		if err != nil {
			continue
		}

		for _, a := range installed {
			if !managedNames[a.Name] {
				unmanagedAssets = append(unmanagedAssets, importableAsset{
					client: client,
					asset:  a,
				})
			}
		}
	}
	status.Clear()

	if len(unmanagedAssets) == 0 {
		return
	}

	// Build multi-select options
	options := make([]components.MultiSelectOption, len(unmanagedAssets))
	for i, item := range unmanagedAssets {
		label := item.asset.Name
		if item.asset.Type.Label != "" {
			label = fmt.Sprintf("%s (%s)", item.asset.Name, item.asset.Type.Label)
		}
		options[i] = components.MultiSelectOption{
			Label:    label,
			Value:    strconv.Itoa(i),
			Selected: true, // Default to selecting all
		}
	}

	styledOut.Newline()
	selected, err := components.MultiSelect("Import existing assets to vault?", options)
	if err != nil {
		return
	}

	// Count selected
	var selectedCount int
	for _, opt := range selected {
		if opt.Selected {
			selectedCount++
		}
	}

	if selectedCount == 0 {
		return
	}

	// Import selected assets
	styledOut.Newline()
	for i, opt := range selected {
		if !opt.Selected {
			continue
		}

		item := unmanagedAssets[i]

		// Get the asset path from the client
		assetPath, err := item.client.GetAssetPath(ctx, item.asset.Name, item.asset.Type, globalScope)
		if err != nil {
			styledOut.Error(fmt.Sprintf("Failed to get path for %s: %v", item.asset.Name, err))
			continue
		}

		// Use the add command to import the asset and prompt for scope
		if err := runAdd(cmd, assetPath); err != nil {
			styledOut.Error(fmt.Sprintf("Failed to import %s: %v", item.asset.Name, err))
			continue
		}

		// Check if user chose a non-global scope - if so, clean up the global copy
		cleanupGlobalCopyIfNeeded(ctx, vault, item.client, item.asset, styledOut)
	}
}

// cleanupGlobalCopyIfNeeded removes the global copy of an asset if user chose a different scope
func cleanupGlobalCopyIfNeeded(ctx context.Context, vault vaultpkg.Vault, client clients.Client, importedAsset clients.InstalledAsset, out *ui.Output) {
	// Re-read lock file to see what scope was chosen
	lockFileContent, _, _, err := vault.GetLockFile(ctx, "")
	if err != nil {
		return
	}

	lf, err := lockfile.Parse(lockFileContent)
	if err != nil {
		return
	}

	// Find the asset and check if it's global
	for i := range lf.Assets {
		if lf.Assets[i].Name == importedAsset.Name {
			// If not global, remove the original global copy via the client
			if !lf.Assets[i].IsGlobal() {
				globalScope := &clients.InstallScope{Type: clients.ScopeGlobal}
				uninstallReq := clients.UninstallRequest{
					Assets: []asset.Asset{{
						Name: importedAsset.Name,
						Type: importedAsset.Type,
					}},
					Scope: globalScope,
				}

				resp, err := client.UninstallAssets(ctx, uninstallReq)
				if err != nil {
					out.Warning(fmt.Sprintf("Could not remove global copy of %s: %v", importedAsset.Name, err))
					return
				}

				for _, result := range resp.Results {
					if result.Status == clients.StatusSuccess {
						out.Success("Removed global copy of " + result.AssetName)
					}
				}
			}
			return
		}
	}
}
