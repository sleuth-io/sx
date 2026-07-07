package asset

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestInferDescription(t *testing.T) {
	// Frontmatter description wins.
	if got := InferDescription("---\nname: x\ndescription: Use when testing\n---\nbody"); got != "Use when testing" {
		t.Fatalf("frontmatter = %q", got)
	}
	// No frontmatter: first paragraph line, headings skipped.
	if got := InferDescription("# Title\n\nFirst real line.\n"); got != "First real line." {
		t.Fatalf("paragraph = %q", got)
	}
	// Long multi-byte lines truncate on a rune boundary.
	long := strings.Repeat("é", 150)
	got := InferDescription(long)
	if !utf8.ValidString(got) || strings.ContainsRune(got, '�') {
		t.Fatalf("truncation split a rune: %q", got)
	}
	if !strings.HasSuffix(got, "…") || len(got) > 210 {
		t.Fatalf("truncation shape wrong: len=%d", len(got))
	}
}
