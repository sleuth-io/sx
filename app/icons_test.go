package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLibraryIconRoundTrip(t *testing.T) {
	t.Setenv("SX_CONFIG_DIR", t.TempDir())

	if got := libraryIconDataURL("work"); got != "" {
		t.Fatalf("expected no icon, got %q", got)
	}

	dir, err := iconsDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	// Tiny valid PNG header is enough — the app never decodes, only serves.
	if err := os.WriteFile(filepath.Join(dir, "work.png"), []byte("\x89PNG fake"), 0644); err != nil {
		t.Fatal(err)
	}

	got := libraryIconDataURL("work")
	if !strings.HasPrefix(got, "data:image/png;base64,") {
		t.Fatalf("data URL = %q", got)
	}

	removeIconFiles("work")
	if got := libraryIconDataURL("work"); got != "" {
		t.Fatalf("expected icon removed, got %q", got)
	}
}

func TestLibraryIconRejectsUnsafeNames(t *testing.T) {
	t.Setenv("SX_CONFIG_DIR", t.TempDir())
	for _, bad := range []string{"../escape", "a/b", ""} {
		if got := libraryIconFile(bad); got != "" {
			t.Errorf("%q: expected no path, got %q", bad, got)
		}
	}
}
