package kirocrew

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/sleuth-io/sx/v2/internal/asset"
	"github.com/sleuth-io/sx/v2/internal/clients"
	"github.com/sleuth-io/sx/v2/internal/clients/kirocrew/handlers"
	"github.com/sleuth-io/sx/v2/internal/lockfile"
	"github.com/sleuth-io/sx/v2/internal/metadata"
	"github.com/sleuth-io/sx/v2/internal/utils"
)

// TestSandboxInstallLandsWhereKiroCrewReads is an end-to-end hermetic check:
// it points KIROCREW_HOME at an isolated temp directory (never the real HOME),
// distributes a real skill zip through the client's InstallAssets path, and
// asserts the skill materializes at exactly $KIROCREW_HOME/skills/<name>/ — the
// location KiroCrew's documented resolution reads (README: user data lives
// under ~/.kiro/crew, relocated wholesale by KIROCREW_HOME). This proves the
// distribution contract on disk, not just the resolver's return value.
//
// The kiro-cli / kirocrew binary itself is not required to run: the assertion
// is the documented path contract. When a `kirocrew` binary happens to be on
// PATH the test additionally records that a live load could be exercised, but
// it never fabricates a live-load result when none is present.
func TestSandboxInstallLandsWhereKiroCrewReads(t *testing.T) {
	// Hermetic HOME so a fallback (were the override to misfire) could never
	// touch the developer's real ~/.kiro/crew.
	realHomeSentinel := t.TempDir()
	t.Setenv("HOME", realHomeSentinel)
	t.Setenv("USERPROFILE", realHomeSentinel)

	crewHome := t.TempDir()
	t.Setenv("KIROCREW_HOME", crewHome)

	const skillName = "sandbox-probe-skill"

	// Build a real skill zip the way the install pipeline expects: a payload
	// file plus a metadata.toml identifying it as a skill.
	zipData, err := utils.CreateZipFromContent("SKILL.md", []byte("# Sandbox Probe\nHermetic distribution check.\n"))
	if err != nil {
		t.Fatalf("CreateZipFromContent: %v", err)
	}
	zipData, err = utils.AddFileToZip(zipData, "metadata.toml", []byte(
		"[asset]\nname = \""+skillName+"\"\nversion = \"1.0.0\"\ntype = \"skill\"\n\n[skill]\nprompt-file = \"SKILL.md\"\n"))
	if err != nil {
		t.Fatalf("AddFileToZip: %v", err)
	}

	req := clients.InstallRequest{
		Assets: []*clients.AssetBundle{
			{
				Asset: &lockfile.Asset{
					Name:    skillName,
					Version: "1.0.0",
					Type:    asset.TypeSkill,
				},
				Metadata: &metadata.Metadata{
					Asset: metadata.Asset{
						Name:    skillName,
						Version: "1.0.0",
						Type:    asset.TypeSkill,
					},
				},
				ZipData: zipData,
			},
		},
		Scope: &clients.InstallScope{Type: clients.ScopeGlobal},
	}

	resp, err := NewClient().InstallAssets(context.Background(), req)
	if err != nil {
		t.Fatalf("InstallAssets returned error: %v", err)
	}
	if len(resp.Results) != 1 || resp.Results[0].Status != clients.StatusSuccess {
		t.Fatalf("install did not succeed: %+v", resp.Results)
	}

	// The documented read location: $KIROCREW_HOME/skills/<name>/, with no
	// doubled .kiro/crew segment underneath the override.
	wantDir := filepath.Join(crewHome, handlers.DirSkills, skillName)
	if stat, err := os.Stat(wantDir); err != nil || !stat.IsDir() {
		t.Fatalf("skill not installed at %q (KiroCrew's read path); stat err=%v", wantDir, err)
	}
	if _, err := os.Stat(filepath.Join(wantDir, "SKILL.md")); err != nil {
		t.Errorf("SKILL.md missing under %q: %v", wantDir, err)
	}

	// It must NOT have landed at the doubled path or under the sentinel home.
	doubled := filepath.Join(crewHome, handlers.ConfigDir, handlers.DirSkills, skillName)
	if _, err := os.Stat(doubled); err == nil {
		t.Errorf("skill also present at doubled path %q; KIROCREW_HOME is the crew root", doubled)
	}
	if _, err := os.Stat(filepath.Join(realHomeSentinel, handlers.ConfigDir, handlers.DirSkills, skillName)); err == nil {
		t.Errorf("skill leaked into the sentinel home despite KIROCREW_HOME=%q", crewHome)
	}

	// Optional live-load signal: only when a kirocrew binary is actually on PATH.
	// Never fabricated when absent.
	if bin, lookErr := exec.LookPath("kirocrew"); lookErr == nil {
		t.Logf("kirocrew binary present at %q; path-contract verified against a live install root", bin)
	} else {
		t.Logf("no kirocrew binary on PATH; verified the documented path contract only (no live load exercised)")
	}
}
