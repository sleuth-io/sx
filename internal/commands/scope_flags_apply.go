package commands

import (
	"github.com/sleuth-io/sx/internal/ui"
	"github.com/sleuth-io/sx/internal/ui/components"
	vaultpkg "github.com/sleuth-io/sx/internal/vault"
)

// resolveAddScope decides where an asset is scoped during `sx add`, given the
// asset's current installation (current/installed, resolved by the caller):
//   - scope flags given → pre-fill that scope and confirm (unless --yes),
//     exactly as if the user had navigated the menu to it;
//   - --yes (or other non-interactive flag) with NO scope flags → inherit the
//     existing scope;
//   - otherwise → open the interactive scope editor.
func resolveAddScope(out *outputHelper, v vaultpkg.Vault, name, version string, current []vaultpkg.InstallTarget, installed bool, opts addOptions) (*scopeResult, error) {
	if opts.hasScopeFlags() {
		return resolveScopeFromFlags(out, name, current, installed, opts)
	}
	if opts.isNonInteractive() {
		return opts.getScopes()
	}
	return promptForRepositories(out, name, version, current, installed, v)
}

// resolveScopeFromFlags turns the unified scope flags into a proposed change,
// shows the same diff preview the interactive editor shows, and—unless --yes—
// asks for confirmation before returning a scopeResult to apply. Flags are a
// shortcut to the menu's outcome, not a way to skip the human's approval.
func resolveScopeFromFlags(out *outputHelper, name string, current []vaultpkg.InstallTarget, installed bool, opts addOptions) (*scopeResult, error) {
	change, err := resolveScopeFlags(opts.toScopeFlags())
	if err != nil {
		return nil, err
	}

	styledOut := ui.NewOutput(out.cmd.OutOrStdout(), out.cmd.ErrOrStderr())
	ioc := components.NewIOContext(out.cmd.InOrStdin(), out.cmd.OutOrStdout())

	// The set the asset ends up with: replace swaps the whole set; add merges.
	after := change.Targets
	if change.Mode == scopeAdd {
		after = unionTargets(current, change.Targets)
	}

	styledOut.Newline()
	styledOut.Header("Scope for " + name)
	displayCurrentTargets(current, installed, styledOut)

	if !displayScopeChanges(current, after, styledOut) {
		styledOut.Info("No changes to apply.")
		return &scopeResult{Inherit: true}, nil
	}

	// --yes skips the approval (for CI/scripts); otherwise the human confirms,
	// just as they would after editing scopes in the menu.
	if !opts.Yes {
		ok, err := ioc.Confirm("Continue with these changes?", true)
		if err != nil {
			return nil, err
		}
		if !ok {
			styledOut.Info("No changes made")
			return &scopeResult{Inherit: true}, nil
		}
	}

	return &scopeResult{
		ApplyTargets: true,
		Targets:      change.Targets,
		Append:       change.Mode == scopeAdd,
	}, nil
}

// unionTargets concatenates two target lists, deduping by display identity so
// the add-mode preview doesn't list a scope the asset already has.
func unionTargets(a, b []vaultpkg.InstallTarget) []vaultpkg.InstallTarget {
	seen := make(map[string]bool)
	out := make([]vaultpkg.InstallTarget, 0, len(a)+len(b))
	for _, t := range a {
		if k := formatTarget(t); !seen[k] {
			seen[k] = true
			out = append(out, t)
		}
	}
	for _, t := range b {
		if k := formatTarget(t); !seen[k] {
			seen[k] = true
			out = append(out, t)
		}
	}
	return out
}
