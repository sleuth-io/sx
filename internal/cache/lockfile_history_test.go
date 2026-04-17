package cache

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveLockFile_RotatesOnContentChange(t *testing.T) {
	t.Setenv("SX_CACHE_DIR", t.TempDir())

	const vault = "https://vault.example.com/vault-a"

	if err := SaveLockFile(vault, []byte("v1")); err != nil {
		t.Fatalf("first save: %v", err)
	}

	// Second save with identical content must not rotate.
	if err := SaveLockFile(vault, []byte("v1")); err != nil {
		t.Fatalf("second save: %v", err)
	}
	history, err := ListLockFileHistory(vault)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(history) != 0 {
		t.Errorf("unchanged content rotated: %+v", history)
	}

	if err := SaveLockFile(vault, []byte("v2")); err != nil {
		t.Fatalf("third save: %v", err)
	}
	history, err = ListLockFileHistory(vault)
	if err != nil {
		t.Fatalf("history2: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("expected 1 rotation, got %d", len(history))
	}

	rotated, err := os.ReadFile(history[0].Path)
	if err != nil {
		t.Fatalf("read rotated: %v", err)
	}
	if string(rotated) != "v1" {
		t.Errorf("rotated content: got %q want %q", rotated, "v1")
	}

	active, err := LoadLockFile(vault)
	if err != nil {
		t.Fatalf("load active: %v", err)
	}
	if string(active) != "v2" {
		t.Errorf("active content: got %q want %q", active, "v2")
	}

	// Rotated filename encodes a parseable timestamp.
	if !strings.HasSuffix(history[0].Path, ".lock") {
		t.Errorf("rotated path missing .lock suffix: %s", history[0].Path)
	}
	if history[0].Timestamp.IsZero() {
		t.Error("rotation timestamp is zero")
	}
}

func TestListLockFileHistory_Empty(t *testing.T) {
	t.Setenv("SX_CACHE_DIR", t.TempDir())

	got, err := ListLockFileHistory("https://vault.example.com/empty")
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty history, got %+v", got)
	}
}

func TestSaveLockFile_HistoryDirStructure(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SX_CACHE_DIR", dir)

	vault := "https://vault.example.com/repo"
	_ = SaveLockFile(vault, []byte("a"))
	_ = SaveLockFile(vault, []byte("b"))
	_ = SaveLockFile(vault, []byte("c"))

	lockfilesDir := filepath.Join(dir, "lockfiles")
	entries, err := os.ReadDir(lockfilesDir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}

	activeCount := 0
	rotatedCount := 0
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".lock") {
			continue
		}
		if strings.Contains(strings.TrimSuffix(name, ".lock"), "-") && strings.Contains(name, "T") {
			rotatedCount++
		} else {
			activeCount++
		}
	}

	if activeCount != 1 {
		t.Errorf("expected 1 active lock file, got %d", activeCount)
	}
	if rotatedCount != 2 {
		t.Errorf("expected 2 rotated lock files, got %d", rotatedCount)
	}
}
