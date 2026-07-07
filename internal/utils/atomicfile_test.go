package utils

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestAtomicWriteFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "doc.json")

	if err := AtomicWriteFile(target, []byte(`{"a":1}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != `{"a":1}` {
		t.Fatalf("read = %q, %v", data, err)
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(target)
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("mode = %v, want 0600", info.Mode().Perm())
		}
	}

	// Overwrite replaces content and leaves no temp files behind.
	if err := AtomicWriteFile(target, []byte(`{"a":2}`), 0o644); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	data, _ = os.ReadFile(target)
	if string(data) != `{"a":2}` {
		t.Fatalf("after overwrite = %q", data)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("temp files left behind: %v", entries)
	}

	// A missing parent directory errors rather than silently succeeding.
	if err := AtomicWriteFile(filepath.Join(dir, "nope", "x"), []byte("x"), 0o644); err == nil {
		t.Fatalf("write into missing dir succeeded")
	}
}
