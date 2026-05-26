package git

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/sx/internal/logger"
)

// globalSSHKeyPath stores the SSH key path for the current execution
var globalSSHKeyPath string

// SetSSHKeyPath sets the global SSH key path from either the flag or environment variable
// This should be called once at startup from the root command
func SetSSHKeyPath(cmd *cobra.Command) {
	// Priority: flag value > environment variable > empty string
	if sshKey, err := cmd.Flags().GetString("ssh-key"); err == nil && sshKey != "" {
		globalSSHKeyPath = sshKey
		printSSHKeyInfo(cmd, "flag", sshKey)
		return
	}

	// Fall back to environment variable (support both new and legacy)
	envKey := os.Getenv("SX_SSH_KEY")
	if envKey == "" {
		envKey = os.Getenv("SKILLS_SSH_KEY")
	}
	if envKey != "" {
		globalSSHKeyPath = envKey
		printSSHKeyInfo(cmd, "environment variable", envKey)
	}
}

// printSSHKeyInfo prints a safe indication that an SSH key was loaded
func printSSHKeyInfo(cmd *cobra.Command, source string, keyPathOrContent string) {
	keyPathOrContent = strings.TrimSpace(keyPathOrContent)

	var msg string
	if strings.HasPrefix(keyPathOrContent, "-----BEGIN") {
		// It's key content - show first line and length
		lines := strings.Split(keyPathOrContent, "\n")
		firstLine := strings.TrimSpace(lines[0])
		msg = fmt.Sprintf("SSH key loaded from %s (inline content, %d bytes, type: %s)\n",
			source, len(keyPathOrContent), firstLine)
	} else {
		// It's a file path - show the path
		msg = fmt.Sprintf("SSH key loaded from %s: %s\n", source, keyPathOrContent)
	}

	cmd.PrintErr(msg)
}

// GetSSHKeyPath returns the global SSH key path
func GetSSHKeyPath() string {
	return globalSSHKeyPath
}

// Client provides high-level git operations with SSH key support
type Client struct {
	sshKeyPath string
	extraEnv   []string
}

// NewClient creates a new git client using the global SSH key path
func NewClient() *Client {
	return &Client{
		sshKeyPath: GetSSHKeyPath(),
	}
}

type ClientOption func(*Client)

func NewClientWithOptions(opts ...ClientOption) *Client {
	c := NewClient()
	for _, opt := range opts {
		if opt != nil {
			opt(c)
		}
	}
	return c
}

func WithEnv(env ...string) ClientOption {
	return func(c *Client) {
		c.extraEnv = append(c.extraEnv, env...)
	}
}

// WithSSHKey overrides the SSH key path that would otherwise be inherited
// from the process-global value set by SetSSHKeyPath. Library consumers that
// don't go through the CLI flag/env wiring should use this to scope an SSH
// key to a single git.Client.
func WithSSHKey(path string) ClientOption {
	return func(c *Client) {
		c.sshKeyPath = path
	}
}

func WithCommitActor(name, email string) ClientOption {
	env := []string{}
	if name != "" {
		env = append(env, "GIT_AUTHOR_NAME="+name, "GIT_COMMITTER_NAME="+name)
	}
	if email != "" {
		env = append(env, "GIT_AUTHOR_EMAIL="+email, "GIT_COMMITTER_EMAIL="+email)
	}
	return WithEnv(env...)
}

func WithHTTPSBasicAuth(host, username, password string) ClientOption {
	return WithHTTPBasicAuth("https", host, username, password)
}

