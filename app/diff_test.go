package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/mgmt"
	vaultpkg "github.com/sleuth-io/sx/internal/vault"
)

func TestDiffLines_MinimalEdits(t *testing.T) {
	lines := diffLines(
		[]string{"a", "b", "c", "d"},
		[]string{"a", "x", "c", "d", "e"},
	)
	var got []string
	for _, l := range lines {
		got = append(got, l.Kind+":"+l.Text)
	}
	want := []string{"context:a", "del:b", "add:x", "context:c", "context:d", "add:e"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("diff = %v, want %v", got, want)
	}
}

func TestDiffLines_LineNumbers(t *testing.T) {
	lines := diffLines([]string{"a", "b"}, []string{"a", "c"})
	// context a: old 1 / new 1; del b: old 2; add c: new 2.
	if lines[0].OldNo != 1 || lines[0].NewNo != 1 {
		t.Fatalf("context numbering = %+v", lines[0])
	}
	if lines[1].Kind != "del" || lines[1].OldNo != 2 || lines[1].NewNo != 0 {
		t.Fatalf("del numbering = %+v", lines[1])
	}
	if lines[2].Kind != "add" || lines[2].NewNo != 2 || lines[2].OldNo != 0 {
		t.Fatalf("add numbering = %+v", lines[2])
	}
}

// Whatever the shape of the edit, every old line must appear as context or
// del and every new line as context or add, in order.
func TestDiffLines_Reconstructs(t *testing.T) {
	oldLines := []string{"one", "two", "three", "four", "five"}
	newLines := []string{"zero", "one", "three", "3.5", "five", "six"}
	var gotOld, gotNew []string
	for _, l := range diffLines(oldLines, newLines) {
		if l.Kind != "add" {
			gotOld = append(gotOld, l.Text)
		}
		if l.Kind != "del" {
			gotNew = append(gotNew, l.Text)
		}
	}
	if strings.Join(gotOld, ",") != strings.Join(oldLines, ",") {
		t.Fatalf("old side = %v", gotOld)
	}
	if strings.Join(gotNew, ",") != strings.Join(newLines, ",") {
		t.Fatalf("new side = %v", gotNew)
	}
}

func TestSplitLines_NoPhantomTrailingLine(t *testing.T) {
	if got := splitLines("a\nb\n"); len(got) != 2 {
		t.Fatalf("splitLines with trailing newline = %v", got)
	}
	if got := splitLines(""); got != nil {
		t.Fatalf("splitLines(\"\") = %v, want nil", got)
	}
}

func TestHunksFrom_GroupsAndMerges(t *testing.T) {
	// 20 identical lines with changes at 5 and 9 (0-based): close enough
	// (gap of 3 < 2*hunkContext) that they must share one hunk.
	oldLines := make([]string, 20)
	newLines := make([]string, 20)
	for i := range oldLines {
		oldLines[i] = fmt.Sprintf("line %d", i)
		newLines[i] = oldLines[i]
	}
	newLines[5] = "changed 5"
	newLines[9] = "changed 9"

	hunks := hunksFrom(diffLines(oldLines, newLines))
	if len(hunks) != 1 {
		t.Fatalf("hunks = %d, want 1 (merged)", len(hunks))
	}
	h := hunks[0]
	// 3 context above line 5 (old line numbers are 1-based → starts at 3).
	if h.OldStart != 3 || h.NewStart != 3 {
		t.Fatalf("hunk starts at %d/%d, want 3/3", h.OldStart, h.NewStart)
	}
	if h.OldLines != h.NewLines {
		t.Fatalf("hunk sides differ: %d vs %d", h.OldLines, h.NewLines)
	}

	// Push the second change out of merge range: two hunks.
	newLines[9] = oldLines[9]
	newLines[15] = "changed 15"
	if hunks := hunksFrom(diffLines(oldLines, newLines)); len(hunks) != 2 {
		t.Fatalf("hunks = %d, want 2 (split)", len(hunks))
	}
}

