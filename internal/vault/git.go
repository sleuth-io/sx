package vault

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sleuth-io/skills/internal/cache"
	"github.com/sleuth-io/skills/internal/git"
	"github.com/sleuth-io/skills/internal/lockfile"
	"github.com/sleuth-io/skills/internal/utils"
)

// GitSourceHandler handles artifacts with source-git
type GitSourceHandler struct {
	gitClient *git.Client
}

// NewGitSourceHandler creates a new Git source handler
func NewGitSourceHandler(gitClient *git.Client) *GitSourceHandler {
	return &GitSourceHandler{
		gitClient: gitClient,
	}
}

// Fetch clones/fetches a git repository and retrieves the artifact
func (g *GitSourceHandler) Fetch(ctx context.Context, artifact *lockfile.Artifact) ([]byte, error) {
	if artifact.SourceGit == nil {
		return nil, fmt.Errorf("artifact does not have source-git")
	}

	source := artifact.SourceGit

	// Get cache path for this repository
	repoCache, err := cache.GetGitRepoCachePath(source.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to get cache path: %w", err)
	}

	// Clone or update repository
	if err := g.cloneOrUpdate(ctx, source.URL, repoCache); err != nil {
		return nil, fmt.Errorf("failed to clone/update repository: %w", err)
	}

	// Checkout the specific commit
	if err := g.checkout(ctx, repoCache, source.Ref); err != nil {
		return nil, fmt.Errorf("failed to checkout ref %s: %w", source.Ref, err)
	}

	// Determine the directory to look for the artifact
	searchDir := repoCache
	if source.Subdirectory != "" {
		searchDir = filepath.Join(repoCache, source.Subdirectory)
		if !utils.IsDirectory(searchDir) {
			return nil, fmt.Errorf("subdirectory not found: %s", source.Subdirectory)
		}
	}

	// First, try to find .zip files in the directory
	zipFiles, err := g.findZipFiles(searchDir)
	if err != nil {
		return nil, fmt.Errorf("failed to find zip files: %w", err)
	}

	if len(zipFiles) > 0 {
		// Found zip files - use the first one or match by name
		var zipFile string
		if len(zipFiles) == 1 {
			zipFile = zipFiles[0]
		} else {
			for _, f := range zipFiles {
				base := filepath.Base(f)
				if strings.HasPrefix(base, artifact.Name) {
					zipFile = f
					break
				}
			}
			if zipFile == "" {
				zipFile = zipFiles[0] // Default to first
			}
		}

		// Read the zip file
		data, err := os.ReadFile(zipFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read zip file: %w", err)
		}

		// Verify it's a valid zip file
		if !utils.IsZipFile(data) {
			return nil, fmt.Errorf("file is not a valid zip archive: %s", zipFile)
		}

		return data, nil
	}

	// No zip files found - check if this is an exploded directory
	// Look for metadata.toml to confirm it's an artifact directory
	metadataPath := filepath.Join(searchDir, "metadata.toml")
	if utils.FileExists(metadataPath) {
		// This is an exploded artifact directory - create a zip from it
		data, err := utils.CreateZip(searchDir)
		if err != nil {
			return nil, fmt.Errorf("failed to create zip from directory: %w", err)
		}
		return data, nil
	}

	return nil, fmt.Errorf("no zip files or exploded artifact directory found in %s", searchDir)
}

// cloneOrUpdate clones the repository if it doesn't exist, or fetches updates if it does
func (g *GitSourceHandler) cloneOrUpdate(ctx context.Context, repoURL, repoPath string) error {
	if utils.IsDirectory(filepath.Join(repoPath, ".git")) {
		// Repository exists, fetch updates
		return g.fetch(ctx, repoPath)
	}

	// Repository doesn't exist, clone it
	return g.clone(ctx, repoURL, repoPath)
}

// clone clones a git repository
func (g *GitSourceHandler) clone(ctx context.Context, repoURL, repoPath string) error {
	return g.gitClient.Clone(ctx, repoURL, repoPath)
}

// fetch fetches updates from the remote repository
func (g *GitSourceHandler) fetch(ctx context.Context, repoPath string) error {
	return g.gitClient.Fetch(ctx, repoPath)
}

// checkout checks out a specific ref (commit SHA)
func (g *GitSourceHandler) checkout(ctx context.Context, repoPath, ref string) error {
	return g.gitClient.Checkout(ctx, repoPath, ref)
}

// findZipFiles finds all .zip files in a directory (non-recursive)
func (g *GitSourceHandler) findZipFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var zipFiles []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasSuffix(strings.ToLower(entry.Name()), ".zip") {
			zipFiles = append(zipFiles, filepath.Join(dir, entry.Name()))
		}
	}

	return zipFiles, nil
}

// ResolveRef resolves a branch or tag name to a commit SHA
// This is used during lock file generation to convert friendly names to commit SHAs
func (g *GitSourceHandler) ResolveRef(ctx context.Context, repoURL, ref string) (string, error) {
	// Get cache path for this repository
	repoCache, err := cache.GetGitRepoCachePath(repoURL)
	if err != nil {
		return "", fmt.Errorf("failed to get cache path: %w", err)
	}

	// Clone or update repository
	if err := g.cloneOrUpdate(ctx, repoURL, repoCache); err != nil {
		return "", fmt.Errorf("failed to clone/update repository: %w", err)
	}

	// Resolve ref to commit SHA
	sha, err := g.gitClient.RevParse(ctx, repoCache, ref)
	if err != nil {
		return "", err
	}

	if len(sha) != 40 {
		return "", fmt.Errorf("invalid commit SHA: %s", sha)
	}

	return sha, nil
}
