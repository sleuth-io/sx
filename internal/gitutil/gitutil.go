package gitutil

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sleuth-io/sx/internal/git"
)

// GitContext represents the current Git repository context
type GitContext struct {
	IsRepo       bool   // True if current directory is in a Git repository
	RepoRoot     string // Absolute path to repository root
	RepoURL      string // Remote repository URL
	RelativePath string // Current path relative to repo root
}

// DetectContext detects the Git context for the current working directory
func DetectContext(ctx context.Context) (*GitContext, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get working directory: %w", err)
	}

	return DetectContextForPath(ctx, cwd)
}

// DetectContextForPath detects the Git context for a specific path
func DetectContextForPath(ctx context.Context, path string) (*GitContext, error) {
	// Check if we're in a Git repository
	if !IsGitRepo(path) {
		return &GitContext{IsRepo: false}, nil
	}

	// Get repository root
	repoRoot, err := GetRepoRoot(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("failed to get repo root: %w", err)
	}

	// Get remote URL
	repoURL, err := GetRemoteURL(ctx, repoRoot)
	if err != nil {
		// Repository might not have a remote, that's okay
		repoURL = ""
	}

	// Calculate relative path
	relativePath, err := filepath.Rel(repoRoot, path)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate relative path: %w", err)
	}

	// Normalize to "." if at root
	if relativePath == "" {
		relativePath = "."
	}

	return &GitContext{
		IsRepo:       true,
		RepoRoot:     repoRoot,
		RepoURL:      repoURL,
		RelativePath: relativePath,
	}, nil
}

// IsGitRepo checks if the given path is inside a Git repository
func IsGitRepo(path string) bool {
	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = path
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}

	return strings.TrimSpace(string(output)) == "true"
}

// GetRepoRoot returns the root directory of the Git repository
func GetRepoRoot(ctx context.Context, path string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel")
	cmd.Dir = path
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git rev-parse failed: %w\nOutput: %s", err, string(output))
	}

	repoRoot := strings.TrimSpace(string(output))
	return repoRoot, nil
}

// GetRemoteURL returns the remote URL for the repository (typically 'origin')
func GetRemoteURL(ctx context.Context, repoPath string) (string, error) {
	gitClient := git.NewClient()
	return gitClient.GetRemoteURL(ctx, repoPath)
}

// GetCurrentBranch returns the current branch name
func GetCurrentBranch(ctx context.Context, repoPath string) (string, error) {
	gitClient := git.NewClient()
	return gitClient.GetCurrentBranch(ctx, repoPath)
}

// GetCurrentCommit returns the current commit SHA
func GetCurrentCommit(ctx context.Context, repoPath string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	cmd.Dir = repoPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git rev-parse failed: %w\nOutput: %s", err, string(output))
	}

	commit := strings.TrimSpace(string(output))
	return commit, nil
}

// HasUncommittedChanges checks if there are uncommitted changes in the repository
func HasUncommittedChanges(ctx context.Context, repoPath string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
	cmd.Dir = repoPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("git status failed: %w\nOutput: %s", err, string(output))
	}

	// If output is empty, there are no changes
	return len(strings.TrimSpace(string(output))) > 0, nil
}
