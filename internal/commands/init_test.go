package commands

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/sx/internal/config"
	"github.com/sleuth-io/sx/internal/utils"
)

// TestPromptForValidGitURL covers the init-time URL validation loop.
// The git remote call is replaced by a stub verifier so we can simulate
// different GitHub responses (success, auth required, not found) without
// running git or hitting the network. This is the test that proves
// `sx init` for github actually checks the response — if anyone disconnects
// the verifier from the prompt loop, these cases fail.
func TestPromptForValidGitURL(t *testing.T) {
	ctx := context.Background()

	t.Run("accepts URL when verifier returns nil", func(t *testing.T) {
		var seen []string
		verify := func(_ context.Context, url string) error {
			seen = append(seen, url)
			return nil
		}
		in := bufio.NewReader(strings.NewReader("git@github.com:foo/bar.git\n"))
		out := &bytes.Buffer{}

		got, err := promptForValidGitURL(ctx, "", in, out, verify)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "git@github.com:foo/bar.git" {
			t.Errorf("got URL %q, want %q", got, "git@github.com:foo/bar.git")
		}
		if len(seen) != 1 || seen[0] != "git@github.com:foo/bar.git" {
			t.Errorf("verifier calls = %v, want exactly one with the entered URL", seen)
		}
	})

	t.Run("retries with new URL when verifier fails and user says yes", func(t *testing.T) {
		var seen []string
		verify := func(_ context.Context, url string) error {
			seen = append(seen, url)
			if url == "https://github.com/foo/bad" {
				return errors.New("repository not found")
			}
			return nil
		}
		// First URL fails, user confirms retry (y), second URL succeeds.
		in := bufio.NewReader(strings.NewReader("https://github.com/foo/bad\ny\nhttps://github.com/foo/good\n"))
		out := &bytes.Buffer{}

		got, err := promptForValidGitURL(ctx, "", in, out, verify)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "https://github.com/foo/good" {
			t.Errorf("got URL %q, want the second one", got)
		}
		if len(seen) != 2 {
			t.Errorf("verifier called %d times, want 2: %v", len(seen), seen)
		}
		if !strings.Contains(out.String(), "Cannot access") {
			t.Errorf("expected user-visible error, got output:\n%s", out.String())
		}
		if !strings.Contains(out.String(), "repository not found") {
			t.Errorf("verifier error not surfaced to user:\n%s", out.String())
		}
	})

	t.Run("aborts when verifier fails and user declines retry", func(t *testing.T) {
		verify := func(_ context.Context, _ string) error {
			return errors.New("authentication required")
		}
		in := bufio.NewReader(strings.NewReader("https://github.com/private/repo\nn\n"))
		out := &bytes.Buffer{}

		_, err := promptForValidGitURL(ctx, "", in, out, verify)
		if err == nil {
			t.Fatal("expected error when user declines retry")
		}
		if !strings.Contains(err.Error(), "aborted") {
			t.Errorf("got error %q, want one mentioning abort", err)
		}
		if !strings.Contains(out.String(), "authentication required") {
			t.Errorf("auth error not surfaced to user:\n%s", out.String())
		}
	})

	t.Run("rejects empty URL", func(t *testing.T) {
		called := false
		verify := func(_ context.Context, _ string) error {
			called = true
			return nil
		}
		in := bufio.NewReader(strings.NewReader("\n"))
		out := &bytes.Buffer{}

		_, err := promptForValidGitURL(ctx, "", in, out, verify)
		if err == nil {
			t.Fatal("expected error for empty URL")
		}
		if called {
			t.Error("verifier should not be called for empty URL")
		}
	})
}

