package vault

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/sleuth-io/sx/internal/bootstrap"
	"github.com/sleuth-io/sx/internal/constants"
	"github.com/sleuth-io/sx/internal/git"
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/metadata"
	"github.com/sleuth-io/sx/internal/utils"
)

// PathVault implements Vault for local filesystem directories
// It follows the same pattern as GitRepository and SleuthRepository
type PathVault struct {
	repoPath    string
	httpHandler *HTTPSourceHandler
	pathHandler *PathSourceHandler
	gitHandler  *GitSourceHandler
}

// NewPathVault creates a new path repository from a file:// URL
func NewPathVault(repoURL string) (*PathVault, error) {
	// Parse the file:// URL
	path, err := parseFileURL(repoURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse file URL: %w", err)
	}

	// Ensure the directory exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, fmt.Errorf("directory does not exist: %s", path)
	}

	gitClient := git.NewClient()
	return &PathVault{
		repoPath:    path,
		httpHandler: NewHTTPSourceHandler(""),   // No auth token for path repos
		pathHandler: NewPathSourceHandler(path), // Use repo path for relative paths
		gitHandler:  NewGitSourceHandler(gitClient),
	}, nil
}

// parseFileURL parses a file:// URL and returns the filesystem path
func parseFileURL(fileURL string) (string, error) {
	// Handle file:// URLs
	if strings.HasPrefix(fileURL, "file://") {
		u, err := url.Parse(fileURL)
		if err != nil {
			return "", fmt.Errorf("invalid file URL: %w", err)
		}
		// url.Parse converts file:///path to Path=/path
		// On Windows, file:///C:/path becomes Path=/C:/path
		path := u.Path

		// On Windows, remove leading slash before drive letter
		if len(path) > 2 && path[0] == '/' && path[2] == ':' {
			path = path[1:]
		}

		return filepath.Clean(path), nil
	}

	// If not a file:// URL, treat as a regular path (for convenience)
	return filepath.Clean(fileURL), nil
}

// Authenticate performs authentication - no-op for path repositories
func (p *PathVault) Authenticate(ctx context.Context) (string, error) {
	return "", nil
}

// GetLockFile retrieves the lock file from the local directory
func (p *PathVault) GetLockFile(ctx context.Context, cachedETag string) (content []byte, etag string, notModified bool, err error) {
	lockFilePath := filepath.Join(p.repoPath, constants.SkillLockFile)
	if _, err := os.Stat(lockFilePath); os.IsNotExist(err) {
		return nil, "", false, fmt.Errorf("%s not found in directory: %s", constants.SkillLockFile, p.repoPath)
	}

	data, err := os.ReadFile(lockFilePath)
	if err != nil {
		return nil, "", false, fmt.Errorf("failed to read lock file: %w", err)
	}

	// No ETag support for local files - always return the data
	return data, "", false, nil
}

// GetAsset downloads an asset using its source configuration
// Reuses the same dispatch pattern as GitRepository and SleuthRepository
func (p *PathVault) GetAsset(ctx context.Context, asset *lockfile.Asset) ([]byte, error) {
	// Dispatch to appropriate source handler based on asset source type
	switch asset.GetSourceType() {
	case "http":
		return p.httpHandler.Fetch(ctx, asset)
	case "path":
		return p.pathHandler.Fetch(ctx, asset)
	case "git":
		return p.gitHandler.Fetch(ctx, asset)
	default:
		return nil, fmt.Errorf("unsupported source type: %s", asset.GetSourceType())
	}
}

