package git

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// globalSSHKeyPath stores the SSH key path for the current execution
var globalSSHKeyPath string

// SetSSHKeyPath sets the global SSH key path from either the flag or environment variable
// This should be called once at startup from the root command
func SetSSHKeyPath(cmd *cobra.Command) {
	// Priority: flag value > environment variable > empty string
	if cmd != nil {
		if sshKey, err := cmd.Flags().GetString("ssh-key"); err == nil && sshKey != "" {
			globalSSHKeyPath = sshKey
			return
		}
	}

	// Fall back to environment variable
	globalSSHKeyPath = os.Getenv("SKILLS_SSH_KEY")
}

// GetSSHKeyPath returns the global SSH key path
func GetSSHKeyPath() string {
	return globalSSHKeyPath
}

// Client provides high-level git operations with SSH key support
type Client struct {
	sshKeyPath string
}

// NewClient creates a new git client using the global SSH key path
func NewClient() *Client {
	return &Client{
		sshKeyPath: GetSSHKeyPath(),
	}
}

// Clone clones a git repository to the specified destination path
func (c *Client) Clone(ctx context.Context, repoURL, destPath string) error {
	// Ensure parent directory exists
	if err := os.MkdirAll(destPath, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	cmd, _, err := execGitCommandWithURL(ctx, c.sshKeyPath, repoURL, "clone", "--quiet")
	if err != nil {
		return err
	}

	// Append destination path
	cmd.Args = append(cmd.Args, destPath)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone failed: %w\nOutput: %s", err, string(output))
	}

	return nil
}

// Fetch fetches all remotes in the repository
func (c *Client) Fetch(ctx context.Context, repoPath string) error {
	cmd := execGitCommand(ctx, c.sshKeyPath, "fetch", "--quiet", "--all")
	cmd.Dir = repoPath

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git fetch failed: %w\nOutput: %s", err, string(output))
	}

	return nil
}

// Pull pulls changes from the remote repository
func (c *Client) Pull(ctx context.Context, repoPath string) error {
	cmd := execGitCommand(ctx, c.sshKeyPath, "pull", "--quiet")
	cmd.Dir = repoPath

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git pull failed: %w\nOutput: %s", err, string(output))
	}

	return nil
}

// Push pushes changes to the remote repository
func (c *Client) Push(ctx context.Context, repoPath string) error {
	cmd := execGitCommand(ctx, c.sshKeyPath, "push", "--quiet")
	cmd.Dir = repoPath

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git push failed: %w\nOutput: %s", err, string(output))
	}

	return nil
}

// Checkout checks out a specific ref (branch, tag, or commit)
func (c *Client) Checkout(ctx context.Context, repoPath, ref string) error {
	cmd := execGitCommand(ctx, c.sshKeyPath, "checkout", "--quiet", ref)
	cmd.Dir = repoPath

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git checkout failed: %w\nOutput: %s", err, string(output))
	}

	return nil
}

// LsRemote queries a remote repository for a specific ref
// Returns the commit hash for the ref
func (c *Client) LsRemote(ctx context.Context, repoURL, ref string) (string, error) {
	// If ref looks like a full commit hash (40 hex chars), return it directly
	if len(ref) == 40 && isHexString(ref) {
		return ref, nil
	}

	cmd, _, err := execGitCommandWithURL(ctx, c.sshKeyPath, repoURL, "ls-remote")
	if err != nil {
		return "", err
	}

	// Append ref
	cmd.Args = append(cmd.Args, ref)

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git ls-remote failed: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 0 || lines[0] == "" {
		return "", fmt.Errorf("ref not found: %s", ref)
	}

	// Parse the output: <commit-hash>\t<ref-name>
	parts := strings.Fields(lines[0])
	if len(parts) < 1 {
		return "", fmt.Errorf("invalid git ls-remote output")
	}

	return parts[0], nil
}

// RevParse resolves a ref to a commit hash in a local repository
func (c *Client) RevParse(ctx context.Context, repoPath, ref string) (string, error) {
	cmd := execGitCommand(ctx, c.sshKeyPath, "rev-parse", ref)
	cmd.Dir = repoPath

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse failed: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}

// GetRemoteURL returns the remote URL for the repository (typically 'origin')
func (c *Client) GetRemoteURL(ctx context.Context, repoPath string) (string, error) {
	cmd := execGitCommand(ctx, c.sshKeyPath, "remote", "get-url", "origin")
	cmd.Dir = repoPath

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git remote get-url failed: %w\nOutput: %s", err, string(output))
	}

	remoteURL := strings.TrimSpace(string(output))
	return remoteURL, nil
}

// GetCurrentBranch returns the current branch name
func (c *Client) GetCurrentBranch(ctx context.Context, repoPath string) (string, error) {
	cmd := execGitCommand(ctx, c.sshKeyPath, "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = repoPath

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git rev-parse failed: %w\nOutput: %s", err, string(output))
	}

	branch := strings.TrimSpace(string(output))
	return branch, nil
}

// Add stages files for commit
func (c *Client) Add(ctx context.Context, repoPath string, paths ...string) error {
	args := append([]string{"add"}, paths...)
	cmd := execGitCommand(ctx, c.sshKeyPath, args...)
	cmd.Dir = repoPath

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git add failed: %w\nOutput: %s", err, string(output))
	}

	return nil
}

// Commit creates a commit with the given message
func (c *Client) Commit(ctx context.Context, repoPath, message string) error {
	cmd := execGitCommand(ctx, c.sshKeyPath, "commit", "-m", message)
	cmd.Dir = repoPath

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git commit failed: %w\nOutput: %s", err, string(output))
	}

	return nil
}

// isHexString checks if a string contains only hexadecimal characters
func isHexString(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}