func TestDiffFiles_StatusesAndTotals(t *testing.T) {
	base := map[string]string{
		"SKILL.md":      "# title\nold line\n",
		"gone.md":       "bye\n",
		"same.md":       "unchanged\n",
		"metadata.toml": "[asset]\n",
	}
	files := []AssetFile{
		{Path: "SKILL.md", Content: "# title\nnew line\n"},
		{Path: "same.md", Content: "unchanged\n"},
		{Path: "fresh.md", Content: "hi\nthere\n"},
		{Path: "metadata.toml", Content: "[asset]\nchanged\n"},
	}

	d := diffFiles(base, files)
	byPath := map[string]FileDiff{}
	for _, f := range d.Files {
		byPath[f.Path] = f
	}
	if len(d.Files) != 3 {
		t.Fatalf("files = %v", byPath)
	}
	if byPath["SKILL.md"].Status != "modified" {
		t.Fatalf("SKILL.md status = %q", byPath["SKILL.md"].Status)
	}
	if byPath["fresh.md"].Status != "added" || byPath["fresh.md"].Additions != 2 {
		t.Fatalf("fresh.md = %+v", byPath["fresh.md"])
	}
	if byPath["gone.md"].Status != "deleted" || byPath["gone.md"].Deletions != 1 {
		t.Fatalf("gone.md = %+v", byPath["gone.md"])
	}
	// metadata.toml is regenerated on publish and must never show up.
	if _, ok := byPath["metadata.toml"]; ok {
		t.Fatalf("metadata.toml leaked into the diff")
	}
	if d.Additions != 3 || d.Deletions != 2 {
		t.Fatalf("totals = +%d -%d, want +3 -2", d.Additions, d.Deletions)
	}
	// Deterministic order for the file list.
	if d.Files[0].Path != "SKILL.md" || d.Files[1].Path != "fresh.md" {
		t.Fatalf("order = %v", d.Files)
	}
}

// A draft with no target asset diffs against nothing — every file is added
// and no vault access happens.
func TestDiffDraft_NewAsset(t *testing.T) {
	a := newTestApp(t)
	d, err := a.DiffDraft(Draft{
		Name:  "brand-new",
		Files: []AssetFile{{Path: "SKILL.md", Content: "one\ntwo\n"}},
	})
	if err != nil {
		t.Fatalf("DiffDraft: %v", err)
	}
	if len(d.Files) != 1 || d.Files[0].Status != "added" || d.Additions != 2 {
		t.Fatalf("diff = %+v", d)
	}
	if len(d.Files[0].Hunks) != 1 || d.Files[0].Hunks[0].NewStart != 1 {
		t.Fatalf("hunks = %+v", d.Files[0].Hunks)
	}
}

// An update draft diffs against the latest published revision of its
// target asset.
func TestDiffDraft_AgainstPublishedAsset(t *testing.T) {
	t.Setenv("SX_CONFIG_DIR", t.TempDir())
	mgmt.ResetActorCache()
	dir := t.TempDir()
	runGitCmd(t, dir, "init")
	runGitCmd(t, dir, "config", "user.email", "alice@example.com")
	runGitCmd(t, dir, "config", "user.name", "Alice")

	v, err := vaultpkg.NewPathVault("file://" + dir)
	if err != nil {
		t.Fatalf("NewPathVault: %v", err)
	}
	a := newTestAppWithVault(t, v)

	skillZip := zipOf(t, map[string]string{
		"SKILL.md":      "---\nname: docs-tone\n---\n\n# docs-tone\nold body\n",
		"metadata.toml": "[asset]\nname = \"docs-tone\"\nversion = \"1\"\ntype = \"skill\"\ndescription = \"Tone.\"\n\n[skill]\nprompt-file = \"SKILL.md\"\n",
	})
	if err := v.AddAsset(a.ctx, &lockfile.Asset{
		Name: "docs-tone", Version: "1", Type: asset.TypeSkill,
	}, skillZip); err != nil {
		t.Fatalf("AddAsset: %v", err)
	}

	d, err := a.DiffDraft(Draft{
		Name:        "docs-tone",
		TargetAsset: "docs-tone",
		Files: []AssetFile{
			{Path: "SKILL.md", Content: "---\nname: docs-tone\n---\n\n# docs-tone\nnew body\n"},
		},
	})
	if err != nil {
		t.Fatalf("DiffDraft: %v", err)
	}
	if len(d.Files) != 1 || d.Files[0].Status != "modified" {
		t.Fatalf("diff = %+v", d)
	}
	if d.Additions != 1 || d.Deletions != 1 {
		t.Fatalf("totals = +%d -%d, want +1 -1", d.Additions, d.Deletions)
	}
}
