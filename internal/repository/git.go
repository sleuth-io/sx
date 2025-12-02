package repository

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sleuth-io/skills/internal/cache"
	"github.com/sleuth-io/skills/internal/lockfile"
	"github.com/sleuth-io/skills/internal/utils"
)

// GitSourceHandler handles artifacts with source-git
type GitSourceHandler struct{}

// NewGitSourceHandler creates a new Git source handler
func NewGitSourceHandler() *GitSourceHandler {
	return &GitSourceHandler{}
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

	// Find .zip files in the directory
	zipFiles, err := g.findZipFiles(searchDir)
	if err != nil {
		return nil, fmt.Errorf("failed to find zip files: %w", err)
	}

	if len(zipFiles) == 0 {
		return nil, fmt.Errorf("no zip files found in %s", searchDir)
	}

	// If multiple zip files, look for one matching the artifact name
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
			return nil, fmt.Errorf("multiple zip files found, none matching artifact name %s", artifact.Name)
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
	// Ensure parent directory exists
	if err := utils.EnsureDir(filepath.Dir(repoPath)); err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, "git", "clone", "--quiet", repoURL, repoPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone failed: %w\nOutput: %s", err, string(output))
	}

	return nil
}

// fetch fetches updates from the remote repository
func (g *GitSourceHandler) fetch(ctx context.Context, repoPath string) error {
	cmd := exec.CommandContext(ctx, "git", "fetch", "--quiet", "--all")
	cmd.Dir = repoPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git fetch failed: %w\nOutput: %s", err, string(output))
	}

	return nil
}

// checkout checks out a specific ref (commit SHA)
func (g *GitSourceHandler) checkout(ctx context.Context, repoPath, ref string) error {
	cmd := exec.CommandContext(ctx, "git", "checkout", "--quiet", ref)
	cmd.Dir = repoPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git checkout failed: %w\nOutput: %s", err, string(output))
	}

	return nil
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
	cmd := exec.CommandContext(ctx, "git", "rev-parse", ref)
	cmd.Dir = repoCache
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git rev-parse failed: %w\nOutput: %s", err, string(output))
	}

	sha := strings.TrimSpace(string(output))
	if len(sha) != 40 {
		return "", fmt.Errorf("invalid commit SHA: %s", sha)
	}

	return sha, nil
}
