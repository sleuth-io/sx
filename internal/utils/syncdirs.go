package utils

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// SyncFolder is a detected cloud-synced folder root on this machine.
type SyncFolder struct {
	Provider string // "Google Drive", "Dropbox", "OneDrive", "iCloud Drive"
	Path     string // absolute path to the sync root
}

// syncCandidate is a possible sync root location for a platform. Pattern
// may contain a glob (provider-account suffixes vary per machine).
type syncCandidate struct {
	Provider string
	Pattern  string
}

// syncFolderCandidates returns the candidate sync roots for a platform,
// relative to home, without checking existence. Pure — the test seam for
// DetectSyncFolders.
func syncFolderCandidates(goos, home string) []syncCandidate {
	j := func(parts ...string) string {
		return filepath.Join(append([]string{home}, parts...)...)
	}
	switch goos {
	case "darwin":
		return []syncCandidate{
			{"Google Drive", j("Library", "CloudStorage", "GoogleDrive-*")},
			{"Dropbox", j("Library", "CloudStorage", "Dropbox*")},
			{"Dropbox", j("Dropbox")},
			{"OneDrive", j("Library", "CloudStorage", "OneDrive*")},
			{"iCloud Drive", j("Library", "Mobile Documents", "com~apple~CloudDocs")},
		}
	case "windows":
		return []syncCandidate{
			{"OneDrive", j("OneDrive*")},
			{"Dropbox", j("Dropbox")},
			{"Google Drive", j("Google Drive")},
			{"Google Drive", j("My Drive")},
		}
	default: // linux and friends
		return []syncCandidate{
			{"Dropbox", j("Dropbox")},
			{"OneDrive", j("OneDrive")},
			{"Google Drive", j("Google Drive")},
		}
	}
}

// DetectSyncFolders returns the cloud-sync roots that exist on this
// machine, in a stable provider order with duplicates removed.
func DetectSyncFolders() []SyncFolder {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return detectSyncFoldersIn(runtime.GOOS, home)
}

// detectSyncFoldersIn is DetectSyncFolders with the platform and home
// directory injected for tests.
func detectSyncFoldersIn(goos, home string) []SyncFolder {
	var folders []SyncFolder
	seen := map[string]bool{}
	for _, c := range syncFolderCandidates(goos, home) {
		matches, err := filepath.Glob(c.Pattern)
		if err != nil {
			continue
		}
		for _, match := range matches {
			if !IsDirectory(match) || seen[match] {
				continue
			}
			// Google Drive for desktop mounts the account root; the
			// usable folder is its "My Drive" child.
			if c.Provider == "Google Drive" {
				if myDrive := filepath.Join(match, "My Drive"); IsDirectory(myDrive) {
					match = myDrive
					if seen[match] {
						continue
					}
				}
			}
			seen[match] = true
			folders = append(folders, SyncFolder{Provider: c.Provider, Path: match})
		}
	}
	return folders
}

// ProviderForPath returns the provider whose sync root contains path,
// or "" if none does.
func ProviderForPath(path string, folders []SyncFolder) string {
	for _, f := range folders {
		if path == f.Path || strings.HasPrefix(path, f.Path+string(os.PathSeparator)) {
			return f.Provider
		}
	}
	return ""
}
