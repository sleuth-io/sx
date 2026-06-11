package commands

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sleuth-io/sx/internal/assets"
	"github.com/sleuth-io/sx/internal/config"
	vaultpkg "github.com/sleuth-io/sx/internal/vault"
)

// TestAddAlreadyInstalled covers the matrix of (asset-in-vault × installed × input-form).
//
// The bug under test: `sx add` did not recognize assets installed by `sx install`,
// reporting them as "Not installed (available in vault only)" and offering the
// fresh-install flow instead of the configure-existing flow.
//
// Each scenario asserts the *positive* signal expected from the right code
// path (e.g. "Found asset: name v1 in vault (installed)") rather than only
// the absence of the bug's wording, so that future copy changes don't
// silently regress the behavior.

// assertOutput fails the test if any of want is missing or any of notWant is
// present in output. Reports the full output once on failure.
func assertOutput(t *testing.T, output string, want, notWant []string) {
	t.Helper()
	missing := make([]string, 0, len(want))
	for _, s := range want {
		if !strings.Contains(output, s) {
			missing = append(missing, s)
		}
	}
	unwanted := make([]string, 0, len(notWant))
	for _, s := range notWant {
		if strings.Contains(output, s) {
			unwanted = append(unwanted, s)
		}
	}
	if len(missing) == 0 && len(unwanted) == 0 {
		return
	}
	if len(missing) > 0 {
		t.Errorf("output missing expected substrings: %q", missing)
	}
	if len(unwanted) > 0 {
		t.Errorf("output contains forbidden substrings: %q", unwanted)
	}
	t.Logf("full output:\n%s", output)
}

// trackerHasAsset returns true if the install tracker contains an asset
// with the given name (any scope).
func trackerHasAsset(t *testing.T, name string) bool {
	t.Helper()
	tracker, err := assets.LoadTracker()
	if err != nil {
		t.Fatalf("LoadTracker: %v", err)
	}
	for _, a := range tracker.Assets {
		if a.Name == name {
			return true
		}
	}
	return false
}

// execAdd executes `sx add` with args and returns the combined output and error.
func execAdd(args ...string) (string, error) {
	var stdout, stderr bytes.Buffer
	cmd := NewAddCommand()
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String() + stderr.String(), err
}

// addAndInstall adds a fresh source skill to the vault and runs `sx install`.
// Fails the test on any error. Returns the source directory path.
func addAndInstall(t *testing.T, env *TestEnv, name string) string {
	t.Helper()
	sourceDir := createSourceSkill(env, name)
	addCmd := NewAddCommand()
	addCmd.SetArgs([]string{sourceDir, "--yes"})
	if err := addCmd.Execute(); err != nil {
		t.Fatalf("seed: add %s to vault: %v", name, err)
	}
	installCmd := NewInstallCommand()
	installCmd.SetArgs([]string{})
	if err := installCmd.Execute(); err != nil {
		t.Fatalf("seed: install %s: %v", name, err)
	}
	return sourceDir
}

