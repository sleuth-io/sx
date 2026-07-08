package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/utils"
	vaultpkg "github.com/sleuth-io/sx/internal/vault"
)

// exportTestApp is pluginTestApp plus a real path vault, one skill and
// one rule asset published, and a collection holding both — the fixture
// every bundle-format test builds on.
func exportTestApp(t *testing.T) *App {
	t.Helper()
	a := pluginTestApp(t)
	vdir := t.TempDir()
	runGitCmd(t, vdir, "init")
	runGitCmd(t, vdir, "config", "user.email", "alice@example.com")
	runGitCmd(t, vdir, "config", "user.name", "Alice")
	v, err := vaultpkg.NewPathVault("file://" + vdir)
	if err != nil {
		t.Fatalf("NewPathVault: %v", err)
	}
	a.vault = v
	a.ctx = context.Background()

	publishTestAsset(t, a, "code-review", asset.TypeSkill.Key, "SKILL.md",
		"---\nname: code-review\ndescription: Reviews code.\n---\n\nReview $ARGUMENTS carefully.\n")
	publishTestAsset(t, a, "style-rule", asset.TypeRule.Key, "RULE.md",
		"---\nname: style-rule\ndescription: A rule.\n---\n\nAlways use tabs.\n")

	if _, err := a.CreateCollection("review-kit"); err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	if err := a.SetCollectionMembershipBulk("review-kit", []string{"code-review", "style-rule"}, true); err != nil {
		t.Fatalf("SetCollectionMembershipBulk: %v", err)
	}
	return a
}

func publishTestAsset(t *testing.T, a *App, name, typeKey, promptFile, promptMD string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, promptFile), []byte(promptMD), 0o644); err != nil {
		t.Fatal(err)
	}
	draft, err := a.CreateDraftFromPaths([]string{dir})
	if err != nil {
		t.Fatalf("CreateDraftFromPaths(%s): %v", name, err)
	}
	draft.Name = name
	draft.Type = typeKey
	updated, err := a.UpdateDraft(draft)
	if err != nil {
		t.Fatalf("UpdateDraft(%s): %v", name, err)
	}
	if _, err := a.PublishDraft(updated.ID); err != nil {
		t.Fatalf("PublishDraft(%s): %v", name, err)
	}
}

func bundleFiles(t *testing.T, zipData []byte) map[string]bool {
	t.Helper()
	names, err := utils.ListZipFiles(zipData)
	if err != nil {
		t.Fatalf("ListZipFiles: %v", err)
	}
	out := map[string]bool{}
	for _, n := range names {
		out[n] = true
	}
	return out
}

// "zip": every member asset ships, one folder per asset.
func TestBuildCollectionBundleZip(t *testing.T) {
	a := exportTestApp(t)
	zipData, err := a.buildCollectionBundle("review-kit", "zip")
	if err != nil {
		t.Fatalf("buildCollectionBundle: %v", err)
	}
	files := bundleFiles(t, zipData)
	if !files["code-review/SKILL.md"] || !files["style-rule/RULE.md"] {
		t.Fatalf("zip bundle missing asset folders: %v", files)
	}
}

// "claude-code": a Claude Code plugin — .claude-plugin/plugin.json plus
// skills/<asset>/ for SKILL assets only; the rule stays out.
func TestBuildCollectionBundleClaudeCode(t *testing.T) {
	a := exportTestApp(t)
	zipData, err := a.buildCollectionBundle("review-kit", "claude-code")
	if err != nil {
		t.Fatalf("buildCollectionBundle: %v", err)
	}
	files := bundleFiles(t, zipData)
	if !files[".claude-plugin/plugin.json"] {
		t.Fatalf("no plugin.json in bundle: %v", files)
	}
	if !files["skills/code-review/SKILL.md"] {
		t.Fatalf("skill missing from bundle: %v", files)
	}
	for name := range files {
		if strings.Contains(name, "style-rule") {
			t.Fatalf("non-skill asset leaked into plugin bundle: %s", name)
		}
	}

	manifest, err := utils.ReadZipFile(zipData, ".claude-plugin/plugin.json")
	if err != nil {
		t.Fatalf("ReadZipFile: %v", err)
	}
	var pm claudeBundlePlugin
	if err := json.Unmarshal(manifest, &pm); err != nil {
		t.Fatalf("plugin.json not valid JSON: %v", err)
	}
	if pm.Name != "review-kit" || pm.Version != "1.0.0" {
		t.Fatalf("plugin.json = %+v", pm)
	}
}

// "codex": .codex-plugin/plugin.json pointing at ./skills.
func TestBuildCollectionBundleCodex(t *testing.T) {
	a := exportTestApp(t)
	zipData, err := a.buildCollectionBundle("review-kit", "codex")
	if err != nil {
		t.Fatalf("buildCollectionBundle: %v", err)
	}
	files := bundleFiles(t, zipData)
	if !files[".codex-plugin/plugin.json"] || !files["skills/code-review/SKILL.md"] {
		t.Fatalf("codex bundle incomplete: %v", files)
	}
	manifest, _ := utils.ReadZipFile(zipData, ".codex-plugin/plugin.json")
	var pm codexBundlePlugin
	if err := json.Unmarshal(manifest, &pm); err != nil {
		t.Fatalf("plugin.json not valid JSON: %v", err)
	}
	if pm.Skills != "./skills" || pm.Name != "review-kit" {
		t.Fatalf("plugin.json = %+v", pm)
	}
}

// "gemini": gemini-extension.json plus the install-path skill→command
// TOML conversion (sx $ARGUMENTS becomes Gemini {{args}}).
func TestBuildCollectionBundleGemini(t *testing.T) {
	a := exportTestApp(t)
	zipData, err := a.buildCollectionBundle("review-kit", "gemini")
	if err != nil {
		t.Fatalf("buildCollectionBundle: %v", err)
	}
	files := bundleFiles(t, zipData)
	if !files["gemini-extension.json"] || !files["commands/code-review.toml"] {
		t.Fatalf("gemini bundle incomplete: %v", files)
	}
	toml, err := utils.ReadZipFile(zipData, "commands/code-review.toml")
	if err != nil {
		t.Fatalf("ReadZipFile: %v", err)
	}
	if !strings.Contains(string(toml), "{{args}}") {
		t.Fatalf("skill prompt not converted to Gemini syntax:\n%s", toml)
	}
}

func TestBuildCollectionBundleValidation(t *testing.T) {
	a := exportTestApp(t)
	if _, err := a.buildCollectionBundle("review-kit", "tarball"); err == nil {
		t.Fatalf("unknown format accepted")
	}
	if _, err := a.buildCollectionBundle("no-such-collection", "zip"); err == nil {
		t.Fatalf("missing collection accepted")
	}

	// A collection with no skill assets can still export as zip, but the
	// plugin formats refuse plainly.
	if _, err := a.CreateCollection("rules-only"); err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	if err := a.SetCollectionMembershipBulk("rules-only", []string{"style-rule"}, true); err != nil {
		t.Fatalf("SetCollectionMembershipBulk: %v", err)
	}
	if _, err := a.buildCollectionBundle("rules-only", "claude-code"); err == nil ||
		!strings.Contains(err.Error(), "no skill assets") {
		t.Fatalf("skill-less plugin export = %v, want a plain refusal", err)
	}
	if _, err := a.buildCollectionBundle("rules-only", "zip"); err != nil {
		t.Fatalf("zip export of rules-only collection: %v", err)
	}
}
