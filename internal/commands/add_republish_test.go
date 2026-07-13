package commands

import (
	"path/filepath"
	"testing"

	"github.com/sleuth-io/sx/internal/manifest"
)

// TestRepublishUpdatesManifestRowInPlace covers issue #190: publishing a new
// version of an asset that already exists in the vault must update the
// existing [[assets]] block in place — version bumped, source path repointed,
// scopes preserved — not append a second block for the same name with no
// scopes.
func TestRepublishUpdatesManifestRowInPlace(t *testing.T) {
	env := NewTestEnv(t)
	vaultDir := env.SetupPathVault()
	sourceDir := createSourceSkill(env, "repub-skill")

	// v1, scoped to a repo.
	if out, err := execAdd(sourceDir, "--yes", "--no-install", "--repo", "git@github.com:org/consumer.git"); err != nil {
		t.Fatalf("add v1: %v\n%s", err, out)
	}

	// Edit and republish with no scope flags.
	env.WriteFile(filepath.Join(sourceDir, "SKILL.md"), "You are repub-skill (edited)")
	if out, err := execAdd(sourceDir, "--yes", "--no-install"); err != nil {
		t.Fatalf("republish: %v\n%s", err, out)
	}

	m, ok, err := manifest.Load(vaultDir)
	if err != nil || !ok {
		t.Fatalf("load vault manifest: ok=%v err=%v", ok, err)
	}
	var rows []manifest.Asset
	for _, a := range m.Assets {
		if a.Name == "repub-skill" {
			rows = append(rows, a)
		}
	}
	if len(rows) != 1 {
		t.Fatalf("manifest rows for repub-skill = %d, want exactly 1 (no duplicate [[assets]] blocks)", len(rows))
	}
	row := rows[0]
	if row.Version != "2" {
		t.Errorf("manifest pins version %q, want \"2\"", row.Version)
	}
	if row.SourcePath == nil || row.SourcePath.Path != ".sx/versions/repub-skill/2" {
		t.Errorf("source path = %+v, want .sx/versions/repub-skill/2", row.SourcePath)
	}
	if len(row.Scopes) != 1 || row.Scopes[0].Kind != manifest.ScopeKindRepo {
		t.Fatalf("scopes = %+v, want the original repo scope preserved", row.Scopes)
	}
}
