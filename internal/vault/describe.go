package vault

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sleuth-io/sx/internal/asset"
)

// maxDescribeRead bounds how much of a markdown file the description
// fallback reads; frontmatter and the first paragraph live at the top.
const maxDescribeRead = 64 << 10

// markdownDescription infers a description from a stored version's
// markdown when its metadata carries none — assets published without a
// metadata description still have one in their frontmatter, and listings
// should say so instead of "no description". Prefers SKILL.md, then the
// alphabetically first top-level markdown file.
func markdownDescription(versionDir string) string {
	entries, err := os.ReadDir(versionDir)
	if err != nil {
		return ""
	}
	var skill, others []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		lower := strings.ToLower(name)
		if !strings.HasSuffix(lower, ".md") && !strings.HasSuffix(lower, ".markdown") {
			continue
		}
		if lower == "skill.md" {
			skill = append(skill, name)
		} else {
			others = append(others, name)
		}
	}
	// SKILL.md first, then the rest alphabetically — sorted even when
	// SKILL.md exists, so the fallback stays deterministic when it
	// yields no description.
	sort.Strings(others)
	candidates := append(skill, others...)
	for _, name := range candidates {
		f, err := os.Open(filepath.Join(versionDir, name))
		if err != nil {
			continue
		}
		buf := make([]byte, maxDescribeRead)
		n, _ := f.Read(buf)
		f.Close()
		if desc := asset.InferDescription(string(buf[:n])); desc != "" {
			return desc
		}
	}
	return ""
}
