package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

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
