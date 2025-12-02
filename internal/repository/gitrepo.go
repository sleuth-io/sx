package repository

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/sleuth-io/skills/internal/cache"
	"github.com/sleuth-io/skills/internal/lockfile"
	"github.com/sleuth-io/skills/internal/metadata"
)

// GitRepository implements Repository for Git repositories
type GitRepository struct {
	repoURL     string
	repoPath    string
	httpHandler *HTTPSourceHandler
	pathHandler *PathSourceHandler
	gitHandler  *GitSourceHandler
}

// NewGitRepository creates a new Git repository
func NewGitRepository(repoURL string) (*GitRepository, error) {
	// Get cache path for this repository
	repoPath, err := cache.GetGitRepoCachePath(repoURL)
	if err != nil {
		return nil, fmt.Errorf("failed to get cache path: %w", err)
	}

	return &GitRepository{
		repoURL:     repoURL,
		repoPath:    repoPath,
		httpHandler: NewHTTPSourceHandler(),
		pathHandler: NewPathSourceHandler(repoPath), // Use repo path for relative paths
		gitHandler:  NewGitSourceHandler(),
	}, nil
}

// Authenticate performs authentication with the Git repository
// For Git repos, this is a no-op as authentication is handled by git itself
func (g *GitRepository) Authenticate(ctx context.Context) (string, error) {
	// Git authentication is handled by the user's git configuration
	// (SSH keys, credential helpers, etc.)
	return "", nil
}

// GetLockFile retrieves the lock file from the Git repository
func (g *GitRepository) GetLockFile(ctx context.Context, cachedETag string) (content []byte, etag string, notModified bool, err error) {
	// Clone or update repository
	if err := g.cloneOrUpdate(ctx); err != nil {
		return nil, "", false, fmt.Errorf("failed to clone/update repository: %w", err)
	}

	// Read sleuth.lock from repository root
	lockFilePath := filepath.Join(g.repoPath, "sleuth.lock")
	if _, err := os.Stat(lockFilePath); os.IsNotExist(err) {
		return nil, "", false, fmt.Errorf("sleuth.lock not found in repository")
	}

	data, err := os.ReadFile(lockFilePath)
	if err != nil {
		return nil, "", false, fmt.Errorf("failed to read lock file: %w", err)
	}

	// For Git repos, we could use the commit SHA as ETag
	// But for simplicity, we'll just return the data without ETag caching
	return data, "", false, nil
}

// GetArtifact downloads an artifact using its source configuration
func (g *GitRepository) GetArtifact(ctx context.Context, artifact *lockfile.Artifact) ([]byte, error) {
	// Dispatch to appropriate source handler based on artifact source type
	switch artifact.GetSourceType() {
	case "http":
		return g.httpHandler.Fetch(ctx, artifact)
	case "path":
		return g.pathHandler.Fetch(ctx, artifact)
	case "git":
		return g.gitHandler.Fetch(ctx, artifact)
	default:
		return nil, fmt.Errorf("unsupported source type: %s", artifact.GetSourceType())
	}
}

// AddArtifact uploads an artifact to the Git repository
func (g *GitRepository) AddArtifact(ctx context.Context, artifact *lockfile.Artifact, zipData []byte) error {
	// Clone or update repository
	if err := g.cloneOrUpdate(ctx); err != nil {
		return fmt.Errorf("failed to clone/update repository: %w", err)
	}

	// Create artifacts directory structure: artifacts/{name}/{version}/
	artifactDir := filepath.Join(g.repoPath, "artifacts", artifact.Name, artifact.Version)
	if err := os.MkdirAll(artifactDir, 0755); err != nil {
		return fmt.Errorf("failed to create artifact directory: %w", err)
	}

	// Write zip file
	zipFilename := fmt.Sprintf("%s-%s.zip", artifact.Name, artifact.Version)
	zipPath := filepath.Join(artifactDir, zipFilename)
	if err := os.WriteFile(zipPath, zipData, 0644); err != nil {
		return fmt.Errorf("failed to write zip file: %w", err)
	}

	// Update lock file
	lockFilePath := filepath.Join(g.repoPath, "sleuth.lock")
	lockFile, err := lockfile.ParseFile(lockFilePath)
	if err != nil {
		// If lock file doesn't exist, create a new one
		lockFile = &lockfile.LockFile{
			LockVersion: "1.0",
			Version:     "1",
			CreatedBy:   "skills-cli/0.1.0",
			Artifacts:   []lockfile.Artifact{},
		}
	}

	// Add artifact to lock file
	lockFile.Artifacts = append(lockFile.Artifacts, *artifact)

	// Write updated lock file
	if err := lockfile.Write(lockFile, lockFilePath); err != nil {
		return fmt.Errorf("failed to write lock file: %w", err)
	}

	// Git add, commit, and push
	if err := g.commitAndPush(ctx, artifact); err != nil {
		return fmt.Errorf("failed to commit and push: %w", err)
	}

	return nil
}

// GetVersionList retrieves available versions for an artifact
// Not applicable for Git repositories
func (g *GitRepository) GetVersionList(ctx context.Context, name string) ([]string, error) {
	return nil, fmt.Errorf("GetVersionList not supported for Git repositories")
}

// GetMetadata retrieves metadata for a specific artifact version
// Not applicable for Git repositories (metadata is inside the zip)
func (g *GitRepository) GetMetadata(ctx context.Context, name, version string) (*metadata.Metadata, error) {
	return nil, fmt.Errorf("GetMetadata not supported for Git repositories")
}

// VerifyIntegrity checks hashes and sizes for downloaded artifacts
func (g *GitRepository) VerifyIntegrity(data []byte, hashes map[string]string, size int64) error {
	// For Git repos, integrity is verified by Git's commit history
	// No additional verification needed
	return nil
}

// cloneOrUpdate clones the repository if it doesn't exist, or pulls updates if it does
func (g *GitRepository) cloneOrUpdate(ctx context.Context) error {
	if _, err := os.Stat(filepath.Join(g.repoPath, ".git")); os.IsNotExist(err) {
		// Repository doesn't exist, clone it
		return g.clone(ctx)
	}

	// Repository exists, pull updates
	return g.pull(ctx)
}

// clone clones the Git repository
func (g *GitRepository) clone(ctx context.Context) error {
	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(g.repoPath), 0755); err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, "git", "clone", "--quiet", g.repoURL, g.repoPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone failed: %w\nOutput: %s", err, string(output))
	}

	return nil
}

// pull pulls updates from the remote repository
func (g *GitRepository) pull(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "git", "pull", "--quiet")
	cmd.Dir = g.repoPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git pull failed: %w\nOutput: %s", err, string(output))
	}

	return nil
}

// commitAndPush commits and pushes changes
func (g *GitRepository) commitAndPush(ctx context.Context, artifact *lockfile.Artifact) error {
	// Add all changes
	cmd := exec.CommandContext(ctx, "git", "add", ".")
	cmd.Dir = g.repoPath
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git add failed: %w\nOutput: %s", err, string(output))
	}

	// Commit with message
	commitMsg := fmt.Sprintf("Add %s %s", artifact.Name, artifact.Version)
	cmd = exec.CommandContext(ctx, "git", "commit", "-m", commitMsg)
	cmd.Dir = g.repoPath
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git commit failed: %w\nOutput: %s", err, string(output))
	}

	// Push
	cmd = exec.CommandContext(ctx, "git", "push", "--quiet")
	cmd.Dir = g.repoPath
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git push failed: %w\nOutput: %s", err, string(output))
	}

	return nil
}
