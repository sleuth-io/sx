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

// republishedRow loads the vault manifest and returns the single row for
// name, failing the test on duplicates or absence.
func republishedRow(t *testing.T, vaultDir, name string) manifest.Asset {
	t.Helper()
	m, ok, err := manifest.Load(vaultDir)
	if err != nil || !ok {
		t.Fatalf("load vault manifest: ok=%v err=%v", ok, err)
	}
	var rows []manifest.Asset
	for _, a := range m.Assets {
		if a.Name == name {
			rows = append(rows, a)
		}
	}
	if len(rows) != 1 {
		t.Fatalf("manifest rows for %s = %d, want exactly 1", name, len(rows))
	}
	return rows[0]
}

// TestRepublishWithReplaceScopeAdvancesManifest covers issue #191: a
// republish routed through the bulk scope installer (--org --replace-scope)
// must advance the manifest row to the just-published version. Before the
// fix, the archive and root view moved to v2 while sx.toml stayed pinned at
// v1 — a silent desync where authors ship a version consumers never receive.
func TestRepublishWithReplaceScopeAdvancesManifest(t *testing.T) {
	env := NewTestEnv(t)
	vaultDir := env.SetupPathVault()
	sourceDir := createSourceSkill(env, "resc-skill")

	if out, err := execAdd(sourceDir, "--yes", "--no-install", "--repo", "git@github.com:org/consumer.git"); err != nil {
		t.Fatalf("add v1: %v\n%s", err, out)
	}
	env.WriteFile(filepath.Join(sourceDir, "SKILL.md"), "You are resc-skill (edited)")
	if out, err := execAdd(sourceDir, "--yes", "--no-install", "--org", "--replace-scope"); err != nil {
		t.Fatalf("republish --org --replace-scope: %v\n%s", err, out)
	}

	row := republishedRow(t, vaultDir, "resc-skill")
	if row.Version != "2" {
		t.Errorf("manifest pins version %q, want \"2\" (archive advanced without the manifest)", row.Version)
	}
	if row.SourcePath == nil || row.SourcePath.Path != ".sx/versions/resc-skill/2" {
		t.Errorf("source path = %+v, want .sx/versions/resc-skill/2", row.SourcePath)
	}
	// --org is the empty scope set: the repo scope is replaced, org-wide.
	if len(row.Scopes) != 0 {
		t.Errorf("scopes = %+v, want none (org-wide)", row.Scopes)
	}
}

// TestRepublishWithAppendScopeAdvancesManifest is the append-mode variant of
// issue #191: `sx add --repo Y` on a new version must bump the row AND end up
// with both the inherited and the newly appended scope.
func TestRepublishWithAppendScopeAdvancesManifest(t *testing.T) {
	env := NewTestEnv(t)
	vaultDir := env.SetupPathVault()
	sourceDir := createSourceSkill(env, "appd-skill")

	if out, err := execAdd(sourceDir, "--yes", "--no-install", "--repo", "git@github.com:org/one.git"); err != nil {
		t.Fatalf("add v1: %v\n%s", err, out)
	}
	env.WriteFile(filepath.Join(sourceDir, "SKILL.md"), "You are appd-skill (edited)")
	if out, err := execAdd(sourceDir, "--yes", "--no-install", "--repo", "git@github.com:org/two.git"); err != nil {
		t.Fatalf("republish --repo two: %v\n%s", err, out)
	}

	row := republishedRow(t, vaultDir, "appd-skill")
	if row.Version != "2" {
		t.Errorf("manifest pins version %q, want \"2\"", row.Version)
	}
	if len(row.Scopes) != 2 {
		t.Fatalf("scopes = %+v, want inherited repo one + appended repo two", row.Scopes)
	}
}
