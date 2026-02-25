package commands

import (
	"context"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/assets"
	"github.com/sleuth-io/sx/internal/clients"
	"github.com/sleuth-io/sx/internal/logger"
	"github.com/sleuth-io/sx/internal/ui"
)

// separateGlobalAndScopedAssets separates installed assets into global and repository-scoped
func separateGlobalAndScopedAssets(installedAssets []assets.InstalledAsset) (global, scoped []assets.InstalledAsset) {
	for _, installed := range installedAssets {
		if installed.Repository == "" && installed.Path == "" {
			global = append(global, installed)
		} else {
			scoped = append(scoped, installed)
		}
	}
	return global, scoped
}

// uninstallAssetsWithScope uninstalls a list of assets from all clients using the given scope
func uninstallAssetsWithScope(ctx context.Context, installedAssets []assets.InstalledAsset, scope *clients.InstallScope, targetClients []clients.Client, styledOut *ui.Output) {
	// Convert InstalledAsset to asset.Asset
	assetsToRemove := make([]asset.Asset, len(installedAssets))
	for i, installed := range installedAssets {
		assetsToRemove[i] = asset.Asset{
			Name:    installed.Name,
			Version: installed.Version,
			Type:    asset.FromString(installed.Type),
			Config:  installed.Config,
		}
	}

	uninstallReq := clients.UninstallRequest{
		Assets:  assetsToRemove,
		Scope:   scope,
		Options: clients.UninstallOptions{},
	}

	log := logger.Get()
	for _, client := range targetClients {
		resp, err := client.UninstallAssets(ctx, uninstallReq)
		if err != nil {
			styledOut.Warning("Cleanup failed for " + client.DisplayName() + ": " + err.Error())
			log.Error("cleanup failed", "client", client.ID(), "error", err)
			continue
		}

		for _, result := range resp.Results {
			switch result.Status {
			case clients.StatusSuccess:
				styledOut.ListItem("-", "Removed "+result.AssetName+" from "+client.DisplayName())
				log.Info("asset removed", "name", result.AssetName, "client", client.ID())
			case clients.StatusFailed:
				styledOut.Warning("Failed to remove " + result.AssetName + " from " + client.DisplayName() + ": " + result.Error.Error())
				log.Error("asset removal failed", "name", result.AssetName, "client", client.ID(), "error", result.Error)
			case clients.StatusSkipped:
				// Skipped assets don't need cleanup logging
			}
		}
	}
}