func WithHTTPBasicAuth(scheme, host, username, password string) ClientOption {
	scheme = strings.TrimSpace(strings.ToLower(scheme))
	if scheme == "" {
		scheme = "https"
	}
	if host == "" || username == "" || password == "" {
		return nil
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
	return WithEnv(
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=http."+scheme+"://"+host+"/.extraheader",
		"GIT_CONFIG_VALUE_0=AUTHORIZATION: basic "+encoded,
	)
}

func (c *Client) ExtraEnv() []string {
	if c == nil {
		return nil
	}
	return append([]string(nil), c.extraEnv...)
}

func (c *Client) command(ctx context.Context, args ...string) *exec.Cmd {
	return execGitCommandWithEnv(ctx, c.sshKeyPath, c.extraEnv, args...)
}

func (c *Client) commandWithURL(ctx context.Context, repoURL string, args ...string) (*exec.Cmd, string, error) {
	return execGitCommandWithURLAndEnv(ctx, c.sshKeyPath, c.extraEnv, repoURL, args...)
}

// Clone clones a git repository to the specified destination path
func (c *Client) Clone(ctx context.Context, repoURL, destPath string) error {
	// Ensure parent directory exists
	if err := os.MkdirAll(destPath, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	cmd, _, err := c.commandWithURL(ctx, repoURL, "clone", "--quiet")
	if err != nil {
		return err
	}

	// Append destination path
	cmd.Args = append(cmd.Args, destPath)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return classifyRemoteError(repoURL, string(output), err)
	}

	return nil
}

// Fetch fetches all remotes in the repository
func (c *Client) Fetch(ctx context.Context, repoPath string) error {
	cmd := c.command(ctx, "fetch", "--quiet", "--all")
	cmd.Dir = repoPath

	output, err := cmd.CombinedOutput()
	if err != nil {
		return classifyRemoteError(c.remoteLocation(ctx, repoPath), string(output), err)
	}

	return nil
}

// Pull pulls changes from the remote repository
func (c *Client) Pull(ctx context.Context, repoPath string) error {
	log := logger.Get()
	start := time.Now()
	log.Debug("git pull starting", "repoPath", repoPath)

	// --no-rebase pins the reconciliation strategy to merge so newer
	// git (≥2.27) doesn't refuse to pull divergent branches without an
	// explicit pull.rebase / pull.ff setting. The merge path is also
	// what lets gitattributes merge=union drivers run on conflicting
	// append-only files like .sx/usage/*.jsonl.
	cmd := c.command(ctx, "pull", "--no-rebase", "--no-edit", "--quiet")
	cmd.Dir = repoPath

	output, err := cmd.CombinedOutput()
	log.Debug("git pull completed", "duration", time.Since(start), "error", err)

	if err != nil {
		return classifyRemoteError(c.remoteLocation(ctx, repoPath), string(output), err)
	}

	return nil
}

// PullRebase pulls changes from the remote repository and rebases local
// commits on top. Used by runInVaultTx to resolve concurrent pushes from
// multiple sx processes writing to the same management files.
func (c *Client) PullRebase(ctx context.Context, repoPath string) error {
	log := logger.Get()
	start := time.Now()
	log.Debug("git pull --rebase starting", "repoPath", repoPath)

	cmd := c.command(ctx, "pull", "--rebase", "--quiet")
	cmd.Dir = repoPath

	output, err := cmd.CombinedOutput()
	log.Debug("git pull --rebase completed", "duration", time.Since(start), "error", err)

	if err != nil {
		return classifyRemoteError(c.remoteLocation(ctx, repoPath), string(output), err)
	}

	return nil
}

// Push pushes changes to the remote repository
func (c *Client) Push(ctx context.Context, repoPath string) error {
	cmd := c.command(ctx, "push", "--quiet")
	cmd.Dir = repoPath

	output, err := cmd.CombinedOutput()
	if err != nil {
		return classifyRemoteError(c.remoteLocation(ctx, repoPath), string(output), err)
	}

	return nil
}

// PushSetUpstream pushes and sets the upstream tracking branch (for first push to empty repos)
func (c *Client) PushSetUpstream(ctx context.Context, repoPath, branch string) error {
	cmd := c.command(ctx, "push", "--quiet", "-u", "origin", branch)
	cmd.Dir = repoPath

	output, err := cmd.CombinedOutput()
	if err != nil {
		return classifyRemoteError(c.remoteLocation(ctx, repoPath), string(output), err)
	}

	return nil
}

// Checkout checks out a specific ref (branch, tag, or commit)
func (c *Client) Checkout(ctx context.Context, repoPath, ref string) error {
	cmd := c.command(ctx, "checkout", "--quiet", ref)
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

	cmd, _, err := c.commandWithURL(ctx, repoURL, "ls-remote")
	if err != nil {
		return "", err
	}

	// Append ref
	cmd.Args = append(cmd.Args, ref)

	output, err := cmd.Output()
	if err != nil {
		var stderr string
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			stderr = string(exitErr.Stderr)
		}
		return "", classifyRemoteError(repoURL, stderr, err)
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 0 || lines[0] == "" {
		return "", fmt.Errorf("ref not found: %s", ref)
	}

	// Parse the output: <commit-hash>\t<ref-name>
	parts := strings.Fields(lines[0])
	if len(parts) < 1 {
		return "", errors.New("invalid git ls-remote output")
	}

	return parts[0], nil
}

// RevParse resolves a ref to a commit hash in a local repository
func (c *Client) RevParse(ctx context.Context, repoPath, ref string) (string, error) {
	cmd := c.command(ctx, "rev-parse", ref)
	cmd.Dir = repoPath

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse failed: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}

// GetRemoteURL returns the remote URL for the repository (typically 'origin')
func (c *Client) GetRemoteURL(ctx context.Context, repoPath string) (string, error) {
	cmd := c.command(ctx, "remote", "get-url", "origin")
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
	cmd := c.command(ctx, "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = repoPath

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git rev-parse failed: %w\nOutput: %s", err, string(output))
	}

	branch := strings.TrimSpace(string(output))
	return branch, nil
}

// GetCurrentBranchSymbolic returns the current branch name using symbolic-ref,
// which works even on empty repos (no commits yet).
func (c *Client) GetCurrentBranchSymbolic(ctx context.Context, repoPath string) (string, error) {
	cmd := c.command(ctx, "symbolic-ref", "--short", "HEAD")
	cmd.Dir = repoPath

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git symbolic-ref failed: %w\nOutput: %s", err, string(output))
	}

	return strings.TrimSpace(string(output)), nil
}

