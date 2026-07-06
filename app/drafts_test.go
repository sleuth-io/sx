package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Drafts persisted without a type (created while a vault's metadata read
// was failing) must repair themselves on load — a typeless draft can never
// publish ("unknown asset type").
func TestLoadDraft_RepairsMissingType(t *testing.T) {
	t.Setenv("SX_CONFIG_DIR", t.TempDir())
	a := &App{}

	root, err := draftsRoot()
	if err != nil {
		t.Fatalf("draftsRoot: %v", err)
	}
	dir := filepath.Join(root, "docs-tone")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	meta, _ := json.Marshal(draftMeta{Name: "docs-tone", Type: "", TargetAsset: "docs-tone"})
	if err := os.WriteFile(filepath.Join(dir, "draft.json"), meta, 0o644); err != nil {
		t.Fatalf("write draft.json: %v", err)
	}
	skill := "---\nname: docs-tone\ndescription: Tone guide.\n---\n\n# docs-tone\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skill), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}

	d, err := a.GetDraft("docs-tone")
	if err != nil {
		t.Fatalf("GetDraft: %v", err)
	}
	if d.Type != "skill" {
		t.Fatalf("repaired type = %q, want skill", d.Type)
	}
	if d.TypeLabel == "" {
		t.Fatalf("repaired draft has no type label")
	}
}
