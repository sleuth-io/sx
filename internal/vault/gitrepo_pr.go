package vault

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// StartPRBranch puts the git vault into pull-request mode: it syncs the clone,
// creates a fresh branch off the current (default) branch, and records the base
// so FinishPRBranch can push the branch, open the PR, and restore the clone.
//
// While in this mode commitAndPush commits locally without pushing, so the whole
// `sx add` operation accumulates on the branch and lands in a single PR. This is
// the fallback offered when the caller lacks RBAC permission to publish directly
// (see docs/rbac.md and AssetEditPermissionError).
func (g *GitVault) StartPRBranch(ctx context.Context, branch string) error {
	fileLock, err := g.acquireFileLock(ctx)
	if err != nil {
		return fmt.Errorf("failed to acquire lock: %w", err)
	}
	defer func() { _ = fileLock.Unlock() }()

	if err := g.cloneOrUpdate(ctx); err != nil {
		return fmt.Errorf("failed to clone/update repository: %w", err)
	}

	// Resolve the base from the remote's default branch, not the clone's current
	// HEAD: this clone is a long-lived cache that may have been left on some other
	// branch, and opening the PR against that would target the wrong base.
	base, err := g.gitClient.GetDefaultBranch(ctx, g.repoPath)
	if err != nil {
		return fmt.Errorf("failed to determine base branch: %w", err)
	}

	// Start the PR branch from a clean base so it never inherits a leftover branch.
	if err := g.gitClient.Checkout(ctx, g.repoPath, base); err != nil {
		return fmt.Errorf("failed to check out base branch %q: %w", base, err)
	}

	if err := g.gitClient.CheckoutNewBranchForce(ctx, g.repoPath, branch); err != nil {
		return fmt.Errorf("failed to create PR branch: %w", err)
	}

	g.prBranch = branch
	g.prBaseBranch = base
	return nil
}

// AbortPRBranch tears down PR mode without pushing: it clears the in-memory PR
// state and restores the cached clone to its base branch, discarding any
// uncommitted PR changes. It's the cleanup path for a PR add that failed after
// StartPRBranch but before FinishPRBranch — without it the persistent clone is
// left checked out on the PR branch (with a local-only commit), which a later
// cloneOrUpdate would then read from. Safe to call when PR mode isn't active.
func (g *GitVault) AbortPRBranch(ctx context.Context) error {
	fileLock, err := g.acquireFileLock(ctx)
	if err != nil {
		return fmt.Errorf("failed to acquire lock: %w", err)
	}
	defer func() { _ = fileLock.Unlock() }()

	base := g.prBaseBranch
	g.prBranch = ""
	g.prBaseBranch = ""
	if base == "" {
		return nil
	}

	// Discard the staged/working PR changes before switching back, so a dirty
	// tree from the failed add doesn't block (or follow us onto) the base branch.
	_ = g.gitClient.Reset(ctx, g.repoPath, "hard", "HEAD")
	if err := g.gitClient.Checkout(ctx, g.repoPath, base); err != nil {
		return fmt.Errorf("failed to restore base branch: %w", err)
	}
	return nil
}

// PRResult reports the outcome of FinishPRBranch.
type PRResult struct {
	// URL is the pull request URL when Created, otherwise a GitHub compare URL
	// the user can open to create the PR by hand.
	URL string
	// Created is true when a pull request was actually opened (via the gh CLI).
	Created bool
	// Fallback explains, when Created is false, why we couldn't open the PR
	// automatically and fell back to the compare URL.
	Fallback string
}

// FinishPRBranch pushes the PR branch, opens a pull request for it, and returns
// the outcome. It always restores the cached clone to the base branch and clears
// PR mode, so later sx operations don't run against the PR branch.
//
// The PR is created with the `gh` CLI when it is available on PATH; otherwise the
// result carries a GitHub compare URL (and a Fallback reason) for the user to
// open the PR manually.
func (g *GitVault) FinishPRBranch(ctx context.Context, title, body string) (PRResult, error) {
	fileLock, err := g.acquireFileLock(ctx)
	if err != nil {
		return PRResult{}, fmt.Errorf("failed to acquire lock: %w", err)
	}
	defer func() { _ = fileLock.Unlock() }()

	branch := g.prBranch
	base := g.prBaseBranch

	// Always leave the clone on the base branch — a later cloneOrUpdate pulls the
	// checked-out branch, so a lingering PR branch would corrupt subsequent reads.
	defer func() {
		if base != "" {
			_ = g.gitClient.Checkout(ctx, g.repoPath, base)
		}
		g.prBranch = ""
		g.prBaseBranch = ""
	}()

	if branch == "" {
		return PRResult{}, errors.New("no PR branch in progress")
	}

	if err := g.gitClient.PushSetUpstream(ctx, g.repoPath, branch); err != nil {
		return PRResult{}, fmt.Errorf("failed to push PR branch: %w", err)
	}

	return openPullRequest(ctx, g.repoPath, g.repoURL, base, branch, title, body), nil
}

