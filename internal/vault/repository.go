package vault

import (
	"context"
	"fmt"
	"time"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/bootstrap"
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/metadata"
)

// ErrVersionExists is returned when attempting to add an asset version that already exists
type ErrVersionExists struct {
	Name    string
	Version string
	Message string
}

func (e *ErrVersionExists) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("version %s already exists for asset %s", e.Version, e.Name)
}

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
	AddAsset(ctx context.Context, asset *lockfile.Asset, zipData []byte) error

	// SetInstallations configures where an asset should be installed
	// Updates the lock file with the installation scopes
	SetInstallations(ctx context.Context, asset *lockfile.Asset) error

	// GetVersionList retrieves available versions for an asset (for resolution)
	// Only applicable to repositories with version management (Sleuth, not Git)
	GetVersionList(ctx context.Context, name string) ([]string, error)

	// GetMetadata retrieves metadata for a specific asset version
	GetMetadata(ctx context.Context, name, version string) (*metadata.Metadata, error)

	// GetAssetByVersion downloads an asset by name and version
	// Used for comparing content when adding assets
	GetAssetByVersion(ctx context.Context, name, version string) ([]byte, error)

	// VerifyIntegrity checks hashes and sizes for downloaded assets
	VerifyIntegrity(data []byte, hashes map[string]string, size int64) error

	// PostUsageStats sends asset usage statistics to the repository
	// jsonlData is newline-separated JSON (JSONL format)
	PostUsageStats(ctx context.Context, jsonlData string) error

	// RemoveAsset removes an asset from the lock file
	// The asset remains in the vault and can be re-added later
	// If version is empty, removes any version of the asset
	RemoveAsset(ctx context.Context, assetName, version string) error

	// ListAssets returns a list of all assets in the vault
	// This enables asset discovery via `sx vault list`
	ListAssets(ctx context.Context, opts ListAssetsOptions) (*ListAssetsResult, error)

	// GetAssetDetails returns detailed information about a specific asset
	// This enables asset inspection via `sx vault show <name>`
	GetAssetDetails(ctx context.Context, name string) (*AssetDetails, error)

	// GetMCPTools returns additional MCP tools provided by this vault
	// Returns nil if the vault doesn't provide any MCP tools
	GetMCPTools() any

	// GetBootstrapOptions returns bootstrap options provided by this vault
	// These are options for MCP servers or other infrastructure the vault provides
	GetBootstrapOptions(ctx context.Context) []bootstrap.Option
}

// SourceHandler handles fetching assets from specific source types
// This is used internally by Vault implementations to handle different source types
type SourceHandler interface {
	// Fetch retrieves asset data from the source
	Fetch(ctx context.Context, asset *lockfile.Asset) ([]byte, error)
}

// ListAssetsOptions contains options for listing vault assets
type ListAssetsOptions struct {
	Type   string // Filter by asset type (skill, mcp, etc.)
	Search string // Search query for filtering assets
	Limit  int    // Maximum number of assets to return (default 100)
}

// AssetSummary contains summary information about a vault asset
type AssetSummary struct {
	Name          string
	Type          asset.Type
	LatestVersion string
	VersionsCount int
	Description   string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// ListAssetsResult contains the results of a ListAssets call
type ListAssetsResult struct {
	Assets []AssetSummary
}

// AssetVersion contains version information for an asset
type AssetVersion struct {
	Version    string
	CreatedAt  time.Time
	FilesCount int
}

// AssetDetails contains detailed information about a specific asset
type AssetDetails struct {
	Name        string
	Type        asset.Type
	Description string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Versions    []AssetVersion
	Metadata    *metadata.Metadata // Metadata for latest version (or nil if not available)
}