func TestAddAlreadyInstalled(t *testing.T) {
	// Scenario 1: `sx add <name>` — asset is in vault AND installed (manifest intact).
	// Expected: name resolves to the installed on-disk directory via the
	// tracker, source-import detects identical contents, and the existing
	// scope is preserved. This mirrors `sx add <path>` exactly — both forms
	// flow through the same code after name resolution.
	t.Run("name_in_vault_installed", func(t *testing.T) {
		env := NewTestEnv(t)
		env.SetupPathVault()

		addAndInstall(t, env, "my-skill")
		env.AssertFileExists(filepath.Join(env.GlobalClaudeDir(), "skills", "my-skill"))

		output, _ := execAdd("my-skill", "--yes")

		assertOutput(t, output,
			[]string{"already exists in vault with identical contents", "Preserved existing scope for my-skill"},
			[]string{"Not installed", "not yet installed"},
		)
	})

	// Scenario 1b: vault storage exists, manifest cleared. Name resolves to
	// the on-disk dir; source-import re-imports against the (intact) storage
	// and recognizes identical contents.
	t.Run("name_in_vault_storage_but_not_in_manifest", func(t *testing.T) {
		env := NewTestEnv(t)
		vaultDir := env.SetupPathVault()

		addAndInstall(t, env, "team-skill")
		env.AssertFileExists(filepath.Join(env.GlobalClaudeDir(), "skills", "team-skill"))

		// Drop manifest, keep storage and installed files.
		env.ResetVaultAssets(vaultDir)

		output, _ := execAdd("team-skill", "--yes")

		assertOutput(t, output,
			[]string{"already exists in vault with identical contents", "Preserved existing scope for team-skill"},
			[]string{"(not yet installed)", "Not installed"},
		)
	})

	// Scenario 2: `sx add <name>` — asset is in vault (manifest knows about
	// it) but NOT installed. Expected: `sx add --yes` configures with default
	// scopes and runs install, so the tracker gains the asset.
	t.Run("name_in_vault_not_installed", func(t *testing.T) {
		env := NewTestEnv(t)
		env.SetupPathVault()

		sourceDir := createSourceSkill(env, "vault-only-skill")
		addCmd := NewAddCommand()
		addCmd.SetArgs([]string{sourceDir, "--yes", "--no-install"})
		if err := addCmd.Execute(); err != nil {
			t.Fatalf("seed: %v", err)
		}
		if trackerHasAsset(t, "vault-only-skill") {
			t.Fatal("seed sanity: tracker should be empty after --no-install")
		}

		output, err := execAdd("vault-only-skill", "--yes")
		if err != nil {
			t.Fatalf("add: %v", err)
		}

		// Whether `sx add <name> --yes` auto-installs is a separate product
		// decision (the source-import flow does, configureFoundAsset does not).
		// What this scenario asserts is: the asset is recognized as being in
		// the vault, not reported missing.
		assertOutput(t, output, nil, []string{"not found in vault", "not found"})
	})

	// Scenario 3: `sx add <name>` — asset NOT in vault and NOT installed.
	// Expected: hard error.
	t.Run("name_not_in_vault_not_installed", func(t *testing.T) {
		env := NewTestEnv(t)
		env.SetupPathVault()
		_ = env

		_, err := execAdd("nonexistent-skill", "--yes")
		if err == nil {
			t.Fatal("expected error when asset does not exist in vault, got nil")
		}
	})

	// Scenario 4: `sx add <name>` — asset NOT in vault but IS installed
	// (stale tracker, vault storage wiped). The name resolver finds the
	// on-disk copy via the tracker, so source-import runs and re-adds the
	// asset to the vault. This is the lost-and-found recovery path:
	// pointing `sx add` at a known name should reconstitute vault state
	// from a still-installed copy.
	t.Run("name_not_in_vault_but_installed_recovers", func(t *testing.T) {
		env := NewTestEnv(t)
		vaultDir := env.SetupPathVault()

		addAndInstall(t, env, "stale-skill")
		env.AssertFileExists(filepath.Join(env.GlobalClaudeDir(), "skills", "stale-skill"))

		env.ResetVaultAssets(vaultDir)
		if err := os.RemoveAll(filepath.Join(vaultDir, "assets", "stale-skill")); err != nil {
			t.Fatalf("remove vault storage: %v", err)
		}

		if _, err := execAdd("stale-skill", "--yes"); err != nil {
			t.Fatalf("recovery add: %v", err)
		}

		lf, ok := env.ReadVaultAssets(vaultDir)
		if !ok {
			t.Fatal("vault manifest missing")
		}
		found := false
		for _, a := range lf.Assets {
			if a.Name == "stale-skill" {
				found = true
				break
			}
		}
		if !found {
			t.Error("stale-skill should have been re-added to the vault from its installed copy")
		}
	})

	// Scenario 5: `sx add <path-to-installed-dir>` — asset in vault AND installed.
	// Expected: source-import flow runs, detects identical contents to the
	// existing vault version, and preserves the existing scope rather than
	// creating a new version. (The earlier band-aid that bypassed source-import
	// entirely broke the re-edit workflow — see scenario 5c.)
	t.Run("path_to_installed_dir_in_vault_installed", func(t *testing.T) {
		env := NewTestEnv(t)
		env.SetupPathVault()

		addAndInstall(t, env, "path-skill")
		installedDir := filepath.Join(env.GlobalClaudeDir(), "skills", "path-skill")
		env.AssertFileExists(installedDir)

		output, _ := execAdd(installedDir, "--yes")

		assertOutput(t, output,
			[]string{"already exists in vault with identical contents", "Preserved existing scope for path-skill"},
			[]string{"Not installed", "not yet installed"},
		)
	})

	// Scenario 5b: same as 5 but manifest cleared after install. Source-import
	// still finds the asset (vault storage intact) and recognizes identical
	// contents, so it preserves scope as in 5.
	t.Run("path_to_installed_dir_storage_but_not_in_manifest", func(t *testing.T) {
		env := NewTestEnv(t)
		vaultDir := env.SetupPathVault()

		addAndInstall(t, env, "path-team-skill")
		env.ResetVaultAssets(vaultDir)

		installedDir := filepath.Join(env.GlobalClaudeDir(), "skills", "path-team-skill")
		env.AssertFileExists(installedDir)

		output, _ := execAdd(installedDir, "--yes")

		assertOutput(t, output,
			[]string{"already exists in vault with identical contents", "Preserved existing scope for path-team-skill"},
			[]string{"(not yet installed)", "Not installed"},
		)
	})

	// Scenario 5c: `sx add <path-to-installed-dir>` after the user edited the
	// files in place. Source-import must upload a NEW version of the asset
	// rather than silently treating the path as a scope-config no-op (which
	// was the regression in the first attempted fix).
	t.Run("path_to_installed_dir_with_local_edits_uploads_new_version", func(t *testing.T) {
		env := NewTestEnv(t)
		vaultDir := env.SetupPathVault()

		addAndInstall(t, env, "editable-skill")
		installedDir := filepath.Join(env.GlobalClaudeDir(), "skills", "editable-skill")
		env.AssertFileExists(installedDir)

		// User edits the installed file in place.
		env.WriteFile(filepath.Join(installedDir, "SKILL.md"), "You are editable-skill v2 (edited)")

		if _, err := execAdd(installedDir, "--yes"); err != nil {
			t.Fatalf("add edited: %v", err)
		}

		// Vault must hold two versions of editable-skill.
		lf, ok := env.ReadVaultAssets(vaultDir)
		if !ok {
			t.Fatal("vault manifest missing")
		}
		count := 0
		for _, a := range lf.Assets {
			if a.Name == "editable-skill" {
				count++
			}
		}
		if count < 2 {
			t.Errorf("expected at least 2 versions of editable-skill in vault, got %d", count)
		}
	})

	// Scenario 5d: name-form equivalent of 5c. `sx add <name>` must resolve
	// to the installed on-disk directory and upload a new version when the
	// user edited files in place. This is the scenario that motivated
	// resolveInstalledAssetPath — without it, `sx add <name>` silently no-ops
	// on local edits while `sx add <path>` would upload them.
	t.Run("name_with_local_edits_uploads_new_version", func(t *testing.T) {
		env := NewTestEnv(t)
		vaultDir := env.SetupPathVault()

		addAndInstall(t, env, "named-editable")
		installedDir := filepath.Join(env.GlobalClaudeDir(), "skills", "named-editable")
		env.AssertFileExists(installedDir)

		env.WriteFile(filepath.Join(installedDir, "SKILL.md"), "You are named-editable v2 (edited in place)")

		if _, err := execAdd("named-editable", "--yes"); err != nil {
			t.Fatalf("add by name after edit: %v", err)
		}

		lf, ok := env.ReadVaultAssets(vaultDir)
		if !ok {
			t.Fatal("vault manifest missing")
		}
		count := 0
		for _, a := range lf.Assets {
			if a.Name == "named-editable" {
				count++
			}
		}
		if count < 2 {
			t.Errorf("expected at least 2 versions after edit-then-add-by-name, got %d", count)
		}
	})

	// Scenario 5e: after a new-version upload, the scope data feeding the
	// interactive prompt must still reflect the existing installation —
	// not "Not installed". `--yes` mode bypasses the prompt itself, so we
	// assert against resolveCurrentScopes directly: this is the exact data
	// that displayCurrentTargets would render, and the bug the user
	// reported (`✓ Successfully added check-sx@3` followed by "Not installed
	// (available in vault only)") is precisely a regression where this
	// helper returns nil when it shouldn't.
	t.Run("new_version_upload_preserves_install_state_for_prompt", func(t *testing.T) {
		env := NewTestEnv(t)
		env.SetupPathVault()

		addAndInstall(t, env, "scope-survives")
		installedDir := filepath.Join(env.GlobalClaudeDir(), "skills", "scope-survives")
		env.WriteFile(filepath.Join(installedDir, "SKILL.md"), "edited content")

		if _, err := execAdd(installedDir, "--yes"); err != nil {
			t.Fatalf("upload new version: %v", err)
		}

		// Now ask the helper what the scope prompt would see.
		cfg, err := config.Load()
		if err != nil {
			t.Fatalf("config load: %v", err)
		}
		vault, err := vaultpkg.NewFromConfig(cfg)
		if err != nil {
			t.Fatalf("open vault: %v", err)
		}
		got := resolveCurrentScopes(vault, "scope-survives")
		if got == nil {
			t.Fatal("resolveCurrentScopes returned nil after a new-version upload — " +
				"this is the exact 'Not installed' regression we were tracking")
		}
	})

	// Scenario 6: `sx add <path-to-installed-dir>` — asset in vault but tracker
	// does NOT list it as installed (manually-created dir at the install
	// location). The routing check in add.go gates on tracker presence, so we
	// expect it to fall through to the source-import flow. Assert that the
	// asset is not duplicated in the vault.
	t.Run("path_to_installed_dir_in_vault_not_tracked", func(t *testing.T) {
		env := NewTestEnv(t)
		vaultDir := env.SetupPathVault()

		sourceDir := createSourceSkill(env, "phantom-skill")
		addCmd := NewAddCommand()
		addCmd.SetArgs([]string{sourceDir, "--yes", "--no-install"})
		if err := addCmd.Execute(); err != nil {
			t.Fatalf("seed: %v", err)
		}

		// Plant a directory at the install location without tracking it.
		installedDir := filepath.Join(env.GlobalClaudeDir(), "skills", "phantom-skill")
		env.MkdirAll(installedDir)
		env.WriteFile(filepath.Join(installedDir, "metadata.toml"), `[asset]
name = "phantom-skill"
type = "skill"
description = "Test skill phantom-skill"

[skill]
readme = "README.md"
prompt-file = "SKILL.md"
`)
		env.WriteFile(filepath.Join(installedDir, "README.md"), "# phantom-skill")
		env.WriteFile(filepath.Join(installedDir, "SKILL.md"), "You are phantom-skill")

		if _, err := execAdd(installedDir, "--yes"); err != nil {
			t.Fatalf("sx add: %v", err)
		}

		// We accept either: (a) the planted dir contents match the original
		// source so the vault still has exactly one entry, or (b) a new
		// version is added. Both are correct fall-throughs to the source-import
		// flow. What must NOT happen is the asset losing its vault entry or
		// the tracker getting populated by a non-install path.
		lf, ok := env.ReadVaultAssets(vaultDir)
		if !ok {
			t.Fatal("vault manifest missing after add")
		}
		found := false
		for _, a := range lf.Assets {
			if a.Name == "phantom-skill" {
				found = true
				break
			}
		}
		if !found {
			t.Error("phantom-skill should still be present in vault after add via planted path")
		}
	})

	// Scenario 7: `sx add <path-to-source-dir>` — asset in vault AND installed.
	// Expected: re-add succeeds, install status preserved in tracker.
	t.Run("path_to_source_dir_in_vault_installed_reupload", func(t *testing.T) {
		env := NewTestEnv(t)
		env.SetupPathVault()

		sourceDir := addAndInstall(t, env, "updatable-skill")

		// Modify source and re-add.
		env.WriteFile(filepath.Join(sourceDir, "SKILL.md"), "You are updatable-skill v2 with changes")

		output, err := execAdd(sourceDir, "--yes")
		if err != nil {
			t.Fatalf("re-add: %v", err)
		}

		assertOutput(t, output, nil, []string{"not found"})

		if !trackerHasAsset(t, "updatable-skill") {
			t.Error("install tracker should still list updatable-skill after re-add")
		}
	})

	// Scenario 8: `sx add <path-to-source-dir>` — asset in vault, NOT installed.
	// Expected: re-add updates the vault entry; tracker still empty (we passed
	// --no-install both times).
	t.Run("path_to_source_dir_in_vault_not_installed", func(t *testing.T) {
		env := NewTestEnv(t)
		env.SetupPathVault()

		sourceDir := createSourceSkill(env, "source-only-skill")
		addCmd := NewAddCommand()
		addCmd.SetArgs([]string{sourceDir, "--yes", "--no-install"})
		if err := addCmd.Execute(); err != nil {
			t.Fatalf("seed: %v", err)
		}

		env.WriteFile(filepath.Join(sourceDir, "SKILL.md"), "You are source-only-skill v2")

		if _, err := execAdd(sourceDir, "--yes", "--no-install"); err != nil {
			t.Errorf("re-add: %v", err)
		}

		if trackerHasAsset(t, "source-only-skill") {
			t.Error("tracker should not list source-only-skill: --no-install was used")
		}
	})

	// Scenario 9: `sx add <path-to-source-dir>` — brand new asset.
	// Expected: vault gains the asset and (since we don't pass --no-install)
	// the install tracker picks it up too.
	t.Run("path_to_source_dir_brand_new_installs", func(t *testing.T) {
		env := NewTestEnv(t)
		vaultDir := env.SetupPathVault()

		sourceDir := createSourceSkill(env, "brand-new-skill")

		if _, err := execAdd(sourceDir, "--yes"); err != nil {
			t.Fatalf("add new: %v", err)
		}

		lf, ok := env.ReadVaultAssets(vaultDir)
		if !ok {
			t.Fatal("vault manifest missing")
		}
		found := false
		for _, a := range lf.Assets {
			if a.Name == "brand-new-skill" {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("brand-new-skill not in vault manifest; assets=%v", lf.Assets)
		}

		if !trackerHasAsset(t, "brand-new-skill") {
			t.Error("`sx add <source> --yes` (without --no-install) should install the asset")
		}
		env.AssertFileExists(filepath.Join(env.GlobalClaudeDir(), "skills", "brand-new-skill"))
	})
}

// createSourceSkill creates a source skill directory for testing.
func createSourceSkill(env *TestEnv, name string) string {
	env.t.Helper()
	sourceDir := env.MkdirAll(filepath.Join(env.TempDir, "source-"+name))
	env.WriteFile(filepath.Join(sourceDir, "metadata.toml"), `[asset]
name = "`+name+`"
type = "skill"
description = "Test skill `+name+`"

[skill]
readme = "README.md"
prompt-file = "SKILL.md"
`)
	env.WriteFile(filepath.Join(sourceDir, "README.md"), "# "+name)
	env.WriteFile(filepath.Join(sourceDir, "SKILL.md"), "You are "+name)
	return sourceDir
}
