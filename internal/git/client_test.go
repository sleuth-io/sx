package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestLsRemote_ClassifiesRealGitErrors runs real `git ls-remote` against
// failing targets and verifies the error goes through classifyRemoteError —
// i.e. the user sees a friendly message ("repository not found",
// "authentication required") rather than raw git output. This is the
// regression guard for the silent-hang bug: if the classifier ever gets
// disconnected from the call path, these tests fail.
func TestLsRemote_ClassifiesRealGitErrors(t *testing.T) {
	client := &Client{}
	ctx := context.Background()

	t.Run("nonexistent local path is classified as not-found", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "does-not-exist")
		_, err := client.LsRemote(ctx, missing, "HEAD")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		msg := err.Error()
		// Strict assertion: must hit the "not found" branch of the
		// classifier. The fallback branch would mean classification
		// silently broke — if a new git version phrases the error
		// differently, fix the classifier substring list, not the test.
		if !strings.Contains(msg, "repository not found") {
			t.Errorf("error not classified as not-found: %s", msg)
		}
		if !strings.Contains(msg, missing) {
			t.Errorf("error should mention the URL %q, got: %s", missing, msg)
		}
	})

	t.Run("bogus hostname is classified as network error", func(t *testing.T) {
		if testing.Short() {
			t.Skip("skipping DNS-touching test in short mode")
		}
		_, err := client.LsRemote(ctx, "https://invalid.localhost.invalid/x.git", "HEAD")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		msg := err.Error()
		if !strings.Contains(msg, "network error") {
			t.Errorf("error not classified as network error: %s", msg)
		}
	})
}

func TestIsEmpty(t *testing.T) {
	t.Run("empty cloned repo", func(t *testing.T) {
		dir := t.TempDir()
		bareDir := filepath.Join(dir, "bare.git")
		cloneDir := filepath.Join(dir, "clone")

		// Create a bare empty remote
		runGit(t, "", "init", "--bare", bareDir)

		// Clone it (produces empty local repo)
		runGit(t, "", "clone", bareDir, cloneDir)

		client := &Client{}
		empty, err := client.IsEmpty(context.Background(), cloneDir)
		if err != nil {
			t.Fatalf("IsEmpty() error: %v", err)
		}
		if !empty {
			t.Error("IsEmpty() = false, want true for empty cloned repo")
		}
	})

	t.Run("repo with commits", func(t *testing.T) {
		dir := t.TempDir()
		repoDir := filepath.Join(dir, "repo")

		runGit(t, "", "init", repoDir)
		runGit(t, repoDir, "config", "user.email", "test@test.com")
		runGit(t, repoDir, "config", "user.name", "Test")

		if err := os.WriteFile(filepath.Join(repoDir, "file.txt"), []byte("hello"), 0644); err != nil {
			t.Fatal(err)
		}
		runGit(t, repoDir, "add", ".")
		runGit(t, repoDir, "commit", "-m", "init")

		client := &Client{}
		empty, err := client.IsEmpty(context.Background(), repoDir)
		if err != nil {
			t.Fatalf("IsEmpty() error: %v", err)
		}
		if empty {
			t.Error("IsEmpty() = true, want false for repo with commits")
		}
	})
}

func TestPullEmptyRepo(t *testing.T) {
	dir := t.TempDir()
	bareDir := filepath.Join(dir, "bare.git")
	cloneDir := filepath.Join(dir, "clone")

	// Create a bare empty remote and clone it
	runGit(t, "", "init", "--bare", bareDir)
	runGit(t, "", "clone", bareDir, cloneDir)

	client := &Client{}

	// Pull on empty repo should fail (this is the bug we're protecting against)
	err := client.Pull(context.Background(), cloneDir)
	if err == nil {
		t.Error("Pull() on empty repo should fail")
	}
}

func TestPushSetUpstream(t *testing.T) {
	dir := t.TempDir()
	bareDir := filepath.Join(dir, "bare.git")
	cloneDir := filepath.Join(dir, "clone")

	// Create a bare empty remote and clone it
	runGit(t, "", "init", "--bare", bareDir)
	runGit(t, "", "clone", bareDir, cloneDir)
	runGit(t, cloneDir, "config", "user.email", "test@test.com")
	runGit(t, cloneDir, "config", "user.name", "Test")

	// Create a commit
	if err := os.WriteFile(filepath.Join(cloneDir, "file.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, cloneDir, "add", ".")
	runGit(t, cloneDir, "commit", "-m", "first commit")

	client := &Client{}

	// PushSetUpstream should succeed on first push to empty remote
	branch, err := client.GetCurrentBranch(context.Background(), cloneDir)
	if err != nil {
		t.Fatalf("GetCurrentBranch() error: %v", err)
	}

	err = client.PushSetUpstream(context.Background(), cloneDir, branch)
	if err != nil {
		t.Fatalf("PushSetUpstream() error: %v", err)
	}

	// Now the repo should not be empty
	empty, err := client.IsEmpty(context.Background(), cloneDir)
	if err != nil {
		t.Fatalf("IsEmpty() error: %v", err)
	}
	if empty {
		t.Error("IsEmpty() = true after push, want false")
	}
}

func TestWithSSHKeyOverridesGlobal(t *testing.T) {
	// Snapshot and restore the global so other tests aren't affected by
	// our temporary override.
	prev := globalSSHKeyPath
	t.Cleanup(func() { globalSSHKeyPath = prev })
	globalSSHKeyPath = "/from/global"

	defaultClient := NewClientWithOptions()
	if defaultClient.sshKeyPath != "/from/global" {
		t.Fatalf("NewClientWithOptions sshKeyPath = %q, want global value", defaultClient.sshKeyPath)
	}

	overridden := NewClientWithOptions(WithSSHKey("/from/option"))
	if overridden.sshKeyPath != "/from/option" {
		t.Fatalf("WithSSHKey did not override global: got %q", overridden.sshKeyPath)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
}
