package repository

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/sleuth-io/skills/internal/constants"
	"github.com/sleuth-io/skills/internal/git"
	"github.com/sleuth-io/skills/internal/lockfile"
	"github.com/sleuth-io/skills/internal/metadata"
)

// PathRepository implements Repository for local filesystem directories
// It follows the same pattern as GitRepository and SleuthRepository
type PathRepository struct {
	repoPath    string
	httpHandler *HTTPSourceHandler
	pathHandler *PathSourceHandler
	gitHandler  *GitSourceHandler
}

// NewPathRepository creates a new path repository from a file:// URL
func NewPathRepository(repoURL string) (*PathRepository, error) {
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
	return &PathRepository{
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
func (p *PathRepository) Authenticate(ctx context.Context) (string, error) {
	return "", nil
}

// GetLockFile retrieves the lock file from the local directory
func (p *PathRepository) GetLockFile(ctx context.Context, cachedETag string) (content []byte, etag string, notModified bool, err error) {
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

// GetArtifact downloads an artifact using its source configuration
// Reuses the same dispatch pattern as GitRepository and SleuthRepository
func (p *PathRepository) GetArtifact(ctx context.Context, artifact *lockfile.Artifact) ([]byte, error) {
	// Dispatch to appropriate source handler based on artifact source type
	switch artifact.GetSourceType() {
	case "http":
		return p.httpHandler.Fetch(ctx, artifact)
	case "path":
		return p.pathHandler.Fetch(ctx, artifact)
	case "git":
		return p.gitHandler.Fetch(ctx, artifact)
	default:
		return nil, fmt.Errorf("unsupported source type: %s", artifact.GetSourceType())
	}
}

// AddArtifact adds an artifact to the local repository
// Follows the same pattern as GitRepository: exploded storage + list.txt
func (p *PathRepository) AddArtifact(ctx context.Context, artifact *lockfile.Artifact, zipData []byte) error {
	// Create artifacts directory structure: artifacts/{name}/{version}/
	artifactDir := filepath.Join(p.repoPath, "artifacts", artifact.Name, artifact.Version)
	if err := os.MkdirAll(artifactDir, 0755); err != nil {
		return fmt.Errorf("failed to create artifact directory: %w", err)
	}

	// Store artifacts exploded (not as zip) for easier browsing
	// Reuse extractZipToDir from GitRepository
	if err := extractZipToDir(zipData, artifactDir); err != nil {
		return fmt.Errorf("failed to extract zip to directory: %w", err)
	}

	// Update list.txt with this version
	listPath := filepath.Join(p.repoPath, "artifacts", artifact.Name, "list.txt")
	if err := p.updateVersionList(listPath, artifact.Version); err != nil {
		return fmt.Errorf("failed to update version list: %w", err)
	}

	// Update artifact with path source pointing to the extracted directory
	relPath := filepath.Join("artifacts", artifact.Name, artifact.Version)
	artifact.SourcePath = &lockfile.SourcePath{
		Path: relPath,
	}

	return nil
}

// GetVersionList retrieves available versions for an artifact from list.txt
// Reuses the same pattern as GitRepository
func (p *PathRepository) GetVersionList(ctx context.Context, name string) ([]string, error) {
	listPath := filepath.Join(p.repoPath, "artifacts", name, "list.txt")
	if _, err := os.Stat(listPath); os.IsNotExist(err) {
		// No versions exist for this artifact
		return []string{}, nil
	}

	data, err := os.ReadFile(listPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read version list: %w", err)
	}

	// Parse versions from file using common parser (shared with GitRepository and SleuthRepository)
	return parseVersionList(data), nil
}

// GetMetadata retrieves metadata for a specific artifact version
// Not applicable for path repositories (metadata is inside the artifact)
func (p *PathRepository) GetMetadata(ctx context.Context, name, version string) (*metadata.Metadata, error) {
	return nil, fmt.Errorf("GetMetadata not supported for path repositories")
}

// VerifyIntegrity checks hashes and sizes for downloaded artifacts
// Same as GitRepository: no verification needed for local files
func (p *PathRepository) VerifyIntegrity(data []byte, hashes map[string]string, size int64) error {
	// For path repos, integrity is assumed since files are local
	// No additional verification needed
	return nil
}

// PostUsageStats is a no-op for path repositories
// Same as GitRepository
func (p *PathRepository) PostUsageStats(ctx context.Context, jsonlData string) error {
	return nil
}

// GetLockFilePath returns the path to the lock file in the path repository
func (p *PathRepository) GetLockFilePath() string {
	return filepath.Join(p.repoPath, constants.SkillLockFile)
}

// updateVersionList updates the list.txt file with a new version
// Reuses the same logic as GitRepository
func (p *PathRepository) updateVersionList(listPath, newVersion string) error {
	var versions []string

	// Read existing versions if file exists
	if data, err := os.ReadFile(listPath); err == nil {
		versions = parseVersionList(data)
	}

	// Check if version already exists
	for _, v := range versions {
		if v == newVersion {
			return nil // Version already in list
		}
	}

	// Add new version
	versions = append(versions, newVersion)

	// Write back to file
	content := strings.Join(versions, "\n") + "\n"
	return os.WriteFile(listPath, []byte(content), 0644)
}
