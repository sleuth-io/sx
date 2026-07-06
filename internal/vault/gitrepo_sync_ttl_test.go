package vault

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sleuth-io/sx/internal/mgmt"
)

// A long-lived GitVault (the desktop app holds one per session) must see
// remote changes eventually: reads within gitSyncTTL serve the local
// clone, and the first read after expiry pulls again.
func TestGitVault_RereadsRemoteAfterTTL(t *testing.T) {
	mgmt.ResetActorCache()
	t.Setenv("SX_CACHE_DIR", t.TempDir())

	remoteDir := filepath.Join(t.TempDir(), "vault.git")
	gitRun(t, "", "init", "--bare", "-b", "main", remoteDir)

	// Writer clone: seeds the vault and later pushes the remote change.
	writerDir := filepath.Join(t.TempDir(), "writer")
	gitRun(t, "", "init", "-b", "main", writerDir)
	gitRun(t, writerDir, "config", "user.email", "writer@example.com")
	gitRun(t, writerDir, "config", "user.name", "Writer")
	if err := os.WriteFile(filepath.Join(writerDir, "sx.toml"), []byte("schema_version = 2\n"), 0o644); err != nil {
		t.Fatalf("write seed manifest: %v", err)
	}
	gitRun(t, writerDir, "add", ".")
	gitRun(t, writerDir, "commit", "-m", "seed")
	gitRun(t, writerDir, "remote", "add", "origin", remoteDir)
	gitRun(t, writerDir, "push", "origin", "main")

	v, err := NewGitVault("file://" + remoteDir)
	if err != nil {
		t.Fatalf("NewGitVault: %v", err)
	}
	ctx := context.Background()
	if err := v.cloneOrUpdate(ctx); err != nil {
		t.Fatalf("initial sync: %v", err)
	}

	// A teammate pushes a manifest change.
	updated := "schema_version = 2\ncreated_by = \"teammate\"\n"
	if err := os.WriteFile(filepath.Join(writerDir, "sx.toml"), []byte(updated), 0o644); err != nil {
		t.Fatalf("update manifest: %v", err)
	}
	gitRun(t, writerDir, "add", ".")
	gitRun(t, writerDir, "commit", "-m", "remote change")
	gitRun(t, writerDir, "push", "origin", "main")

	readManifest := func() string {
		t.Helper()
		if err := v.cloneOrUpdate(ctx); err != nil {
			t.Fatalf("cloneOrUpdate: %v", err)
		}
		data, err := os.ReadFile(filepath.Join(v.repoPath, "sx.toml"))
		if err != nil {
			t.Fatalf("read clone manifest: %v", err)
		}
		return string(data)
	}

	// Inside the TTL the clone is trusted — the remote change is not yet
	// visible (that's the fast path rapid UI reads rely on).
	if got := readManifest(); got == updated {
		t.Fatalf("remote change visible inside TTL; expected the cached clone")
	}

	// Simulate the TTL elapsing; the next read must pull.
	v.syncMu.Lock()
	v.lastSynced = time.Now().Add(-gitSyncTTL - time.Second)
	v.syncMu.Unlock()
	if got := readManifest(); got != updated {
		t.Fatalf("remote change NOT visible after TTL expiry: %q", got)
	}
}
