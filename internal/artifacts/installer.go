package artifacts

import (
	"context"
	"fmt"
	"sync"

	"github.com/sleuth-io/skills/internal/handlers"
	"github.com/sleuth-io/skills/internal/lockfile"
	"github.com/sleuth-io/skills/internal/metadata"
	"github.com/sleuth-io/skills/internal/repository"
	"github.com/sleuth-io/skills/internal/utils"
)

// ArtifactInstaller handles installation of artifacts
type ArtifactInstaller struct {
	repo       repository.Repository
	targetBase string
	cache      *ArtifactCache
}

// NewArtifactInstaller creates a new artifact installer
func NewArtifactInstaller(repo repository.Repository, targetBase string) *ArtifactInstaller {
	return &ArtifactInstaller{
		repo:       repo,
		targetBase: targetBase,
		cache:      NewArtifactCache(),
	}
}

// Install installs a single artifact
func (i *ArtifactInstaller) Install(ctx context.Context, artifact *lockfile.Artifact, zipData []byte, meta *metadata.Metadata) error {
	// Create handler for this artifact type
	handler, err := handlers.NewHandler(meta)
	if err != nil {
		return fmt.Errorf("failed to create handler: %w", err)
	}

	// Install the artifact
	if err := handler.Install(ctx, zipData, i.targetBase); err != nil {
		return fmt.Errorf("failed to install artifact: %w", err)
	}

	return nil
}

// InstallAll installs multiple artifacts in dependency order
func (i *ArtifactInstaller) InstallAll(ctx context.Context, artifacts []*ArtifactWithMetadata) (*InstallResult, error) {
	result := &InstallResult{
		Installed: []string{},
		Failed:    []string{},
		Errors:    []error{},
	}

	// Install each artifact in order (already sorted by dependencies)
	for _, item := range artifacts {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}

		err := i.Install(ctx, item.Artifact, item.ZipData, item.Metadata)
		if err != nil {
			result.Failed = append(result.Failed, item.Artifact.Name)
			result.Errors = append(result.Errors, fmt.Errorf("%s: %w", item.Artifact.Name, err))
			// Continue with other artifacts (don't fail-fast)
			continue
		}

		result.Installed = append(result.Installed, item.Artifact.Name)
	}

	return result, nil
}

// Remove removes a single artifact
func (i *ArtifactInstaller) Remove(ctx context.Context, artifact *lockfile.Artifact) error {
	// We need metadata to know the artifact type
	// For removal, we can try to read metadata from the installed location
	// or create a minimal metadata object based on the artifact type

	meta := &metadata.Metadata{
		Artifact: metadata.Artifact{
			Name:    artifact.Name,
			Version: artifact.Version,
			Type:    string(artifact.Type),
		},
	}

	// Create handler for this artifact type
	handler, err := handlers.NewHandler(meta)
	if err != nil {
		return fmt.Errorf("failed to create handler: %w", err)
	}

	// Remove the artifact
	if err := handler.Remove(ctx, i.targetBase); err != nil {
		return fmt.Errorf("failed to remove artifact: %w", err)
	}

	return nil
}

// ArtifactFetcher handles fetching artifacts from a repository
type ArtifactFetcher struct {
	repo repository.Repository
}

// NewArtifactFetcher creates a new artifact fetcher
func NewArtifactFetcher(repo repository.Repository) *ArtifactFetcher {
	return &ArtifactFetcher{
		repo: repo,
	}
}

// FetchArtifact downloads a single artifact
func (f *ArtifactFetcher) FetchArtifact(ctx context.Context, artifact *lockfile.Artifact) (zipData []byte, meta *metadata.Metadata, err error) {
	// Download artifact
	zipData, err = f.repo.GetArtifact(ctx, artifact)
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

				zipData, meta, err := f.FetchArtifact(ctx, task.Artifact)
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

// ArtifactCache manages caching of downloaded artifacts
type ArtifactCache struct {
	mu    sync.RWMutex
	cache map[string][]byte
}

// NewArtifactCache creates a new artifact cache
func NewArtifactCache() *ArtifactCache {
	return &ArtifactCache{
		cache: make(map[string][]byte),
	}
}

// Get retrieves an artifact from cache
func (c *ArtifactCache) Get(name, version string) ([]byte, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	key := fmt.Sprintf("%s@%s", name, version)
	data, ok := c.cache[key]
	return data, ok
}

// Set stores an artifact in cache
func (c *ArtifactCache) Set(name, version string, data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := fmt.Sprintf("%s@%s", name, version)
	c.cache[key] = data
}

// Clear clears the cache
func (c *ArtifactCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.cache = make(map[string][]byte)
}
