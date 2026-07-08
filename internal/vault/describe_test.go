package vault

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMarkdownDescriptionFallback(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Empty dir → nothing.
	if got := markdownDescription(dir); got != "" {
		t.Fatalf("empty dir = %q", got)
	}

	// SKILL.md preferred over other markdown.
	write("aaa.md", "---\ndescription: not this one\n---\n")
	write("SKILL.md", "---\nname: x\ndescription: Use when checking fallbacks\n---\n# X\n")
	if got := markdownDescription(dir); got != "Use when checking fallbacks" {
		t.Fatalf("got %q", got)
	}

	// Without SKILL.md, alphabetical order decides.
	if err := os.Remove(filepath.Join(dir, "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	write("bbb.md", "---\ndescription: later file\n---\n")
	if got := markdownDescription(dir); got != "not this one" {
		t.Fatalf("got %q", got)
	}
}