// CheckoutNewBranch creates and switches to a new branch
func (c *Client) CheckoutNewBranch(ctx context.Context, repoPath, branch string) error {
	cmd := c.command(ctx, "checkout", "-b", branch)
	cmd.Dir = repoPath

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git checkout -b failed: %w\nOutput: %s", err, string(output))
	}

	return nil
}

// Add stages files for commit
func (c *Client) Add(ctx context.Context, repoPath string, paths ...string) error {
	args := append([]string{"add"}, paths...)
	cmd := c.command(ctx, args...)
	cmd.Dir = repoPath

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git add failed: %w\nOutput: %s", err, string(output))
	}

	return nil
}

// Commit creates a commit with the given message
func (c *Client) Commit(ctx context.Context, repoPath, message string) error {
	cmd := c.command(ctx, "commit", "-m", message)
	cmd.Dir = repoPath

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git commit failed: %w\nOutput: %s", err, string(output))
	}

	return nil
}

// IsEmpty checks if a repository has no commits (e.g., freshly cloned empty repo)
func (c *Client) IsEmpty(ctx context.Context, repoPath string) (bool, error) {
	cmd := c.command(ctx, "rev-parse", "HEAD")
	cmd.Dir = repoPath

	err := cmd.Run()
	if err != nil {
		// Exit code 128 means no commits (expected for empty repos)
		exitError := &exec.ExitError{}
		if errors.As(err, &exitError) && exitError.ExitCode() == 128 {
			return true, nil
		}
		return false, fmt.Errorf("git rev-parse HEAD failed: %w", err)
	}
	return false, nil
}

// HasStagedChanges checks if there are staged changes ready to be committed
func (c *Client) HasStagedChanges(ctx context.Context, repoPath string) (bool, error) {
	cmd := c.command(ctx, "diff", "--cached", "--quiet")
	cmd.Dir = repoPath

	err := cmd.Run()
	if err != nil {
		// Exit code 1 means there are changes
		exitError := &exec.ExitError{}
		if errors.As(err, &exitError) {
			if exitError.ExitCode() == 1 {
				return true, nil
			}
		}
		return false, fmt.Errorf("git diff failed: %w", err)
	}

	// Exit code 0 means no changes
	return false, nil
}

// remoteLocation returns the best human-readable identifier for a repo in
// error messages: prefer the origin URL (what the user typed/cloned from),
// fall back to the local path. Used by Fetch/Pull/Push so classified errors
// mention something the user recognizes, not a cache-dir hash.
func (c *Client) remoteLocation(ctx context.Context, repoPath string) string {
	cmd := c.command(ctx, "config", "--get", "remote.origin.url")
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err == nil {
		if url := strings.TrimSpace(string(out)); url != "" {
			return url
		}
	}
	return repoPath
}

// classifyRemoteError turns raw git stderr/output into an actionable error.
// It distinguishes "repo not found" from "auth required" so the caller can
// show a useful next-step hint instead of dumping git's raw output.
func classifyRemoteError(repoURL, output string, err error) error {
	authHint := "To authenticate:\n" +
		"  - For private repos over HTTPS: configure a git credential helper\n" +
		"    (e.g. `gh auth setup-git` for GitHub, or `git config --global credential.helper store`)\n" +
		"  - Or use an SSH URL like git@github.com:owner/repo.git\n" +
		"    with your SSH agent running, or pass --ssh-key /path/to/key"

	lc := strings.ToLower(output)
	switch {
	case strings.Contains(lc, "terminal prompts disabled"),
		strings.Contains(lc, "could not read username"),
		strings.Contains(lc, "could not read password"),
		strings.Contains(lc, "authentication failed"),
		strings.Contains(lc, "permission denied (publickey)"):
		return fmt.Errorf("authentication required for %s\nThe repository may be private, or you may not have access.\n%s", repoURL, authHint)
	case strings.Contains(lc, "repository not found"),
		strings.Contains(lc, "does not appear to be a git repository"),
		strings.Contains(lc, "remote: not found"):
		return fmt.Errorf("repository not found: %s\nCheck the URL is correct. If it's a private repo, this can also mean you lack access:\n%s", repoURL, authHint)
	case strings.Contains(lc, "could not resolve host"),
		strings.Contains(lc, "network is unreachable"),
		strings.Contains(lc, "connection refused"),
		strings.Contains(lc, "connection timed out"):
		return fmt.Errorf("network error reaching %s: %s", repoURL, strings.TrimSpace(output))
	}
	if output == "" {
		return fmt.Errorf("git operation failed for %s: %w", repoURL, err)
	}
	return fmt.Errorf("git operation failed for %s: %w\nOutput: %s", repoURL, err, output)
}

// isHexString checks if a string contains only hexadecimal characters
func isHexString(s string) bool {
	const hexChars = "0123456789abcdefABCDEF"
	for _, c := range s {
		if !strings.ContainsRune(hexChars, c) {
			return false
		}
	}
	return true
}
