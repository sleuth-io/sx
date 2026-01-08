package assets

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/schollz/progressbar/v3"

	"github.com/sleuth-io/sx/internal/cache"
	"github.com/sleuth-io/sx/internal/config"
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/metadata"
	"github.com/sleuth-io/sx/internal/utils"
	vaultpkg "github.com/sleuth-io/sx/internal/vault"
)

// AssetFetcher handles fetching assets from a vault
type AssetFetcher struct {
	vault vaultpkg.Vault
}

// NewAssetFetcher creates a new asset fetcher
func NewAssetFetcher(vault vaultpkg.Vault) *AssetFetcher {
	return &AssetFetcher{
		vault: vault,
	}
}

// FetchAsset downloads a single asset
func (f *AssetFetcher) FetchAsset(ctx context.Context, asset *lockfile.Asset) (zipData []byte, meta *metadata.Metadata, err error) {
	// Try disk cache first
	zipData, err = cache.LoadAssetFromDisk(asset.Name, asset.Version)
	if err == nil {
		// Cache hit, extract metadata and return
		metadataBytes, err := utils.ReadZipFile(zipData, "metadata.toml")
		if err == nil {
			meta, err = metadata.Parse(metadataBytes)
			if err == nil && meta.Validate() == nil {
				// Valid cached asset
				return zipData, meta, nil
			}
		}
		// Cache corrupted, fall through to download
	}

	// Cache miss or invalid, download asset
	zipData, err = f.vault.GetAsset(ctx, asset)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to download asset: %w", err)
	}

	// Verify it's a valid zip
	if !utils.IsZipFile(zipData) {
		return nil, nil, errors.New("downloaded file is not a valid zip archive")
	}

	// Extract and parse metadata from zip
	metadataBytes, err := utils.ReadZipFile(zipData, "metadata.toml")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read metadata.toml from zip: %w", err)
	}

	meta, err = metadata.Parse(metadataBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse metadata: %w", err)
	}

	// Validate metadata
	if err := meta.Validate(); err != nil {
		return nil, nil, fmt.Errorf("metadata validation failed: %w", err)
	}

	// Cache to disk for future use
	_ = cache.SaveAssetToDisk(asset.Name, asset.Version, zipData)
	// Ignore cache save errors - not critical

	return zipData, meta, nil
}

// FetchAssetWithProgress downloads a single asset with progress bar
func (f *AssetFetcher) FetchAssetWithProgress(ctx context.Context, asset *lockfile.Asset, bar *progressbar.ProgressBar) (zipData []byte, meta *metadata.Metadata, err error) {
	// Try disk cache first
	zipData, err = cache.LoadAssetFromDisk(asset.Name, asset.Version)
	if err == nil {
		// Cache hit, extract metadata and return
		metadataBytes, err := utils.ReadZipFile(zipData, "metadata.toml")
		if err == nil {
			meta, err = metadata.Parse(metadataBytes)
			if err == nil && meta.Validate() == nil {
				// Valid cached asset - complete progress bar immediately
				if bar != nil {
					bar.ChangeMax64(int64(len(zipData)))
					_ = bar.Set64(int64(len(zipData)))
				}
				return zipData, meta, nil
			}
		}
		// Cache corrupted, fall through to download
	}

	// Download asset through vault (handles auth properly)
	zipData, err = f.vault.GetAsset(ctx, asset)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to download asset: %w", err)
	}

	// Update progress bar to 100% after download
	if bar != nil {
		bar.ChangeMax64(int64(len(zipData)))
		_ = bar.Set64(int64(len(zipData)))
	}

	// Verify it's a valid zip
	if !utils.IsZipFile(zipData) {
		return nil, nil, errors.New("downloaded file is not a valid zip archive")
	}

	// Extract and parse metadata from zip
	metadataBytes, err := utils.ReadZipFile(zipData, "metadata.toml")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read metadata.toml from zip: %w", err)
	}

	meta, err = metadata.Parse(metadataBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse metadata: %w", err)
	}

	// Validate metadata
	if err := meta.Validate(); err != nil {
		return nil, nil, fmt.Errorf("metadata validation failed: %w", err)
	}

	// Cache to disk for future use
	_ = cache.SaveAssetToDisk(asset.Name, asset.Version, zipData)
	// Ignore cache save errors - not critical

	return zipData, meta, nil
}

// FetchAssets downloads multiple assets in parallel
func (f *AssetFetcher) FetchAssets(ctx context.Context, assets []*lockfile.Asset, concurrency int) ([]DownloadResult, error) {
	if concurrency <= 0 {
		concurrency = 10 // Default
	}

	results := make([]DownloadResult, len(assets))
	tasks := make(chan DownloadTask, len(assets))
	resultChan := make(chan DownloadResult, len(assets))

	// Create worker pool
	var wg sync.WaitGroup
	for range concurrency {
		wg.Go(func() {
			for task := range tasks {
				select {
				case <-ctx.Done():
					resultChan <- DownloadResult{
						Asset: task.Asset,
						Error: ctx.Err(),
						Index: task.Index,
					}
					return
				default:
				}

				// Create progress bar for this asset if not in silent mode
				var bar *progressbar.ProgressBar
				if !config.IsSilent() {
					bar = progressbar.NewOptions64(
						-1, // Unknown size initially
						progressbar.OptionSetDescription(fmt.Sprintf("[%d/%d] %s", task.Index+1, len(assets), task.Asset.Name)),
						progressbar.OptionSetWidth(30),
						progressbar.OptionShowBytes(true),
						progressbar.OptionSetPredictTime(true),
						progressbar.OptionClearOnFinish(),
					)
				}

				zipData, meta, err := f.FetchAssetWithProgress(ctx, task.Asset, bar)

				if bar != nil {
					_ = bar.Finish()
				}

				resultChan <- DownloadResult{
					Asset:    task.Asset,
					ZipData:  zipData,
					Metadata: meta,
					Error:    err,
					Index:    task.Index,
				}
			}
		})
	}

	// Send tasks
	go func() {
		for i, asset := range assets {
			tasks <- DownloadTask{
				Asset: asset,
				Index: i,
			}
		}
		close(tasks)
	}()

	// Collect results
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	for result := range resultChan {
		results[result.Index] = result
	}

	return results, nil
}
