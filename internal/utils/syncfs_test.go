package utils

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsSyncArtifact(t *testing.T) {
	artifacts := []string{
		"foo (conflicted copy 2026-07-04)",
		"sx (Bob's conflicted copy 2026-07-04).toml",
		"notes (Case Conflict 1)",
		".DS_Store",
		"Thumbs.db",
		"desktop.ini",
		"~$report.docx",
		".goutputstream-ABC123",
		".tmp.driveupload",
		".tmp.drivedownload-4321",
		"photo.jpg.icloud",
		"setup.exe.crdownload",
		"video.mp4.partial",
	}
	for _, name := range artifacts {
		if !IsSyncArtifact(name) {
			t.Errorf("IsSyncArtifact(%q) = false, want true", name)
		}
	}

	content := []string{
		"foo",
		"foo (1)", // numbered copies need sibling context, handled elsewhere
		"my skill (v2)",
		"conflicted-copy-notes",
		"partial-results",
		"dropbox-integration",
	}
	for _, name := range content {
		if IsSyncArtifact(name) {
			t.Errorf("IsSyncArtifact(%q) = true, want false", name)
		}
	}
}

func TestNumberedCopyBase(t *testing.T) {
	tests := []struct {
		name string
		base string
		ok   bool
	}{
		{"foo (1)", "foo", true},
		{"foo (12)", "foo", true},
		{"my skill (2)", "my skill", true},
		{"foo", "", false},
		{"foo (v2)", "", false},
		{"(1)", "", false},
		{"foo(1)", "", false},
	}
	for _, tt := range tests {
		base, ok := NumberedCopyBase(tt.name)
		if base != tt.base || ok != tt.ok {
			t.Errorf("NumberedCopyBase(%q) = (%q, %v), want (%q, %v)", tt.name, base, ok, tt.base, tt.ok)
		}
	}
}

func TestFindConflictCopies(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "sx.toml")
	files := []string{
		"sx.toml",
		// Conflict copies of sx.toml:
		"sx (Bob's conflicted copy 2026-07-04).toml",
		"sx.toml (1)",
		"sx (2).toml",
		"sx 2.toml",
		// Not conflict copies:
		"sx-backup.toml",
		"sx-DESKTOP-ABC.toml", // OneDrive hostname suffix: deliberately unmatched
		"other.toml",
		"sxfoo.toml (1)",
	}
	for _, name := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	copies, err := FindConflictCopies(target)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"sx (Bob's conflicted copy 2026-07-04).toml": true,
		"sx.toml (1)": true,
		"sx (2).toml": true,
		"sx 2.toml":   true,
	}
	if len(copies) != len(want) {
		t.Fatalf("copies = %v, want %d entries", copies, len(want))
	}
	for _, c := range copies {
		if !want[c] {
			t.Errorf("unexpected conflict copy %q", c)
		}
	}
}

func TestFindConflictCopiesClean(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "list.txt"), []byte("1.0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	copies, err := FindConflictCopies(filepath.Join(dir, "list.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if len(copies) != 0 {
		t.Errorf("copies = %v, want none", copies)
	}
}

func TestFindConflictCopiesMissingDir(t *testing.T) {
	copies, err := FindConflictCopies(filepath.Join(t.TempDir(), "nope", "sx.toml"))
	if err != nil || copies != nil {
		t.Errorf("missing dir: copies = %v, err = %v; want nil, nil", copies, err)
	}
}
