package commands

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// Issue 243: `sx add --yes` printed "Install failed" and still exited 0, so a
// script could not tell that the asset it just added does not install.
func TestAddYesReturnsErrorWhenInstallFails(t *testing.T) {
	env := NewTestEnv(t)
	vaultDir := env.SetupPathVault()
	sourceDir := env.MkdirAll(filepath.Join(env.TempDir, "source-skill"))
	env.WriteFile(filepath.Join(sourceDir, "metadata.toml"), `[asset]
name = "exit-skill"
type = "skill"
description = "A test skill"

[skill]
prompt-file = "SKILL.md"
`)
	env.WriteFile(filepath.Join(sourceDir, "SKILL.md"), "You are a test skill")

	orig := addInstallFn
	t.Cleanup(func() { addInstallFn = orig })
	addInstallFn = func(*cobra.Command, []string, bool, string, bool, string, string, bool, bool) error {
		return errors.New("no assets downloaded successfully")
	}

	addCmd := NewAddCommand()
	addCmd.SetArgs([]string{sourceDir, "--yes"})
	err := addCmd.Execute()
	if err == nil {
		t.Fatal("expected sx add --yes to fail when the install fails")
	}
	if !strings.Contains(err.Error(), "install failed") || !strings.Contains(err.Error(), "no assets downloaded successfully") {
		t.Errorf("error should name the failed install and its cause, got %q", err)
	}
	// The add itself happened; only the install failed.
	env.AssertFileExists(filepath.Join(vaultDir, "assets", "exit-skill", "metadata.toml"))
}

func TestAddYesSucceedsWhenInstallSucceeds(t *testing.T) {
	env := NewTestEnv(t)
	env.SetupPathVault()
	sourceDir := env.MkdirAll(filepath.Join(env.TempDir, "source-skill"))
	env.WriteFile(filepath.Join(sourceDir, "metadata.toml"), `[asset]
name = "ok-skill"
type = "skill"
description = "A test skill"

[skill]
prompt-file = "SKILL.md"
`)
	env.WriteFile(filepath.Join(sourceDir, "SKILL.md"), "You are a test skill")

	orig := addInstallFn
	t.Cleanup(func() { addInstallFn = orig })
	calls := 0
	addInstallFn = func(*cobra.Command, []string, bool, string, bool, string, string, bool, bool) error {
		calls++
		return nil
	}

	addCmd := NewAddCommand()
	addCmd.SetArgs([]string{sourceDir, "--yes"})
	if err := addCmd.Execute(); err != nil {
		t.Fatalf("add should succeed when the install succeeds: %v", err)
	}
	if calls != 1 {
		t.Errorf("expected one install after add, got %d", calls)
	}
}
