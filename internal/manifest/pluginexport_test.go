package manifest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sleuth-io/sx/internal/asset"
)

func pluginTestManifest() *Manifest {
	return &Manifest{
		SchemaVersion: 2,
		Assets: []Asset{
			{Name: "brand-voice", Version: "1", Type: asset.TypeSkill},
			{Name: "brand-voice", Version: "2", Type: asset.TypeSkill},
			{Name: "release-notes", Version: "1", Type: asset.TypeSkill},
			{Name: "pr-checklist", Version: "1", Type: asset.TypeRule},
		},
		Collections: []Collection{
			{Name: "writing", Description: "Writing helpers", Assets: []string{"brand-voice", "release-notes", "pr-checklist"}},
			{Name: "empty", Assets: []string{"pr-checklist"}},
		},
	}
}

func readJSONFile(t *testing.T, path string, v any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
}

func TestWritePluginManifests(t *testing.T) {
	root := t.TempDir()
	if err := Save(root, pluginTestManifest()); err != nil {
		t.Fatalf("Save: %v", err)
	}

	var claude claudeMarketplace
	readJSONFile(t, filepath.Join(root, claudeMarketplaceFile), &claude)
	slug := filepath.Base(root)
	if claude.Name == "" || claude.Owner.Name == "" {
		t.Errorf("claude marketplace missing name/owner: %+v", claude)
	}
	if len(claude.Plugins) != 2 {
		t.Fatalf("want 2 claude plugins (library + writing), got %d: %+v", len(claude.Plugins), claude.Plugins)
	}
	if claude.Plugins[0].Skills[0] != "./assets" {
		t.Errorf("library plugin should scan ./assets, got %v", claude.Plugins[0].Skills)
	}
	writing := claude.Plugins[1]
	if writing.Name != "writing" || len(writing.Skills) != 2 {
		t.Errorf("writing collection plugin wrong: %+v", writing)
	}
	for _, s := range writing.Skills {
		if s == "./assets/pr-checklist" {
			t.Errorf("rule asset leaked into skills list: %v", writing.Skills)
		}
	}
	_ = slug

	var codexP codexPlugin
	readJSONFile(t, filepath.Join(root, codexPluginFile), &codexP)
	if codexP.Skills != "./assets" || codexP.Version == "" {
		t.Errorf("codex plugin wrong: %+v", codexP)
	}

	var codexM codexMarketplace
	readJSONFile(t, filepath.Join(root, codexMarketplaceFile), &codexM)
	if len(codexM.Plugins) != 1 || codexM.Plugins[0].Source.Path != "./" {
		t.Errorf("codex marketplace wrong: %+v", codexM)
	}
}

func TestWritePluginManifestsRemovedWhenNoSkills(t *testing.T) {
	root := t.TempDir()
	if err := Save(root, pluginTestManifest()); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Re-save with only a rule left: manifests must disappear.
	m := &Manifest{
		SchemaVersion: 2,
		Assets:        []Asset{{Name: "pr-checklist", Version: "1", Type: asset.TypeRule}},
	}
	if err := Save(root, m); err != nil {
		t.Fatalf("Save: %v", err)
	}
	for _, file := range []string{claudeMarketplaceFile, codexPluginFile, codexMarketplaceFile} {
		if _, err := os.Stat(filepath.Join(root, file)); !os.IsNotExist(err) {
			t.Errorf("%s should be removed when the vault has no skills", file)
		}
	}
}

func TestWritePluginManifestsSkippedOnV1Layout(t *testing.T) {
	root := t.TempDir()
	m := pluginTestManifest()
	m.SchemaVersion = 1
	if err := Save(root, m); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, claudeMarketplaceFile)); !os.IsNotExist(err) {
		t.Error("v1 layout must not generate marketplace manifests (assets/ is not directly usable)")
	}
}

func TestVaultSlugFromGitOrigin(t *testing.T) {
	root := t.TempDir()
	gitDir := filepath.Join(root, ".git")
	if err := os.MkdirAll(gitDir, 0755); err != nil {
		t.Fatal(err)
	}
	config := "[core]\n\trepositoryformatversion = 0\n[remote \"origin\"]\n\turl = git@github.com:acme/Skills-Repository.git\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n"
	if err := os.WriteFile(filepath.Join(gitDir, "config"), []byte(config), 0644); err != nil {
		t.Fatal(err)
	}
	if got := vaultSlug(root); got != "skills-repository" {
		t.Errorf("vaultSlug = %q, want skills-repository", got)
	}
}
