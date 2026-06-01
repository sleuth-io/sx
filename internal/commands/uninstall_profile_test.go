package commands

import (
	"bytes"
	"path/filepath"
	"sort"
	"testing"

	"github.com/sleuth-io/sx/internal/assets"
)

// `sx uninstall` should only remove assets installed by the current profile.
// An empty profile counts as the current profile (matching
// `vault list --installed`), so untagged leftovers are removed too, but
// another profile's assets are left alone.
func TestFilterUninstallPlanByProfile(t *testing.T) {
	plan := UninstallPlan{
		Assets: []AssetUninstallPlan{
			{Name: "gh-skill", Profile: "gh"},
			{Name: "default-skill", Profile: "default"},
			{Name: "untagged-skill", Profile: ""},
		},
	}

	t.Run("current profile gh keeps gh + empty", func(t *testing.T) {
		got := planNames(filterUninstallPlanByProfile(plan, "gh"))
		want := []string{"gh-skill", "untagged-skill"}
		if !eqStrings(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("current profile default keeps default + empty", func(t *testing.T) {
		got := planNames(filterUninstallPlanByProfile(plan, "default"))
		want := []string{"default-skill", "untagged-skill"}
		if !eqStrings(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
}

// End-to-end: install default-skill under the default profile, plant a gh
// entry in the tracker, then `sx uninstall --all` under default. The default
// (and the on-disk asset) goes; gh's entry is left alone.
func TestUninstallAll_LeavesOtherProfileAssets(t *testing.T) {
	env := NewTestEnv(t)
	setupTwoProfiles(t, env)
	env.Chdir(env.MkdirAll(filepath.Join(env.TempDir, "work")))

	installCmd := NewInstallCommand()
	installCmd.SetOut(&bytes.Buffer{})
	installCmd.SetErr(&bytes.Buffer{})
	if err := installCmd.Execute(); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	// Plant a gh-profile entry that the default-profile uninstall must not touch.
	tracker, err := assets.LoadTracker()
	if err != nil {
		t.Fatalf("LoadTracker: %v", err)
	}
	tracker.UpsertAsset(assets.InstalledAsset{
		Name: "gh-skill", Version: "1.0.0", Type: "skill", Profile: "gh", Clients: []string{"claude-code"},
	})
	if err := assets.SaveTracker(tracker); err != nil {
		t.Fatalf("SaveTracker: %v", err)
	}

	uninstallCmd := NewUninstallCommand()
	uninstallCmd.SetArgs([]string{"--all", "--yes"})
	uninstallCmd.SetOut(&bytes.Buffer{})
	uninstallCmd.SetErr(&bytes.Buffer{})
	if err := uninstallCmd.Execute(); err != nil {
		t.Fatalf("uninstall --all failed: %v", err)
	}

	after, err := assets.LoadTracker()
	if err != nil {
		t.Fatalf("LoadTracker after: %v", err)
	}
	got := profileByName(after.Assets)
	if _, stillThere := got["default-skill"]; stillThere {
		t.Errorf("default-skill should be uninstalled, tracker: %+v", after.Assets)
	}
	if got["gh-skill"] != "gh" {
		t.Errorf("gh-skill should be left alone, tracker: %+v", after.Assets)
	}
}

func planNames(plan UninstallPlan) []string {
	out := make([]string, 0, len(plan.Assets))
	for _, a := range plan.Assets {
		out = append(out, a.Name)
	}
	sort.Strings(out)
	return out
}
