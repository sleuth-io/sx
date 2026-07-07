package vault

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sleuth-io/sx/internal/mgmt"
)

func TestPathVaultIconRoundTrip(t *testing.T) {
	dir := t.TempDir()
	v, err := NewPathVault("file://" + dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if data, err := v.GetVaultIcon(ctx); err != nil || data != nil {
		t.Fatalf("empty vault icon = %v, %v", data, err)
	}

	icon := []byte("\x89PNG shared icon")
	if err := v.SetVaultIcon(ctx, icon); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".sx", "vault-icon")); err != nil {
		t.Fatalf("icon not stored in vault: %v", err)
	}
	got, err := v.GetVaultIcon(ctx)
	if err != nil || !bytes.Equal(got, icon) {
		t.Fatalf("round trip = %q, %v", got, err)
	}

	// Empty data removes it — for everyone.
	if err := v.SetVaultIcon(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if data, err := v.GetVaultIcon(ctx); err != nil || data != nil {
		t.Fatalf("after remove = %v, %v", data, err)
	}

	// An oversized shared icon (another client, hand edit) is treated as
	// absent rather than read into memory on every poll.
	if err := os.WriteFile(filepath.Join(dir, ".sx", "vault-icon"), bytes.Repeat([]byte("x"), maxVaultIconBytes+1), 0644); err != nil {
		t.Fatal(err)
	}
	if data, err := v.GetVaultIcon(ctx); err != nil || data != nil {
		t.Fatalf("oversized icon = %d bytes, %v", len(data), err)
	}
}

// One user sets the icon; a second user (own clone) sees it after sync.
func TestGitVaultIconSharedAcrossUsers(t *testing.T) {
	mgmt.ResetActorCache()

	remoteDir := filepath.Join(t.TempDir(), "vault.git")
	gitRun(t, "", "init", "--bare", "-b", "main", remoteDir)
	seedDir := filepath.Join(t.TempDir(), "seed")
	gitRun(t, "", "init", "-b", "main", seedDir)
	gitRun(t, seedDir, "config", "user.email", "seed@example.com")
	gitRun(t, seedDir, "config", "user.name", "Seed")
	if err := os.WriteFile(filepath.Join(seedDir, "README.md"), []byte("seed\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, seedDir, "add", ".")
	gitRun(t, seedDir, "commit", "-m", "seed")
	gitRun(t, seedDir, "remote", "add", "origin", remoteDir)
	gitRun(t, seedDir, "push", "origin", "main")
	repoURL := "file://" + remoteDir
	ctx := context.Background()

	// User A sets the icon.
	t.Setenv("SX_CACHE_DIR", t.TempDir())
	a, err := NewGitVault(repoURL)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.cloneOrUpdate(ctx); err != nil {
		t.Fatalf("clone: %v", err)
	}
	gitRun(t, a.repoPath, "config", "user.email", "alice@example.com")
	gitRun(t, a.repoPath, "config", "user.name", "Alice")
	icon := []byte("\x89PNG team icon")
	if err := a.SetVaultIcon(ctx, icon); err != nil {
		t.Fatalf("SetVaultIcon: %v", err)
	}

	// User B, separate clone cache, syncs and sees it.
	mgmt.ResetActorCache()
	t.Setenv("SX_CACHE_DIR", t.TempDir())
	b, err := NewGitVault(repoURL)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.cloneOrUpdate(ctx); err != nil {
		t.Fatalf("clone B: %v", err)
	}
	got, err := b.GetVaultIcon(ctx)
	if err != nil || !bytes.Equal(got, icon) {
		t.Fatalf("user B icon = %q, %v", got, err)
	}
}