// AddAsset adds an asset to the local repository
// Follows the same pattern as GitRepository: exploded storage + list.txt
func (p *PathVault) AddAsset(ctx context.Context, asset *lockfile.Asset, zipData []byte) error {
	// Create assets directory structure: assets/{name}/{version}/
	assetDir := filepath.Join(p.repoPath, "assets", asset.Name, asset.Version)
	if err := os.MkdirAll(assetDir, 0755); err != nil {
		return fmt.Errorf("failed to create asset directory: %w", err)
	}

	// Store assets exploded (not as zip) for easier browsing
	// Reuse extractZipToDir from GitRepository
	if err := extractZipToDir(zipData, assetDir); err != nil {
		return fmt.Errorf("failed to extract zip to directory: %w", err)
	}

	// Update list.txt with this version
	listPath := filepath.Join(p.repoPath, "assets", asset.Name, "list.txt")
	if err := p.updateVersionList(listPath, asset.Version); err != nil {
		return fmt.Errorf("failed to update version list: %w", err)
	}

	// Update asset with path source pointing to the extracted directory
	relPath := filepath.Join("assets", asset.Name, asset.Version)
	asset.SourcePath = &lockfile.SourcePath{
		Path: relPath,
	}

	return nil
}

// GetVersionList retrieves available versions for an asset from list.txt
// Reuses the same pattern as GitRepository
func (p *PathVault) GetVersionList(ctx context.Context, name string) ([]string, error) {
	listPath := filepath.Join(p.repoPath, "assets", name, "list.txt")
	if _, err := os.Stat(listPath); os.IsNotExist(err) {
		// No versions exist for this asset
		return []string{}, nil
	}

	data, err := os.ReadFile(listPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read version list: %w", err)
	}

	// Parse versions from file using common parser (shared with GitRepository and SleuthRepository)
	return parseVersionList(data), nil
}

// GetMetadata retrieves metadata for a specific asset version
func (p *PathVault) GetMetadata(ctx context.Context, name, version string) (*metadata.Metadata, error) {
	metadataPath := filepath.Join(p.repoPath, "assets", name, version, "metadata.toml")
	data, err := os.ReadFile(metadataPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read metadata: %w", err)
	}
	return metadata.Parse(data)
}

// GetAssetByVersion retrieves an asset by name and version
// Creates a zip from the exploded directory
func (p *PathVault) GetAssetByVersion(ctx context.Context, name, version string) ([]byte, error) {
	assetDir := filepath.Join(p.repoPath, "assets", name, version)
	if _, err := os.Stat(assetDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("asset %s@%s not found", name, version)
	}

	// Create zip from directory
	return utils.CreateZip(assetDir)
}

// VerifyIntegrity checks hashes and sizes for downloaded assets
// Same as GitRepository: no verification needed for local files
func (p *PathVault) VerifyIntegrity(data []byte, hashes map[string]string, size int64) error {
	// For path repos, integrity is assumed since files are local
	// No additional verification needed
	return nil
}

// PostUsageStats is a no-op for path repositories
// Same as GitRepository
func (p *PathVault) PostUsageStats(ctx context.Context, jsonlData string) error {
	return nil
}

// GetLockFilePath returns the path to the lock file in the path repository
func (p *PathVault) GetLockFilePath() string {
	return filepath.Join(p.repoPath, constants.SkillLockFile)
}

// SetInstallations updates the lock file with installation scopes
func (p *PathVault) SetInstallations(ctx context.Context, asset *lockfile.Asset) error {
	lockFilePath := p.GetLockFilePath()
	return lockfile.AddOrUpdateAsset(lockFilePath, asset)
}

// RemoveAsset removes an asset from the lock file
func (p *PathVault) RemoveAsset(ctx context.Context, assetName, version string) error {
	return lockfile.RemoveAsset(p.GetLockFilePath(), assetName, version)
}

// updateVersionList updates the list.txt file with a new version
// Reuses the same logic as GitRepository
func (p *PathVault) updateVersionList(listPath, newVersion string) error {
	var versions []string

	// Read existing versions if file exists
	if data, err := os.ReadFile(listPath); err == nil {
		versions = parseVersionList(data)
	}

	// Check if version already exists
	if slices.Contains(versions, newVersion) {
		return nil // Version already in list
	}

	// Add new version
	versions = append(versions, newVersion)

	// Write back to file
	content := strings.Join(versions, "\n") + "\n"
	return os.WriteFile(listPath, []byte(content), 0644)
}