// openPullRequest creates a pull request from head into base using the `gh` CLI
// when available, returning the PR URL it prints. When gh is absent or fails, it
// returns a GitHub compare URL the user can open to create the PR by hand, along
// with the reason it couldn't open the PR automatically.
func openPullRequest(ctx context.Context, repoPath, repoURL, base, head, title, body string) PRResult {
	ghPath, err := exec.LookPath("gh")
	if err != nil {
		return PRResult{
			URL:      compareURL(repoURL, base, head),
			Fallback: "the gh CLI is not installed",
		}
	}

	cmd := exec.CommandContext(ctx, ghPath, ghPRCreateArgs(repoURL, base, head, title, body)...)
	cmd.Dir = repoPath
	out, err := cmd.CombinedOutput()
	if err != nil {
		// gh failed (not authed, not a GitHub remote, PR already exists, …):
		// fall back to the compare URL, surfacing gh's own message as the reason.
		return PRResult{
			URL:      compareURL(repoURL, base, head),
			Fallback: ghFailureReason(string(out), err),
		}
	}

	url := lastURL(string(out))
	if url == "" {
		url = compareURL(repoURL, base, head)
	}
	return PRResult{URL: url, Created: true}
}

// ghPRCreateArgs builds the argument list for `gh pr create`. It MUST pin the
// target repository with --repo so gh resolves it via the GitHub API instead of
// inspecting the local clone: that clone lives under a hidden cache dir
// (~/.cache/sx/git-repos/...), and a snap-confined gh cannot read hidden dirs in
// $HOME, so cwd-based detection fails with "fatal: not a git repository". Pinning
// --repo sidesteps the sandbox entirely. Do not remove --repo — see the
// regression test in gitrepo_pr_test.go.
func ghPRCreateArgs(repoURL, base, head, title, body string) []string {
	args := []string{"pr", "create",
		"--base", base, "--head", head, "--title", title, "--body", body}
	if slug := repoSlug(repoURL); slug != "" {
		args = append(args, "--repo", slug)
	}
	return args
}

// repoSlug reduces a git remote URL to the HOST/OWNER/REPO form that gh's --repo
// flag accepts (e.g. github.com/sleuth-io/skills-repository), or "" when the URL
// can't be reduced. Handles the SSH and https remote forms via webBaseURL.
func repoSlug(repoURL string) string {
	u := webBaseURL(repoURL) // https://host/owner/repo
	u = strings.TrimPrefix(u, "https://")
	u = strings.TrimPrefix(u, "http://")
	return strings.Trim(u, "/")
}

// ghFailureReason distills `gh pr create` failure output into a single-line
// reason, preferring gh's own message over the bare exit error.
func ghFailureReason(out string, err error) string {
	if msg := strings.TrimSpace(out); msg != "" {
		if line, _, ok := strings.Cut(msg, "\n"); ok {
			return strings.TrimSpace(line)
		}
		return msg
	}
	return err.Error()
}

// compareURL builds the GitHub "open a pull request" comparison URL for a branch.
func compareURL(repoURL, base, head string) string {
	return fmt.Sprintf("%s/compare/%s...%s?expand=1", webBaseURL(repoURL), base, head)
}

// webBaseURL converts a git remote URL into its https web base, handling the
// common SSH (git@host:org/repo.git) and https (…/repo.git) forms.
func webBaseURL(repoURL string) string {
	u := strings.TrimSuffix(strings.TrimSpace(repoURL), ".git")
	if rest, ok := strings.CutPrefix(u, "git@"); ok {
		// git@github.com:org/repo -> https://github.com/org/repo
		if host, path, ok := strings.Cut(rest, ":"); ok {
			u = "https://" + host + "/" + path
		}
	}
	u = strings.Replace(u, "ssh://git@", "https://", 1)
	return u
}

// lastURL returns the last http(s) URL found in s, which is what `gh pr create`
// prints on success (the PR URL is the final line of its output).
func lastURL(s string) string {
	var found string
	for f := range strings.FieldsSeq(s) {
		if strings.HasPrefix(f, "http://") || strings.HasPrefix(f, "https://") {
			found = f
		}
	}
	return found
}
