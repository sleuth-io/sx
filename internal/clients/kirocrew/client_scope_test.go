package kirocrew

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sleuth-io/sx/v2/internal/asset"
	"github.com/sleuth-io/sx/v2/internal/clients"
	"github.com/sleuth-io/sx/v2/internal/clients/kirocrew/handlers"
	"github.com/sleuth-io/sx/v2/internal/lockfile"
	"github.com/sleuth-io/sx/v2/internal/metadata"
	"github.com/sleuth-io/sx/v2/internal/utils"
)

// scopeProbeSkill is the skill name every case below distributes.
const scopeProbeSkill = "scope-probe-skill"

// newScopeProbeBundle builds a real skill zip plus its bundle, the same shape
// the install pipeline hands a client.
func newScopeProbeBundle(t *testing.T) *clients.AssetBundle {
	t.Helper()

	zipData, err := utils.CreateZipFromContent("SKILL.md", []byte("# Scope Probe\n"))
	if err != nil {
		t.Fatalf("CreateZipFromContent: %v", err)
	}
	zipData, err = utils.AddFileToZip(zipData, "metadata.toml", []byte(
		"[asset]\nname = \""+scopeProbeSkill+"\"\nversion = \"1.0.0\"\ntype = \"skill\"\n\n[skill]\nprompt-file = \"SKILL.md\"\n"))
	if err != nil {
		t.Fatalf("AddFileToZip: %v", err)
	}

	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: scopeProbeSkill, Version: "1.0.0", Type: asset.TypeSkill},
	}
	return &clients.AssetBundle{
		Asset:    &lockfile.Asset{Name: scopeProbeSkill, Version: "1.0.0", Type: asset.TypeSkill},
		Metadata: meta,
		ZipData:  zipData,
	}
}

// TestNonGlobalScopeInstallSkipsAndWritesNothing is the on-disk half of the
// global-only ruling: KiroCrew resolves its crew skills root from its single
// data home and never discovers a repo-local .kiro/crew, so a repo- or
// path-scoped install must report a skip and leave the filesystem alone. The
// assertion that matters is the absence of <repo>/.kiro — the tree the previous
// behavior created and KiroCrew would never have read.
func TestNonGlobalScopeInstallSkipsAndWritesNothing(t *testing.T) {
	cases := []struct {
		name  string
		scope *clients.InstallScope
	}{
		{"repo scope", &clients.InstallScope{Type: clients.ScopeRepository}},
		{"path scope", &clients.InstallScope{Type: clients.ScopePath, Path: filepath.Join("services", "api")}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			homeSentinel := t.TempDir()
			t.Setenv("HOME", homeSentinel)
			t.Setenv("USERPROFILE", homeSentinel)
			crewHome := t.TempDir()
			t.Setenv("KIROCREW_HOME", crewHome)

			repoRoot := t.TempDir()
			scope := *tc.scope
			scope.RepoRoot = repoRoot

			resp, err := NewClient().InstallAssets(context.Background(), clients.InstallRequest{
				Assets: []*clients.AssetBundle{newScopeProbeBundle(t)},
				Scope:  &scope,
			})
			if err != nil {
				t.Fatalf("InstallAssets returned a hard error; a skip was expected: %v", err)
			}
			if len(resp.Results) != 1 {
				t.Fatalf("got %d results, want 1 (one per requested asset): %+v", len(resp.Results), resp.Results)
			}
			if resp.Results[0].Status != clients.StatusSkipped {
				t.Errorf("status = %q, want %q", resp.Results[0].Status, clients.StatusSkipped)
			}
			if !strings.Contains(resp.Results[0].Message, "global") {
				t.Errorf("message %q does not explain the global-only restriction", resp.Results[0].Message)
			}

			// Nothing may be written into the repository tree.
			if _, err := os.Stat(filepath.Join(repoRoot, ".kiro")); err == nil {
				t.Errorf("install created .kiro under the repo root %q; KiroCrew never reads a repo-local crew dir", repoRoot)
			}
			// Nor may it silently fall back to the global crew root.
			if _, err := os.Stat(filepath.Join(crewHome, handlers.DirSkills, scopeProbeSkill)); err == nil {
				t.Errorf("non-global install fell back to the global crew root %q", crewHome)
			}
		})
	}
}

// TestNonGlobalScopeVerifyReportsNotInstalled covers the verify side. A
// clients.VerifyResult carries no status field, so the honest reading of a
// non-global scope is "not installed", with the reason in the message.
func TestNonGlobalScopeVerifyReportsNotInstalled(t *testing.T) {
	homeSentinel := t.TempDir()
	t.Setenv("HOME", homeSentinel)
	t.Setenv("USERPROFILE", homeSentinel)
	t.Setenv("KIROCREW_HOME", t.TempDir())

	assets := []*lockfile.Asset{{Name: scopeProbeSkill, Version: "1.0.0", Type: asset.TypeSkill}}
	results := NewClient().VerifyAssets(context.Background(), assets, &clients.InstallScope{
		Type:     clients.ScopeRepository,
		RepoRoot: t.TempDir(),
	})

	if len(results) != 1 {
		t.Fatalf("got %d results, want 1: %+v", len(results), results)
	}
	if results[0].Installed {
		t.Error("Installed = true for a repo-scoped asset KiroCrew cannot read")
	}
	if !strings.Contains(results[0].Message, "global") {
		t.Errorf("message %q does not explain the global-only restriction", results[0].Message)
	}
}