// TestPromptForSharedFolderPath covers the shared-folder location prompt:
// detected sync roots are offered as a menu (with a default vault
// subfolder), "Somewhere else" and no-detection fall back to free-form
// path entry. Components run in non-TTY numbered/readline mode here, so
// input lines are menu numbers and typed paths.
func TestPromptForSharedFolderPath(t *testing.T) {
	detected := []utils.SyncFolder{
		{Provider: "Dropbox", Path: "/home/u/Dropbox"},
		{Provider: "Google Drive", Path: "/home/u/gd/My Drive"},
	}

	t.Run("detected root with default subfolder", func(t *testing.T) {
		// Choose option 1 (Dropbox), accept the suggested subfolder.
		in := bufio.NewReader(strings.NewReader("1\n\n"))
		out := &bytes.Buffer{}
		got, err := promptForSharedFolderPath(detected, "", in, out)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if want := filepath.Join("/home/u/Dropbox", "sx-vault"); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
		if !strings.Contains(out.String(), "Dropbox") || !strings.Contains(out.String(), "Google Drive") {
			t.Error("menu should list detected providers")
		}
	})

	t.Run("somewhere else falls through to typed path", func(t *testing.T) {
		// Option 3 is "Somewhere else", then a typed path.
		in := bufio.NewReader(strings.NewReader("3\n/srv/shared/vault\n"))
		out := &bytes.Buffer{}
		got, err := promptForSharedFolderPath(detected, "", in, out)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "/srv/shared/vault" {
			t.Errorf("got %q, want typed path", got)
		}
	})

	t.Run("no detection goes straight to input", func(t *testing.T) {
		in := bufio.NewReader(strings.NewReader("/srv/shared/vault\n"))
		out := &bytes.Buffer{}
		got, err := promptForSharedFolderPath(nil, "", in, out)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "/srv/shared/vault" {
			t.Errorf("got %q, want typed path", got)
		}
	})

	t.Run("empty path is an error", func(t *testing.T) {
		in := bufio.NewReader(strings.NewReader("\n"))
		out := &bytes.Buffer{}
		if _, err := promptForSharedFolderPath(nil, "", in, out); err == nil {
			t.Fatal("empty path should error")
		}
	})
}

// TestInitNonInteractivePathFlag proves `sx init --type path --path X`
// works (README documented the flag before it existed) and that
// --repo-url is still accepted for back-compat.
func TestInitNonInteractivePathFlag(t *testing.T) {
	ctx := context.Background()
	newCmd := func() *cobra.Command {
		cmd := &cobra.Command{}
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		return cmd
	}

	t.Run("path flag", func(t *testing.T) {
		t.Setenv("SX_CONFIG_DIR", t.TempDir())
		vaultDir := filepath.Join(t.TempDir(), "team-vault")
		if err := runInitNonInteractive(newCmd(), ctx, "path", "", "", vaultDir, nil); err != nil {
			t.Fatalf("init --type path --path: %v", err)
		}
		cfg, err := config.Load()
		if err != nil {
			t.Fatalf("load config: %v", err)
		}
		if cfg.Type != config.RepositoryTypePath || cfg.RepositoryURL != "file://"+vaultDir {
			t.Errorf("config = %s %s, want path vault at %s", cfg.Type, cfg.RepositoryURL, vaultDir)
		}
		if !utils.IsDirectory(vaultDir) {
			t.Error("vault directory should have been created")
		}
	})

	t.Run("repo-url back-compat", func(t *testing.T) {
		t.Setenv("SX_CONFIG_DIR", t.TempDir())
		vaultDir := filepath.Join(t.TempDir(), "compat-vault")
		if err := runInitNonInteractive(newCmd(), ctx, "path", "", vaultDir, "", nil); err != nil {
			t.Fatalf("init --type path --repo-url: %v", err)
		}
		cfg, err := config.Load()
		if err != nil {
			t.Fatalf("load config: %v", err)
		}
		if cfg.RepositoryURL != "file://"+vaultDir {
			t.Errorf("config url = %s, want file://%s", cfg.RepositoryURL, vaultDir)
		}
	})

	t.Run("neither flag errors", func(t *testing.T) {
		t.Setenv("SX_CONFIG_DIR", t.TempDir())
		err := runInitNonInteractive(newCmd(), ctx, "path", "", "", "", nil)
		if err == nil || !strings.Contains(err.Error(), "--path") {
			t.Errorf("err = %v, want mention of --path", err)
		}
	})
}
