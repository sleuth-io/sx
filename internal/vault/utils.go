package vault

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/sleuth-io/sx/internal/utils"
)

// parseVersionList parses a newline-separated list of versions from bytes
// This is the standard format for list.txt files across all repository types
func parseVersionList(data []byte) []string {
	var versions []string
	for line := range bytes.SplitSeq(data, []byte("\n")) {
		version := string(bytes.TrimSpace(line))
		if version != "" {
			versions = append(versions, version)
		}
	}
	return versions
}

// filterScanEntries drops directory entries that are not vault content:
// dot-prefixed staging directories, cloud-sync artifacts (conflicted
// copies, sync temp files, OS junk), and "name (N)" duplicates whose
// base name also exists — the shape Google Drive leaves behind after a
// concurrent-write collision. Skipped conflict copies are warned about
// on stderr; nothing is ever deleted. A standalone "foo (1)" with no
// sibling "foo" is kept: it's a legitimately named asset.
func filterScanEntries(entries []os.DirEntry) []os.DirEntry {
	names := make(map[string]bool, len(entries))
	for _, entry := range entries {
		names[entry.Name()] = true
	}
	var kept []os.DirEntry
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if utils.IsSyncArtifact(name) {
			fmt.Fprintf(os.Stderr, "Warning: skipping %q — looks like a cloud-sync artifact. Delete it once the folder has synced.\n", name)
			continue
		}
		if base, ok := utils.NumberedCopyBase(name); ok && names[base] {
			fmt.Fprintf(os.Stderr, "Warning: skipping %q — looks like a sync-conflict copy of %q. If it is, delete it after verifying %q is intact; if it's a real asset, rename it (sx vault rename).\n", name, base, base)
			continue
		}
		kept = append(kept, entry)
	}
	return kept
}

// chunkSlice splits items into contiguous sub-slices of at most size elements.
// An empty input yields no chunks; a non-positive size yields a single chunk.
// Used to bound per-request payloads (audit import, usage POST).
func chunkSlice[T any](items []T, size int) [][]T {
	if len(items) == 0 {
		return nil
	}
	if size <= 0 {
		return [][]T{items}
	}
	chunks := make([][]T, 0, (len(items)+size-1)/size)
	for start := 0; start < len(items); start += size {
		chunks = append(chunks, items[start:min(start+size, len(items))])
	}
	return chunks
}
