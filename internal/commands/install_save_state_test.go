package commands

import (
	"bytes"
	"testing"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/assets"
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/scope"
)

func TestSaveInstallationState_SkipsFailedAssets(t *testing.T) {
	// Redirect tracker file to temp dir so we don't pollute the real cache
	t.Setenv("SX_CACHE_DIR", t.TempDir())

	tracker := &assets.Tracker{
		Version: "3",
		Assets:  []assets.InstalledAsset{},
	}

	currentScope := &scope.Scope{
		Type: lockfile.ScopeGlobal,
	}

	sortedAssets := []*lockfile.Asset{
		{Name: "skill-ok", Version: "1.0.0", Type: asset.TypeSkill},
		{Name: "skill-fail", Version: "1.0.0", Type: asset.TypeSkill},
		{Name: "skill-existing", Version: "2.0.0", Type: asset.TypeSkill},
	}

	// Only skill-ok and skill-fail were attempted this run
	assetsToInstall := []*lockfile.Asset{
		{Name: "skill-ok", Version: "1.0.0", Type: asset.TypeSkill},
		{Name: "skill-fail", Version: "1.0.0", Type: asset.TypeSkill},
	}

	// Only skill-ok was successfully installed
	installResult := &assets.InstallResult{
		Installed: []string{"skill-ok"},
		Failed:    []string{"skill-fail"},
	}

	cmd := &cobra.Command{}
	cmd.SetErr(&bytes.Buffer{})
	out := newOutputHelper(cmd)

	saveInstallationState(tracker, sortedAssets, assetsToInstall, nil, installResult, currentScope, []string{"claude-code"}, out)

	// Verify: skill-ok should be saved (attempted + succeeded)
	// Verify: skill-fail should NOT be saved (attempted + failed)
	// Verify: skill-existing should be saved (not attempted this run, preserved from lock file)
	savedNames := make(map[string]bool)
	for _, a := range tracker.Assets {
		savedNames[a.Name] = true
	}

	if !savedNames["skill-ok"] {
		t.Error("expected skill-ok to be saved (successfully installed)")
	}
	if savedNames["skill-fail"] {
		t.Error("expected skill-fail to NOT be saved (installation failed)")
	}
	if !savedNames["skill-existing"] {
		t.Error("expected skill-existing to be saved (not attempted, preserved from lock file)")
	}
}

func TestSaveInstallationState_NilInstallResult(t *testing.T) {
	// When nothing was attempted (e.g., handleNothingToInstall), all assets are saved
	t.Setenv("SX_CACHE_DIR", t.TempDir())

	tracker := &assets.Tracker{
		Version: "3",
		Assets:  []assets.InstalledAsset{},
	}

	currentScope := &scope.Scope{
		Type: lockfile.ScopeGlobal,
	}

	sortedAssets := []*lockfile.Asset{
		{Name: "skill-a", Version: "1.0.0", Type: asset.TypeSkill},
		{Name: "skill-b", Version: "2.0.0", Type: asset.TypeSkill},
	}

	cmd := &cobra.Command{}
	cmd.SetErr(&bytes.Buffer{})
	out := newOutputHelper(cmd)

	// nil assetsToInstall and nil installResult (nothing-to-install path)
	saveInstallationState(tracker, sortedAssets, nil, nil, nil, currentScope, []string{"claude-code"}, out)

	if len(tracker.Assets) != 2 {
		t.Errorf("expected 2 assets saved, got %d", len(tracker.Assets))
	}
}
