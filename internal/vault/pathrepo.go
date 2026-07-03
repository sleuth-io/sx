package vault

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/sleuth-io/sx/internal/bootstrap"
	"github.com/sleuth-io/sx/internal/git"
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/manifest"
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

// GetLockFile loads the vault's manifest and returns a lock file resolved
// for the caller's identity. Team/user scopes in the manifest are
// flattened to repo-scoped entries based on team membership and the
// caller's email so the install pipeline sees a concrete, identity-
// specific view. A shared read lock is held for the duration of the
// read so concurrent mgmt writes can't commit a manifest without its
// audit trail between the two IO operations.
func (p *PathVault) GetLockFile(ctx context.Context, cachedETag string) (content []byte, etag string, notModified bool, err error) {
	var data []byte
	err = p.withReadLock(ctx, func() error {
		bytes, err := resolveLockBytesForActor(ctx, p.repoPath)
		if err != nil {
			return err
		}
		data = bytes
		return nil
	})
	if err != nil {
		return nil, "", false, err
	}
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
	l, err := detectLayout(p.repoPath)
	if err != nil {
		return err
	}
	if err := storeAssetVersion(p.repoPath, l, asset.Name, asset.Version, zipData); err != nil {
		return err
	}

	// Point the asset at the stored (immutable) version directory so the
	// manifest records a layout-correct source path.
	asset.SourcePath = &lockfile.SourcePath{
		Path: l.SourcePathRel(asset.Name, asset.Version),
	}

	return nil
}

// GetVersionList retrieves available versions for an asset from list.txt
// Reuses the same pattern as GitRepository
func (p *PathVault) GetVersionList(ctx context.Context, name string) ([]string, error) {
	l, err := detectLayout(p.repoPath)
	if err != nil {
		return nil, err
	}
	return versionListForAsset(p.repoPath, l, name)
}

// GetMetadata retrieves metadata for a specific asset version
func (p *PathVault) GetMetadata(ctx context.Context, name, version string) (*metadata.Metadata, error) {
	l, err := detectLayout(p.repoPath)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(p.repoPath, l.MetadataPath(name, version)))
	if err != nil {
		if os.IsNotExist(err) {
			asset, ok, manifestErr := findAssetVersionInManifest(p.repoPath, name, version)
			if manifestErr != nil {
				return nil, manifestErr
			}
			if ok {
				return metadataFromAssetZip(ctx, p, asset)
			}
		}
		return nil, fmt.Errorf("failed to read metadata: %w", err)
	}
	return metadata.Parse(data)
}

