package asset

import (
	"strings"
	"unicode/utf8"
)

// InferDescription pulls a short description out of markdown content:
// frontmatter description first, else the first non-heading paragraph
// line. One source of truth for every surface that needs a description
// when metadata doesn't carry one (publish backfill, vault listings,
// the app's draft editor).
func InferDescription(content string) string {
	lines := strings.Split(content, "\n")
	inFrontmatter := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if i == 0 && trimmed == "---" {
			inFrontmatter = true
			continue
		}
		if inFrontmatter {
			if trimmed == "---" {
				inFrontmatter = false
				continue
			}
			if desc, ok := strings.CutPrefix(trimmed, "description:"); ok {
				return strings.Trim(strings.TrimSpace(desc), `"'`)
			}
			continue
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if len(trimmed) > 200 {
			// Truncate on a rune boundary — a byte slice can split a
			// multi-byte character and emit a stray replacement char.
			cut := 197
			for cut > 0 && !utf8.RuneStart(trimmed[cut]) {
				cut--
			}
			trimmed = trimmed[:cut] + "…"
		}
		return trimmed
	}
	return ""
}
