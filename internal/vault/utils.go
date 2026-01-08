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
