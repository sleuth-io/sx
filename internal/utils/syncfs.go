package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Cloud-sync clients (Dropbox, Google Drive, OneDrive, iCloud) drop
// byproducts into synced folders: conflicted copies when two machines
// write concurrently, temp files mid-transfer, and OS junk. Vault
// directory scans must not mistake these for assets or versions.

var osJunkFiles = map[string]bool{
	".DS_Store":   true,
	"Thumbs.db":   true,
	"desktop.ini": true,
	"Icon\r":      true,
}

// IsSyncArtifact reports whether a directory entry name is a cloud-sync
// byproduct rather than user content: conflicted copies, sync temp
// files, and OS junk.
func IsSyncArtifact(name string) bool {
	if osJunkFiles[name] {
		return true
	}
	lower := strings.ToLower(name)
	if strings.Contains(lower, "(conflicted copy") ||
		strings.Contains(lower, "'s conflicted copy") ||
		strings.Contains(lower, "(case conflict") {
		return true
	}
	for _, prefix := range []string{"~$", ".goutputstream-", ".tmp.driveupload", ".tmp.drivedownload"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	for _, suffix := range []string{".icloud", ".crdownload", ".partial"} {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	return strings.HasSuffix(lower, ".dropbox.cache")
}

// numberedCopyRe matches "name (N)" duplicate suffixes that Google
// Drive and others append when resolving name collisions.
var numberedCopyRe = regexp.MustCompile(`^(.+?) \((\d+)\)$`)

// NumberedCopyBase returns the base name if name looks like a "name (N)"
// duplicate (e.g. "foo (1)" -> "foo", true). A bare "(1)" is not a copy.
func NumberedCopyBase(name string) (string, bool) {
	m := numberedCopyRe.FindStringSubmatch(name)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// FindConflictCopies returns sibling files that look like sync-conflict
// copies of the file at path, e.g. for "sx.toml": "sx (Bob's conflicted
// copy 2026-07-04).toml", "sx.toml (1)", "sx (1).toml", "sx 2.toml".
// OneDrive's hostname suffix ("sx-HOSTNAME.toml") is deliberately not
// matched — it is indistinguishable from ordinary hyphenated names.
func FindConflictCopies(path string) ([]string, error) {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to scan %s for conflict copies: %w", dir, err)
	}

	numbered := []*regexp.Regexp{
		// "sx.toml (1)"
		regexp.MustCompile(`^` + regexp.QuoteMeta(base) + ` \(\d+\)$`),
		// "sx (1).toml"
		regexp.MustCompile(`^` + regexp.QuoteMeta(stem) + ` \(\d+\)` + regexp.QuoteMeta(ext) + `$`),
		// "sx 2.toml" (iCloud numbered copy)
		regexp.MustCompile(`^` + regexp.QuoteMeta(stem) + ` \d+` + regexp.QuoteMeta(ext) + `$`),
	}

	var copies []string
	for _, entry := range entries {
		name := entry.Name()
		if name == base || entry.IsDir() {
			continue
		}
		lower := strings.ToLower(name)
		conflicted := (strings.Contains(lower, "conflicted copy") || strings.Contains(lower, "(case conflict")) &&
			strings.HasPrefix(name, stem)
		if !conflicted {
			for _, re := range numbered {
				if re.MatchString(name) {
					conflicted = true
					break
				}
			}
		}
		if conflicted {
			copies = append(copies, name)
		}
	}
	return copies, nil
}
