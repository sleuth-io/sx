package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sleuth-io/sx/internal/config"
)

func TestFetchOrgIconImage(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		switch r.URL.Path {
		case "/icon.png":
			_, _ = w.Write([]byte("\x89PNG icon bytes"))
		case "/huge.png":
			_, _ = w.Write(bytes.Repeat([]byte("x"), maxIconBytes+10))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	cfg := &config.Config{ServerURL: srv.URL, AuthToken: "tok"}
	ctx := context.Background()

	// Relative URL resolves against the server and carries auth.
	data, err := fetchOrgIconImage(ctx, cfg, "/icon.png")
	if err != nil || !strings.Contains(string(data), "icon bytes") {
		t.Fatalf("relative fetch: %v %q", err, data)
	}
	if gotAuth != "Bearer tok" {
		t.Fatalf("same-host auth = %q", gotAuth)
	}

	// Oversized icons are rejected, not cached.
	if _, err := fetchOrgIconImage(ctx, cfg, srv.URL+"/huge.png"); err == nil {
		t.Fatal("expected size-cap error")
	}

	// A different host never receives the token.
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte("\x89PNG cdn"))
	}))
	defer other.Close()
	if _, err := fetchOrgIconImage(ctx, cfg, other.URL+"/i.png"); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "" {
		t.Fatalf("cross-host auth leaked: %q", gotAuth)
	}
}

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
