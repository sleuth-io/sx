package git

import (
	"errors"
	"io/fs"
	"os"
	"strings"
	"testing"
)

// Inline key content (SX_SSH_KEY holding the key itself, as CI secrets do)
// must be written once per process, with 0600 permissions and a trailing
// newline, and removed by CleanupTempSSHKeys so the key doesn't outlive sx.
func TestBuildSSHCommand_InlineKeyCachedAndCleanedUp(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	key := "-----BEGIN OPENSSH PRIVATE KEY-----\nabc\n-----END OPENSSH PRIVATE KEY-----"

	first := buildSSHCommand(key)
	second := buildSSHCommand(key)
	if first == "" || first != second {
		t.Fatalf("want one cached temp file for identical key content, got %q then %q", first, second)
	}
	path := strings.TrimPrefix(strings.Fields(first)[2], "")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("temp key file missing: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("temp key perms = %04o, want 0600", info.Mode().Perm())
	}
	data, _ := os.ReadFile(path)
	if string(data) != key+"\n" {
		t.Errorf("temp key content = %q, want key with trailing newline", string(data))
	}

	CleanupTempSSHKeys()
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("temp key file should be removed on cleanup, stat err=%v", err)
	}
	// After cleanup a new command writes a fresh file rather than reusing the deleted path.
	if again := buildSSHCommand(key); again == "" || again == first {
		t.Fatalf("after cleanup want a fresh temp file, got %q (first was %q)", again, first)
	}
	CleanupTempSSHKeys()
}
