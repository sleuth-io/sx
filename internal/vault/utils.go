package vault

import "bytes"

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
