package publish

import (
	"strings"
	"testing"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/utils"
)

func zipOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var out []byte
	var err error
	first := true
	for name, content := range files {
		if first {
			out, err = utils.CreateZipFromContent(name, []byte(content))
			first = false
		} else {
			out, err = utils.AddFileToZip(out, name, []byte(content))
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	return out
}

// A publish without a metadata description backfills from frontmatter —
// the fix for whole libraries listing "No description yet." while every
// SKILL.md declared one.
func TestBuildMetadataBackfillsDescription(t *testing.T) {
	zipData := zipOf(t, map[string]string{
		"SKILL.md": "---\nname: my-skill\ndescription: Use when testing descriptions\n---\n# My skill\n",
	})
	meta := BuildMetadata("my-skill", "1", asset.TypeSkill, zipData)
	if meta.Asset.Description != "Use when testing descriptions" {
		t.Fatalf("description = %q", meta.Asset.Description)
	}

	// An explicit metadata description always wins.
	withMeta := zipOf(t, map[string]string{
		"SKILL.md":      "---\ndescription: from frontmatter\n---\n",
		"metadata.toml": "[asset]\nname = \"my-skill\"\nversion = \"1\"\ntype = \"skill\"\ndescription = \"from metadata\"\n",
	})
	meta = BuildMetadata("my-skill", "1", asset.TypeSkill, withMeta)
	if meta.Asset.Description != "from metadata" {
		t.Fatalf("metadata description overridden: %q", meta.Asset.Description)
	}

	// SKILL.md wins over alphabetically earlier markdown.
	multi := zipOf(t, map[string]string{
		"AAA.md":   "---\ndescription: wrong file\n---\n",
		"SKILL.md": "---\ndescription: right file\n---\n",
	})
	meta = BuildMetadata("x", "1", asset.TypeSkill, multi)
	if meta.Asset.Description != "right file" {
		t.Fatalf("SKILL.md not preferred: %q", meta.Asset.Description)
	}

	// No frontmatter: first paragraph line, capped.
	prose := zipOf(t, map[string]string{
		"README.md": "# Title\n\n" + strings.Repeat("long ", 60) + "\n",
	})
	meta = BuildMetadata("x", "1", asset.TypeSkill, prose)
	if meta.Asset.Description == "" || len(meta.Asset.Description) > 210 {
		t.Fatalf("paragraph fallback = %q (len %d)", meta.Asset.Description, len(meta.Asset.Description))
	}
}
