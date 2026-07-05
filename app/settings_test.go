package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestGhErrorMessageExtractsAPIDetail(t *testing.T) {
	// gh api puts the JSON error body on stdout and the summary on stderr.
	stdout := `{"message":"Repository creation failed.","errors":[{"resource":"Repository","code":"custom","field":"name","message":"name already exists on this account"}],"status":"422"}`
	err := &exec.ExitError{Stderr: []byte("gh: Repository creation failed. (HTTP 422)")}
	if got := ghErrorMessage([]byte(stdout), err); got != "name already exists on this account" {
		t.Fatalf("got %q", got)
	}
	// No errors array: the body's top-level message.
	if got := ghErrorMessage([]byte(`{"message":"Must have admin rights to Repository."}`), err); got != "Must have admin rights to Repository." {
		t.Fatalf("got %q", got)
	}
	// No JSON body at all: the stderr summary line.
	err = &exec.ExitError{Stderr: []byte("gh: Not Found (HTTP 404)")}
	if got := ghErrorMessage(nil, err); got != "Not Found (HTTP 404)" {
		t.Fatalf("got %q", got)
	}
}

func TestGithubRepoPattern(t *testing.T) {
	cases := []struct {
		url         string
		owner, repo string
	}{
		{"https://github.com/acme/skills.git", "acme", "skills"},
		{"https://github.com/acme/skills", "acme", "skills"},
		{"https://github.com/acme/skills/", "acme", "skills"},
		{"git@github.com:acme/my.skills.git", "acme", "my.skills"},
		{"git@github.com:acme/skills", "acme", "skills"},
		{"https://gitlab.com/acme/skills.git", "", ""},
		{"file:///tmp/vault", "", ""},
		{"https://github.com/acme", "", ""},
	}
	for _, c := range cases {
		m := githubRepoPattern.FindStringSubmatch(c.url)
		if c.owner == "" {
			if m != nil {
				t.Errorf("%s: expected no match, got %v", c.url, m)
			}
			continue
		}
		if m == nil {
			t.Errorf("%s: expected match", c.url)
			continue
		}
		if m[1] != c.owner || m[2] != c.repo {
			t.Errorf("%s: got %s/%s, want %s/%s", c.url, m[1], m[2], c.owner, c.repo)
		}
	}
}

func TestDeleteVaultFolderRefusesNonVaults(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "important.txt"), []byte("keep me"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := deleteVaultFolder(dir); err == nil {
		t.Fatal("deleted a folder with no sx.toml")
	}
	if _, err := os.Stat(filepath.Join(dir, "important.txt")); err != nil {
		t.Fatal("refusal must leave contents untouched")
	}
}

func TestDeleteVaultFolderDeletesRealVault(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sx.toml"), []byte("schema_version = 2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "assets", "x"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := deleteVaultFolder(dir); err != nil {
		t.Fatalf("deleteVaultFolder: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("vault folder still exists")
	}
}

func TestDeleteVaultFolderEdges(t *testing.T) {
	// Empty dir: deletable (a never-written library).
	empty := t.TempDir()
	if err := deleteVaultFolder(empty); err != nil {
		t.Fatalf("empty dir: %v", err)
	}
	// Already gone: fine.
	if err := deleteVaultFolder(filepath.Join(empty, "nope")); err != nil {
		t.Fatalf("missing dir: %v", err)
	}
	// Suspicious targets: refused.
	for _, bad := range []string{"", "/", "relative/path"} {
		if err := deleteVaultFolder(bad); err == nil {
			t.Errorf("%q: expected refusal", bad)
		}
	}
}
