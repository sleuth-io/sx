package commands

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/sleuth-io/sx/internal/assets"
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/logger"
	"github.com/sleuth-io/sx/internal/scope"
	"github.com/sleuth-io/sx/internal/ui"
	"github.com/sleuth-io/sx/internal/ui/components"
	vaultpkg "github.com/sleuth-io/sx/internal/vault"
)

// downloadAssetsResult holds the result of downloading assets
type downloadAssetsResult struct {
	Downloads []*assets.AssetWithMetadata
	Errors    []error
}

// downloadAssetsWithStatus downloads assets and returns successful downloads
func downloadAssetsWithStatus(
	ctx context.Context,
	vault vaultpkg.Vault,
	assetsToInstall []*lockfile.Asset,
	status *components.Status,
	styledOut *ui.Output,
) (*downloadAssetsResult, error) {
	status.Start(fmt.Sprintf("Downloading %d assets", len(assetsToInstall)))

	fetcher := assets.NewAssetFetcher(vault)
	results, err := fetcher.FetchAssets(ctx, assetsToInstall, 10)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch assets: %w", err)
	}

	result := processDownloadResults(results, styledOut)
	status.Clear()

	if len(result.Downloads) == 0 {
		styledOut.Error("No assets downloaded successfully")
		return nil, errors.New("no assets downloaded successfully")
	}

	return result, nil
}

// processDownloadResults separates successful downloads from errors
func processDownloadResults(results []assets.DownloadResult, styledOut *ui.Output) *downloadAssetsResult {
	var downloadErrors []error
	var successfulDownloads []*assets.AssetWithMetadata

	for _, result := range results {
		if result.Error != nil {
			downloadErrors = append(downloadErrors, fmt.Errorf("%s: %w", result.Asset.Name, result.Error))
		} else {
			successfulDownloads = append(successfulDownloads, &assets.AssetWithMetadata{
				Asset:    result.Asset,
				Metadata: result.Metadata,
				ZipData:  result.ZipData,
			})
		}
	}

	// Log download errors
	if len(downloadErrors) > 0 {
		log := logger.Get()
		for _, err := range downloadErrors {
			styledOut.ErrorItem(err.Error())
			log.Error("asset download failed", "error", err)
		}
	}

	return &downloadAssetsResult{
		Downloads: successfulDownloads,
		Errors:    downloadErrors,
	}
}

// reportInstallResults reports the results of installation to the user
func reportInstallResults(
	installResult *assets.InstallResult,
	downloads []*assets.AssetWithMetadata,
	currentScope *scope.Scope,
	styledOut *ui.Output,
) error {
	log := logger.Get()

	if len(installResult.Installed) > 0 {
		styledOut.Success(fmt.Sprintf("Installed %d assets", len(installResult.Installed)))
		for _, name := range installResult.Installed {
			styledOut.SuccessItem(name)
			logInstalledAsset(name, downloads, currentScope, log)
		}
	}

	if len(installResult.Failed) > 0 {
		styledOut.Error(fmt.Sprintf("Failed to install %d assets", len(installResult.Failed)))
		for i, name := range installResult.Failed {
			styledOut.ErrorItem(fmt.Sprintf("%s: %v", name, installResult.Errors[i]))
			log.Error("asset installation failed", "name", name, "error", installResult.Errors[i])
		}
		return errors.New("some assets failed to install")
	}

	return nil
}

// logInstalledAsset logs details about an installed asset
func logInstalledAsset(name string, downloads []*assets.AssetWithMetadata, currentScope *scope.Scope, log *slog.Logger) {
	for _, art := range downloads {
		if art.Asset.Name == name {
			log.Info("asset installed",
				"name", name,
				"version", art.Asset.Version,
				"type", art.Metadata.Asset.Type,
				"scope", currentScope.Type)
			break
		}
	}
}