// ListAssets returns a list of all assets in the vault by reading the assets/ directory
func (p *PathVault) ListAssets(ctx context.Context, opts ListAssetsOptions) (*ListAssetsResult, error) {
	// Read assets/ directory
	assetsDir := filepath.Join(p.repoPath, "assets")
	entries, err := os.ReadDir(assetsDir)
	if err != nil {
		if os.IsNotExist(err) {
			// No assets directory means no assets
			return &ListAssetsResult{Assets: []AssetSummary{}}, nil
		}
		return nil, fmt.Errorf("failed to read assets directory: %w", err)
	}

	var assets []AssetSummary
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// Read list.txt for versions
		versions, err := p.GetVersionList(ctx, entry.Name())
		if err != nil || len(versions) == 0 {
			continue // Skip if no versions
		}

		// Get metadata for latest version
		latestVersion := versions[len(versions)-1]
		metadataPath := filepath.Join(p.repoPath, "assets", entry.Name(), latestVersion, "metadata.toml")

		assetSummary := AssetSummary{
			Name:          entry.Name(),
			LatestVersion: latestVersion,
			VersionsCount: len(versions),
		}

		// Try to read metadata
		if metaData, err := os.ReadFile(metadataPath); err == nil {
			if meta, err := metadata.Parse(metaData); err == nil {
				assetSummary.Type = meta.Asset.Type
				assetSummary.Description = meta.Asset.Description
			}
		}

		// Get file timestamps
		assetDirInfo, _ := entry.Info()
		if assetDirInfo != nil {
			assetSummary.CreatedAt = assetDirInfo.ModTime()
			assetSummary.UpdatedAt = assetDirInfo.ModTime()
		}

		// Apply type filter if specified
		if opts.Type != "" && assetSummary.Type.Key != opts.Type {
			continue
		}

		assets = append(assets, assetSummary)
	}

	// Apply limit if specified
	if opts.Limit > 0 && len(assets) > opts.Limit {
		assets = assets[:opts.Limit]
	}

	return &ListAssetsResult{Assets: assets}, nil
}

// GetAssetDetails returns detailed information about a specific asset
func (p *PathVault) GetAssetDetails(ctx context.Context, name string) (*AssetDetails, error) {
	// Check if asset directory exists
	assetDir := filepath.Join(p.repoPath, "assets", name)
	if _, err := os.Stat(assetDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("asset '%s' not found", name)
	}

	// Get version list
	versions, err := p.GetVersionList(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("failed to get version list: %w", err)
	}

	if len(versions) == 0 {
		return nil, fmt.Errorf("asset '%s' has no versions", name)
	}

	// Build version list with file info
	var versionList []AssetVersion
	for _, v := range versions {
		versionDir := filepath.Join(assetDir, v)
		versionInfo, err := os.Stat(versionDir)

		versionEntry := AssetVersion{Version: v}
		if err == nil {
			versionEntry.CreatedAt = versionInfo.ModTime()

			// Count files in version directory
			if entries, err := os.ReadDir(versionDir); err == nil {
				fileCount := 0
				for _, e := range entries {
					if !e.IsDir() {
						fileCount++
					}
				}
				versionEntry.FilesCount = fileCount
			}
		}

		versionList = append(versionList, versionEntry)
	}

	// Get metadata for latest version
	latestVersion := versions[len(versions)-1]
	metadataPath := filepath.Join(assetDir, latestVersion, "metadata.toml")

	details := &AssetDetails{
		Name:     name,
		Versions: versionList,
	}

	// Try to read metadata
	if metaData, err := os.ReadFile(metadataPath); err == nil {
		if meta, err := metadata.Parse(metaData); err == nil {
			details.Type = meta.Asset.Type
			details.Description = meta.Asset.Description
			details.Metadata = meta
		}
	}

	// Get directory timestamps
	if assetDirInfo, err := os.Stat(assetDir); err == nil {
		details.CreatedAt = assetDirInfo.ModTime()
		details.UpdatedAt = assetDirInfo.ModTime()
	}

	return details, nil
}

// GetMCPTools returns no additional MCP tools for PathVault
func (p *PathVault) GetMCPTools() any {
	return nil
}

// GetBootstrapOptions returns no bootstrap options for PathVault
func (p *PathVault) GetBootstrapOptions(ctx context.Context) []bootstrap.Option {
	return nil
}