// GetAssetByVersion retrieves an asset by name and version
// Creates a zip from the exploded directory
func (p *PathVault) GetAssetByVersion(ctx context.Context, name, version string) ([]byte, error) {
	l, err := detectLayout(p.repoPath)
	if err != nil {
		return nil, err
	}
	assetDir := filepath.Join(p.repoPath, l.VersionDir(name, version))
	if _, err := os.Stat(assetDir); os.IsNotExist(err) {
		asset, ok, manifestErr := findAssetVersionInManifest(p.repoPath, name, version)
		if manifestErr != nil {
			return nil, manifestErr
		}
		if ok {
			return p.GetAsset(ctx, asset)
		}
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

// PostUsageStats parses the JSONL payload and persists events to
// .sx/usage/YYYY-MM.jsonl via RecordUsageEvents.
func (p *PathVault) PostUsageStats(ctx context.Context, jsonlData string) error {
	events, err := parseUsageJSONL(jsonlData)
	if err != nil {
		return err
	}
	return p.RecordUsageEvents(ctx, events)
}

// ManifestPath returns the absolute path to the vault's manifest file.
func (p *PathVault) ManifestPath() string {
	return filepath.Join(p.repoPath, manifest.FileName)
}

// SetInstallations upserts an asset into the vault's manifest. The incoming
// asset's scopes replace the stored set, except for existing scopes the actor
// isn't allowed to remove (e.g. a team they don't admin), which are carried
// through — see commonSetInstallations and docs/rbac.md.
func (p *PathVault) SetInstallations(ctx context.Context, asset *lockfile.Asset, scopeEntity string) error {
	actor, err := p.CurrentActor(ctx)
	if err != nil {
		return err
	}
	return commonSetInstallations(p.repoPath, actor, asset)
}

// InheritInstallations copies scopes from any existing entry of this
// asset in the manifest, then upserts. Used when adding a new version of
// an asset so its install scopes are preserved.
func (p *PathVault) InheritInstallations(ctx context.Context, asset *lockfile.Asset) error {
	if err := inheritAssetScopesFromManifest(p.repoPath, asset); err != nil {
		return err
	}
	return upsertAssetInManifest(p.repoPath, asset)
}

// RemoveAsset removes an asset from the manifest. If delete is true, the
// asset's files are also removed from the vault's storage directory.
func (p *PathVault) RemoveAsset(ctx context.Context, assetName, version string, delete bool) error {
	removed, err := removeAssetFromManifest(p.repoPath, assetName, version)
	if err != nil {
		return err
	}
	if removed == 0 {
		return ErrAssetNotFound
	}

	if delete {
		if err := p.deleteAssetFiles(assetName, version); err != nil {
			return fmt.Errorf("failed to delete asset files: %w", err)
		}
	}

	return nil
}

// deleteAssetFiles removes asset files from the vault storage.
func (p *PathVault) deleteAssetFiles(assetName, version string) error {
	l, err := detectLayout(p.repoPath)
	if err != nil {
		return err
	}
	return deleteAssetStorage(p.repoPath, l, assetName, version)
}

// RenameAsset renames an asset in the vault.
func (p *PathVault) RenameAsset(ctx context.Context, oldName, newName string) error {
	l, err := detectLayout(p.repoPath)
	if err != nil {
		return err
	}
	if err := renameAssetStorage(p.repoPath, l, oldName, newName); err != nil {
		return err
	}
	updateRenamedAssetMetadata(p.repoPath, l, newName)

	return renameAssetInManifest(p.repoPath, l, oldName, newName)
}

// ListAssets returns a list of all assets in the vault by reading the assets/ directory
func (p *PathVault) ListAssets(ctx context.Context, opts ListAssetsOptions) (*ListAssetsResult, error) {
	l, err := detectLayout(p.repoPath)
	if err != nil {
		return nil, err
	}

	// Read assets/ directory
	assetsDir := filepath.Join(p.repoPath, l.AssetsRoot())
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
		// Dot-prefixed entries are root-view staging directories, not assets
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		// Read list.txt for versions
		versions, err := p.GetVersionList(ctx, entry.Name())
		if err != nil || len(versions) == 0 {
			continue // Skip if no versions
		}

		// Get metadata for latest version
		latestVersion := versions[len(versions)-1]
		metadataPath := filepath.Join(p.repoPath, l.MetadataPath(entry.Name(), latestVersion))

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

	if search := strings.TrimSpace(opts.Search); search != "" {
		assets = filterBySearch(assets, search)
	}

	// Apply limit if specified
	if opts.Limit > 0 && len(assets) > opts.Limit {
		assets = assets[:opts.Limit]
	}

	return &ListAssetsResult{Assets: assets}, nil
}

// GetAssetDetails returns detailed information about a specific asset
func (p *PathVault) GetAssetDetails(ctx context.Context, name string) (*AssetDetails, error) {
	l, err := detectLayout(p.repoPath)
	if err != nil {
		return nil, err
	}

	// Check if asset directory exists
	assetDir := filepath.Join(p.repoPath, l.AssetDir(name))
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
		versionDir := filepath.Join(p.repoPath, l.VersionDir(name, v))
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
	metadataPath := filepath.Join(p.repoPath, l.MetadataPath(name, latestVersion))

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

// GetMCPTools returns the asset-shim registrar so callers (notably the cloud
// serve MCP builder) can publish list_my_assets / load_my_asset / … on top of
// the path-backed vault. Without this, claude.ai connects to the relay and
// reports "no tools available" because PathVault has no native MCP surface.
func (p *PathVault) GetMCPTools() any {
	return &AssetShimRegistrar{Repo: p}
}

// GetBootstrapOptions returns no bootstrap options for PathVault
func (p *PathVault) GetBootstrapOptions(ctx context.Context) []bootstrap.Option {
	return nil
}
