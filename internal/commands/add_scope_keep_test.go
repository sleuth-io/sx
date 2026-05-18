package commands

import (
	"bufio"
	"io"
	"strings"
	"testing"

	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/ui"
	"github.com/sleuth-io/sx/internal/ui/components"
	vaultpkg "github.com/sleuth-io/sx/internal/vault"
)

// emptyVault is a no-op vault used to drive promptForRepositoriesWithUI without
// pulling in real config/manifest setup. The prompt only reaches into the vault
// via the optional ScopeOptionProvider interface, which this type does not
// implement, so the standard option set is rendered.
type emptyVault struct {
	vaultpkg.Vault
}

// TestPromptKeepCurrentSettingsSetsInherit pins the SK-502 contract: when the
// user picks "Keep current settings" the returned scopeResult must have
// Inherit==true. Downstream (handleIdenticalAsset / addNewAsset) only takes
// the no-mutation branch when Inherit is true; for Sleuth vaults that branch
// is the difference between "send nothing, server preserves scope" and
// "send currentRepos as authoritative, server overwrites identity-dependent
// scopes (user/team/bot) that the client never saw".
//
// Bug history: before the fix, "keep" returned only Scopes=currentRepos and
// Inherit defaulted to false, so the prompt's "no changes" promise silently
// became "overwrite to whatever the stripped lockfile view shows".
func TestPromptKeepCurrentSettingsSetsInherit(t *testing.T) {
	// currentRepos is empty-but-not-nil, which maps to a global install in
	// displayCurrentInstallation. "Keep current" is shown because nil vs
	// empty slice is the prompt's "installed" gate.
	currentRepos := []lockfile.Scope{}

	// "1\n" selects the first option. With currentRepos != nil, that's
	// "Keep current settings".
	in := bufio.NewReader(strings.NewReader("1\n"))
	out := io.Discard
	styledOut := ui.NewOutput(out, out)
	ioc := components.NewIOContext(in, out)

	result, err := promptForRepositoriesWithUI(
		"my-skill", "1",
		currentRepos,
		&emptyVault{},
		styledOut,
		ioc,
	)
	if err != nil {
		t.Fatalf("prompt: %v", err)
	}

	if !result.Inherit {
		t.Errorf("expected Inherit=true on Keep current settings, got %#v", result)
	}
	if result.Remove {
		t.Errorf("Keep current must not set Remove=true, got %#v", result)
	}
}
