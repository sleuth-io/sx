package vault

import (
	"context"

	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/metadata"
)

// Vault represents a source of assets with read and write capabilities
// This interface unifies the concepts of "vault" and "source fetcher"
type Vault interface {
	// Authenticate performs authentication with the repository
	// Returns an auth token or empty string if no auth needed
	Authenticate(ctx context.Context) (string, error)

	// GetLockFile retrieves the lock file from the repository
	// Returns lock file content and ETag for caching
	// If cachedETag matches, returns notModified=true with empty content
	GetLockFile(ctx context.Context, cachedETag string) (content []byte, etag string, notModified bool, err error)

	// GetAsset downloads an asset using its source configuration from the lock file
	// The asset parameter contains the source configuration (source-http, source-git, source-path)
	GetAsset(ctx context.Context, asset *lockfile.Asset) ([]byte, error)

	// AddAsset uploads an asset to the repository
	// Updates the lock file with the new asset entry
	AddAsset(ctx context.Context, asset *lockfile.Asset, zipData []byte) error

	// GetVersionList retrieves available versions for an asset (for resolution)
	// Only applicable to repositories with version management (Sleuth, not Git)
	GetVersionList(ctx context.Context, name string) ([]string, error)

	// GetMetadata retrieves metadata for a specific asset version
	// Only applicable to repositories with version management (Sleuth, not Git)
	GetMetadata(ctx context.Context, name, version string) (*metadata.Metadata, error)

	// VerifyIntegrity checks hashes and sizes for downloaded assets
	VerifyIntegrity(data []byte, hashes map[string]string, size int64) error

	// PostUsageStats sends asset usage statistics to the repository
	// jsonlData is newline-separated JSON (JSONL format)
	PostUsageStats(ctx context.Context, jsonlData string) error

	// RemoveAsset removes an asset from the lock file
	// The asset remains in the vault and can be re-added later
	// If version is empty, removes any version of the asset
	RemoveAsset(ctx context.Context, assetName, version string) error
}

// SourceHandler handles fetching assets from specific source types
// This is used internally by Vault implementations to handle different source types
type SourceHandler interface {
	// Fetch retrieves asset data from the source
	Fetch(ctx context.Context, asset *lockfile.Asset) ([]byte, error)
}
