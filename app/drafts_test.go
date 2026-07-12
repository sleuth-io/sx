package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/mgmt"
	"github.com/sleuth-io/sx/internal/utils"
	vaultpkg "github.com/sleuth-io/sx/internal/vault"
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

// The download path serves the stored archive for the newest revision —
// a real zip whose entries are the skill's files.
func TestLatestAssetZip(t *testing.T) {
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
	a := &App{ctx: context.Background(), vault: v}

	skillZip := zipOf(t, map[string]string{
		"SKILL.md":      "---\nname: docs-tone\ndescription: Tone.\n---\n\n# docs-tone\n",
		"metadata.toml": "[asset]\nname = \"docs-tone\"\nversion = \"1\"\ntype = \"skill\"\ndescription = \"Tone.\"\n\n[skill]\nprompt-file = \"SKILL.md\"\n",
	})
	if err := v.AddAsset(a.ctx, &lockfile.Asset{
		Name: "docs-tone", Version: "1", Type: asset.TypeSkill,
	}, skillZip); err != nil {
		t.Fatalf("AddAsset: %v", err)
	}

	got, err := a.latestAssetZip("docs-tone")
	if err != nil {
		t.Fatalf("latestAssetZip: %v", err)
	}
	content, err := utils.ReadZipFile(got, "SKILL.md")
	if err != nil {
		t.Fatalf("returned bytes are not the asset zip: %v", err)
	}
	if !strings.Contains(string(content), "# docs-tone") {
		t.Fatalf("SKILL.md content = %q", content)
	}

	if _, err := a.latestAssetZip("missing"); err == nil {
		t.Fatalf("want error for unknown asset")
	}
}

// Extension-created drafts (sx.drafts.create) over an existing asset must
// carry TargetAsset — publishing with it unset takes the new-asset branch
// and resets the asset's sharing to everyone.
func TestCreateDraftFromFiles_TargetsExistingAsset(t *testing.T) {
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
	a := &App{ctx: context.Background(), vault: v}

	skillZip := zipOf(t, map[string]string{
		"SKILL.md":      "---\nname: docs-tone\ndescription: Tone.\n---\n\n# docs-tone\n",
		"metadata.toml": "[asset]\nname = \"docs-tone\"\nversion = \"1\"\ntype = \"skill\"\ndescription = \"Tone.\"\n\n[skill]\nprompt-file = \"SKILL.md\"\n",
	})
	if err := v.AddAsset(a.ctx, &lockfile.Asset{
		Name: "docs-tone", Version: "1", Type: asset.TypeSkill,
	}, skillZip); err != nil {
		t.Fatalf("AddAsset: %v", err)
	}

	files := []AssetFile{
		{Path: "SKILL.md", Content: "---\nname: docs-tone\ndescription: Tone.\n---\n\n# docs-tone\n\nUpdated.\n"},
		{Path: "evals/evals.json", Content: `{"evals": []}`},
	}
	d, err := a.CreateDraftFromFiles("docs-tone", files)
	if err != nil {
		t.Fatalf("CreateDraftFromFiles: %v", err)
	}
	if d.TargetAsset != "docs-tone" {
		t.Fatalf("TargetAsset = %q, want docs-tone", d.TargetAsset)
	}

	// A name the vault has never seen stays a new-asset draft.
	fresh, err := a.CreateDraftFromFiles("brand-new-skill", files)
	if err != nil {
		t.Fatalf("CreateDraftFromFiles (new name): %v", err)
	}
	if fresh.TargetAsset != "" {
		t.Fatalf("TargetAsset = %q for a new asset, want empty", fresh.TargetAsset)
	}
}

func runGitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func zipOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, content := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatalf("zip create: %v", err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			t.Fatalf("zip write: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}
