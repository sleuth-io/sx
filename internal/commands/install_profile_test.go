package commands

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sleuth-io/sx/internal/assets"
	"github.com/sleuth-io/sx/internal/config"
)

// setupTwoProfiles writes a multi-profile config with two path vaults,
// "default" (active) and "gh", and seeds one skill into each. Returns the two
// vault dirs.
func setupTwoProfiles(t *testing.T, env *TestEnv) (defaultVault, ghVault string) {
	t.Helper()

	defaultVault = env.MkdirAll(filepath.Join(env.TempDir, "vault-default"))
	ghVault = env.MkdirAll(filepath.Join(env.TempDir, "vault-gh"))

	env.AddSkillToVault(defaultVault, "default-skill", "1.0.0")
	env.WriteLockFile(defaultVault, `
[[assets]]
name = "default-skill"
version = "1.0.0"
type = "skill"

[assets.source-path]
path = "assets/default-skill/1.0.0"
`)

	env.AddSkillToVault(ghVault, "gh-skill", "1.0.0")
	env.WriteLockFile(ghVault, `
[[assets]]
name = "gh-skill"
version = "1.0.0"
type = "skill"

[assets.source-path]
path = "assets/gh-skill/1.0.0"
`)

	configDir := env.MkdirAll(filepath.Join(env.HomeDir, ".config", "sx"))
	cfg := fmt.Sprintf(`{
  "defaultProfile": "default",
  "activeProfiles": ["default"],
  "profiles": {
    "default": {"type": "path", "repositoryUrl": "file://%s"},
    "gh": {"type": "path", "repositoryUrl": "file://%s"}
  }
}`, defaultVault, ghVault)
	env.WriteFile(filepath.Join(configDir, "config.json"), cfg)

	return defaultVault, ghVault
}

//  1. A plain `sx install` stamps each installed asset with the current
//     (default) profile.
func TestInstall_StampsCurrentProfile(t *testing.T) {
	env := NewTestEnv(t)
	setupTwoProfiles(t, env)
	env.Chdir(env.MkdirAll(filepath.Join(env.TempDir, "work")))

	cmd := NewInstallCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	tracker, err := assets.LoadTracker()
	if err != nil {
		t.Fatalf("LoadTracker: %v", err)
	}
	got := profileByName(tracker.Assets)
	if got["default-skill"] != "default" {
		t.Errorf("default-skill profile = %q, want %q (assets: %+v)", got["default-skill"], "default", tracker.Assets)
	}
}

//  2. `sx install --profile gh` stamps the gh profile. We simulate the
//     --profile flag with SetActiveProfile, which is what the flag does.
func TestInstall_ProfileFlagStampsThatProfile(t *testing.T) {
	env := NewTestEnv(t)
	setupTwoProfiles(t, env)
	env.Chdir(env.MkdirAll(filepath.Join(env.TempDir, "work")))

	config.SetActiveProfile("gh")
	t.Cleanup(func() { config.SetActiveProfile("") })

	cmd := NewInstallCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("install --profile gh failed: %v", err)
	}

	tracker, err := assets.LoadTracker()
	if err != nil {
		t.Fatalf("LoadTracker: %v", err)
	}
	got := profileByName(tracker.Assets)
	if got["gh-skill"] != "gh" {
		t.Errorf("gh-skill profile = %q, want %q (assets: %+v)", got["gh-skill"], "gh", tracker.Assets)
	}
}

//  3. `sx vault list --installed` shows assets for the current profile plus
//     untagged (empty-profile) ones, and hides other profiles' assets.
func TestVaultListInstalled_FiltersByCurrentProfile(t *testing.T) {
	env := NewTestEnv(t)
	setupTwoProfiles(t, env)
	env.Chdir(env.MkdirAll(filepath.Join(env.TempDir, "work")))

	if err := assets.SaveTracker(&assets.Tracker{
		Version: assets.TrackerFormatVersion,
		Assets: []assets.InstalledAsset{
			{Name: "default-skill", Version: "1.0.0", Type: "skill", Profile: "default", Clients: []string{"claude-code"}},
			{Name: "gh-skill", Version: "1.0.0", Type: "skill", Profile: "gh", Clients: []string{"claude-code"}},
			{Name: "legacy-skill", Version: "1.0.0", Type: "skill", Profile: "", Clients: []string{"claude-code"}},
		},
	}); err != nil {
		t.Fatalf("seed tracker: %v", err)
	}

	cmd := NewVaultCommand()
	cmd.SetArgs([]string{"list", "--installed"})
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("vault list --installed failed: %v", err)
	}
	out := stdout.String()

	mustContain(t, out, "default-skill") // current profile
	mustContain(t, out, "legacy-skill")  // empty profile = current
	mustNotContain(t, out, "gh-skill")   // other profile hidden
}

func profileByName(in []assets.InstalledAsset) map[string]string {
	m := map[string]string{}
	for _, a := range in {
		m[a.Name] = a.Profile
	}
	return m
}

func mustContain(t *testing.T, s, sub string) {
	t.Helper()
	if !strings.Contains(s, sub) {
		t.Errorf("expected output to contain %q, got:\n%s", sub, s)
	}
}

func mustNotContain(t *testing.T, s, sub string) {
	t.Helper()
	if !strings.Contains(s, sub) {
		return
	}
	t.Errorf("expected output to NOT contain %q, got:\n%s", sub, s)
}
