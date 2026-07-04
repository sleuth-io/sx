package vault

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// seedV2PathVault migrates the standard v1 fixture in place and returns a
// PathVault over it. The resulting vault has assets "chat" (1.0, 2.0) and
// "rules" (1.0) in v2 latest-at-root format.
func seedV2PathVault(t *testing.T, dir string) *PathVault {
	t.Helper()
	seedV1Vault(t, dir)
	if _, err := migrateStorageToV2(dir, "alice@example.com"); err != nil {
		t.Fatal(err)
	}
	v, err := NewPathVault("file://" + dir)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestListAssetsSkipsSyncArtifacts(t *testing.T) {
	dir := t.TempDir()
	v := seedV2PathVault(t, dir)

	// Sync byproducts that must never surface as assets.
	for _, rel := range []string{
		"assets/chat (conflicted copy 2026-07-04)/SKILL.md",
		"assets/chat (1)/SKILL.md", // numbered copy of an existing asset
	} {
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("junk"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", ".DS_Store"), []byte{0}, 0644); err != nil {
		t.Fatal(err)
	}

	// A legitimately named asset with a "(N)" suffix and no sibling base
	// must still be listed.
	writeStandaloneAsset(t, dir, "report (1)")

	list, err := v.ListAssets(context.Background(), ListAssetsOptions{})
	if err != nil {
		t.Fatalf("ListAssets: %v", err)
	}
	var names []string
	for _, a := range list.Assets {
		names = append(names, a.Name)
	}
	want := map[string]bool{"chat": true, "rules": true, "report (1)": true}
	if len(names) != len(want) {
		t.Fatalf("ListAssets = %v, want exactly chat, rules, report (1)", names)
	}
	for _, n := range names {
		if !want[n] {
			t.Errorf("unexpected asset %q in %v", n, names)
		}
	}
}

// writeStandaloneAsset creates a minimal v2-format asset (root view +
// archive + list.txt) directly on disk.
func writeStandaloneAsset(t *testing.T, dir, name string) {
	t.Helper()
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("assets/"+name+"/SKILL.md", "# "+name)
	write(".sx/versions/"+name+"/1.0/SKILL.md", "# "+name)
	write(".sx/versions/"+name+"/list.txt", "1.0\n")
}

func TestGetLockFileErrorsOnConflictedManifest(t *testing.T) {
	dir := t.TempDir()
	v := seedV2PathVault(t, dir)

	conflicted := filepath.Join(dir, "sx (Bob's conflicted copy 2026-07-04).toml")
	if err := os.WriteFile(conflicted, []byte("schema_version = 2\n"), 0644); err != nil {
		t.Fatal(err)
	}

	content, _, _, err := v.GetLockFile(context.Background(), "")
	if err == nil {
		t.Fatalf("GetLockFile should fail with a conflicted manifest present (content=%d bytes)", len(content))
	}
	if !strings.Contains(err.Error(), "sync conflict") || !strings.Contains(err.Error(), "conflicted copy") {
		t.Errorf("error should explain the sync conflict, got: %v", err)
	}

	// Writes are refused too: the check runs under the write lock.
	if _, err := v.acquirePathLock(context.Background()); err == nil {
		t.Error("acquirePathLock should fail with a conflicted manifest present")
	}

	// Resolving the conflict clears both paths.
	if err := os.Remove(conflicted); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := v.GetLockFile(context.Background(), ""); err != nil {
		t.Errorf("GetLockFile after cleanup: %v", err)
	}
}

func TestVersionListToleratesConflictedListFile(t *testing.T) {
	dir := t.TempDir()
	v := seedV2PathVault(t, dir)

	// A conflicted copy of list.txt is ignored (with a warning), never read.
	conflicted := filepath.Join(dir, ".sx", "versions", "chat", "list.txt (1)")
	if err := os.WriteFile(conflicted, []byte("9.9\n"), 0644); err != nil {
		t.Fatal(err)
	}

	versions, err := v.GetVersionList(context.Background(), "chat")
	if err != nil {
		t.Fatalf("GetVersionList: %v", err)
	}
	if strings.Join(versions, ",") != "1.0,2.0" {
		t.Errorf("versions = %v, want [1.0 2.0] from the real list.txt", versions)
	}
}
