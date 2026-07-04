package utils

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSyncFolderCandidatesPerPlatform(t *testing.T) {
	home := filepath.Join("home", "u")
	tests := []struct {
		goos     string
		provider string
		fragment string
	}{
		{"darwin", "Google Drive", filepath.Join("Library", "CloudStorage", "GoogleDrive-*")},
		{"darwin", "Dropbox", filepath.Join("Library", "CloudStorage", "Dropbox*")},
		{"darwin", "iCloud Drive", "com~apple~CloudDocs"},
		{"windows", "OneDrive", "OneDrive*"},
		{"linux", "Dropbox", "Dropbox"},
	}
	for _, tt := range tests {
		found := false
		for _, c := range syncFolderCandidates(tt.goos, home) {
			if c.Provider == tt.provider && strings.Contains(c.Pattern, tt.fragment) {
				if !strings.HasPrefix(c.Pattern, home) {
					t.Errorf("%s/%s: pattern %q not under home", tt.goos, tt.provider, c.Pattern)
				}
				found = true
			}
		}
		if !found {
			t.Errorf("%s: no %s candidate containing %q", tt.goos, tt.provider, tt.fragment)
		}
	}
}

func TestDetectSyncFoldersIn(t *testing.T) {
	home := t.TempDir()
	mkdir := func(parts ...string) string {
		t.Helper()
		p := filepath.Join(append([]string{home}, parts...)...)
		if err := os.MkdirAll(p, 0755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	myDrive := mkdir("Library", "CloudStorage", "GoogleDrive-user@example.com", "My Drive")
	dropbox := mkdir("Library", "CloudStorage", "Dropbox")
	// A CloudStorage OneDrive entry that is a file, not a dir: skipped.
	if err := os.WriteFile(filepath.Join(home, "Library", "CloudStorage", "OneDrive-x"), []byte{}, 0644); err != nil {
		t.Fatal(err)
	}

	folders := detectSyncFoldersIn("darwin", home)
	got := map[string]string{}
	for _, f := range folders {
		got[f.Provider] = f.Path
	}
	if got["Google Drive"] != myDrive {
		t.Errorf("Google Drive = %q, want My Drive child %q", got["Google Drive"], myDrive)
	}
	if got["Dropbox"] != dropbox {
		t.Errorf("Dropbox = %q, want %q", got["Dropbox"], dropbox)
	}
	if _, ok := got["OneDrive"]; ok {
		t.Error("OneDrive file entry should not be detected as a folder")
	}
	if _, ok := got["iCloud Drive"]; ok {
		t.Error("iCloud Drive should not be detected when absent")
	}
}

func TestDetectSyncFoldersInEmptyHome(t *testing.T) {
	if folders := detectSyncFoldersIn("darwin", t.TempDir()); len(folders) != 0 {
		t.Errorf("folders = %v, want none", folders)
	}
}

func TestProviderForPath(t *testing.T) {
	folders := []SyncFolder{
		{Provider: "Dropbox", Path: filepath.Join("h", "Dropbox")},
		{Provider: "Google Drive", Path: filepath.Join("h", "gd", "My Drive")},
	}
	tests := []struct {
		path string
		want string
	}{
		{filepath.Join("h", "Dropbox", "sx-vault"), "Dropbox"},
		{filepath.Join("h", "Dropbox"), "Dropbox"},
		{filepath.Join("h", "gd", "My Drive", "team", "vault"), "Google Drive"},
		{filepath.Join("h", "DropboxOther", "x"), ""},
		{filepath.Join("h", "elsewhere"), ""},
	}
	for _, tt := range tests {
		if got := ProviderForPath(tt.path, folders); got != tt.want {
			t.Errorf("ProviderForPath(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}
