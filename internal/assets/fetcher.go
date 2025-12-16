package assets

import (
	"context"
	"fmt"
	"sync"

	"github.com/schollz/progressbar/v3"
	"github.com/sleuth-io/skills/internal/cache"
	"github.com/sleuth-io/skills/internal/config"
	"github.com/sleuth-io/skills/internal/lockfile"
	"github.com/sleuth-io/skills/internal/metadata"
	"github.com/sleuth-io/skills/internal/utils"
	vaultpkg "github.com/sleuth-io/skills/internal/vault"
)

// ArtifactFetcher handles fetching artifacts from a vault
type ArtifactFetcher struct {
	vault vaultpkg.Vault
}

// NewArtifactFetcher creates a new artifact fetcher
func NewArtifactFetcher(vault vaultpkg.Vault) *ArtifactFetcher {
	return &ArtifactFetcher{
		vault: vault,
	}
}

// FetchArtifact downloads a single artifact
func (f *ArtifactFetcher) FetchArtifact(ctx context.Context, artifact *lockfile.Artifact) (zipData []byte, meta *metadata.Metadata, err error) {
	// Try disk cache first
	zipData, err = cache.LoadArtifactFromDisk(artifact.Name, artifact.Version)
	if err == nil {
		// Cache hit, extract metadata and return
		metadataBytes, err := utils.ReadZipFile(zipData, "metadata.toml")
		if err == nil {
			meta, err = metadata.Parse(metadataBytes)
			if err == nil && meta.Validate() == nil {
				// Valid cached artifact
				return zipData, meta, nil
			}
		}
		// Cache corrupted, fall through to download
	}

	// Cache miss or invalid, download artifact
	zipData, err = f.vault.GetArtifact(ctx, artifact)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to download artifact: %w", err)
	}

	// Verify it's a valid zip
	if !utils.IsZipFile(zipData) {
		return nil, nil, fmt.Errorf("downloaded file is not a valid zip archive")
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
	_ = cache.SaveArtifactToDisk(artifact.Name, artifact.Version, zipData)
	// Ignore cache save errors - not critical

	return zipData, meta, nil
}

// FetchArtifactWithProgress downloads a single artifact with progress bar
func (f *ArtifactFetcher) FetchArtifactWithProgress(ctx context.Context, artifact *lockfile.Artifact, bar *progressbar.ProgressBar) (zipData []byte, meta *metadata.Metadata, err error) {
	// Try disk cache first
	zipData, err = cache.LoadArtifactFromDisk(artifact.Name, artifact.Version)
	if err == nil {
		// Cache hit, extract metadata and return
		metadataBytes, err := utils.ReadZipFile(zipData, "metadata.toml")
		if err == nil {
			meta, err = metadata.Parse(metadataBytes)
			if err == nil && meta.Validate() == nil {
				// Valid cached artifact - complete progress bar immediately
				if bar != nil {
					bar.ChangeMax64(int64(len(zipData)))
					_ = bar.Set64(int64(len(zipData)))
				}
				return zipData, meta, nil
			}
		}
		// Cache corrupted, fall through to download
	}

	// Download artifact through repository (handles auth properly)
	zipData, err = f.vault.GetArtifact(ctx, artifact)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to download artifact: %w", err)
	}

	// Update progress bar to 100% after download
	if bar != nil {
		bar.ChangeMax64(int64(len(zipData)))
		_ = bar.Set64(int64(len(zipData)))
	}

	// Verify it's a valid zip
	if !utils.IsZipFile(zipData) {
		return nil, nil, fmt.Errorf("downloaded file is not a valid zip archive")
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
	_ = cache.SaveArtifactToDisk(artifact.Name, artifact.Version, zipData)
	// Ignore cache save errors - not critical

	return zipData, meta, nil
}

// FetchArtifacts downloads multiple artifacts in parallel
func (f *ArtifactFetcher) FetchArtifacts(ctx context.Context, artifacts []*lockfile.Artifact, concurrency int) ([]DownloadResult, error) {
	if concurrency <= 0 {
		concurrency = 10 // Default
	}

	results := make([]DownloadResult, len(artifacts))
	tasks := make(chan DownloadTask, len(artifacts))
	resultChan := make(chan DownloadResult, len(artifacts))

	// Create worker pool
	var wg sync.WaitGroup
	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for task := range tasks {
				select {
				case <-ctx.Done():
					resultChan <- DownloadResult{
						Artifact: task.Artifact,
						Error:    ctx.Err(),
						Index:    task.Index,
					}
					return
				default:
				}

				// Create progress bar for this artifact if not in silent mode
				var bar *progressbar.ProgressBar
				if !config.IsSilent() {
					bar = progressbar.NewOptions64(
						-1, // Unknown size initially
						progressbar.OptionSetDescription(fmt.Sprintf("[%d/%d] %s", task.Index+1, len(artifacts), task.Artifact.Name)),
						progressbar.OptionSetWidth(30),
						progressbar.OptionShowBytes(true),
						progressbar.OptionSetPredictTime(true),
						progressbar.OptionClearOnFinish(),
					)
				}

				zipData, meta, err := f.FetchArtifactWithProgress(ctx, task.Artifact, bar)

				if bar != nil {
					_ = bar.Finish()
				}

				resultChan <- DownloadResult{
					Artifact: task.Artifact,
					ZipData:  zipData,
					Metadata: meta,
					Error:    err,
					Index:    task.Index,
				}
			}
		}()
	}

	// Send tasks
	go func() {
		for i, artifact := range artifacts {
			tasks <- DownloadTask{
				Artifact: artifact,
				Index:    i,
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
