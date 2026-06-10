package commands

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/ui"
	"github.com/sleuth-io/sx/internal/ui/components"
	"github.com/sleuth-io/sx/internal/vault"
)

// scopeResult holds the result of scope prompting
type scopeResult struct {
	Scopes      []lockfile.Scope
	Targets     []vault.InstallTarget // kind-aware scopes (repo/path/team/user/bot) from the interactive editor
	Append      bool                  // Merge Targets with existing server scopes instead of replacing them
	ScopeEntity string                // vault-specific (e.g., "personal"), empty for standard scoping
	Remove      bool                  // User chose "remove from installation"
	Inherit     bool                  // Preserve existing installations (no scope flags provided)

	// Edited marks the interactive "Edit scopes" path on a vault that supports
	// targeted removal (Sleuth). The change is applied as a precise diff:
	// Removed installs are uninstalled by GID and Added installs are appended,
	// so kept installs are never re-resolved or clobbered.
	Edited  bool
	Added   []vault.InstallTarget // installs to add (appended to the server set)
	Removed []vault.InstallTarget // installs to remove (carry the server EntityID)

	// ApplyTargets forces the kind-aware bulk path (SetAssetInstallations) for
	// Targets regardless of kind — used by the flag-driven flow so a repo/path
	// or org replace goes through the same setter as identity scopes, instead of
	// the legacy lockfile SetInstallations branch. Append selects replace vs add.
	ApplyTargets bool
}

// promptForRepositories prompts user for repository configurations and returns them.
// current is the asset's real installation as kind-aware targets and installed
// reports whether it's installed at all (an empty target set with installed=true
// means global). Returns scopeResult with Remove=true if user chooses not to install.
func promptForRepositories(out *outputHelper, assetName, version string, current []vault.InstallTarget, installed bool, v vault.Vault) (*scopeResult, error) {
	// Use the new UI components (they automatically fall back to simple text in non-TTY)
	styledOut := ui.NewOutput(out.cmd.OutOrStdout(), out.cmd.ErrOrStderr())
	ioc := components.NewIOContext(out.cmd.InOrStdin(), out.cmd.OutOrStdout())
	return promptForRepositoriesWithUI(assetName, version, current, installed, v, styledOut, ioc)
}

// promptForRepositoriesWithUI prompts user for repository configurations using new UI.
// current is the asset's real installation as kind-aware targets and installed
// reports whether it's installed at all (empty targets + installed=true => global).
// Returns scopeResult with Remove=true if user chooses not to install.
func promptForRepositoriesWithUI(assetName, version string, current []vault.InstallTarget, installed bool, v vault.Vault, styledOut *ui.Output, ioc *components.IOContext) (*scopeResult, error) {
	// Build options based on current state with Value field for switch
	var options []components.Option

	// Only show "Keep current" if already installed
	if installed {
		options = append(options, components.Option{
			Label:       "Keep current settings",
			Value:       "keep",
			Description: "No changes will be made",
		})
	}

	options = append(options,
		components.Option{
			Label:       "Make it available globally",
			Value:       "global",
			Description: "Org-wide, no restrictions",
		},
	)

	// Vault-specific scope options (e.g. the Sleuth vault's "Just for me"
	// personal install) sit directly under "Make it available globally" — the
	// two global/self options read together — and are handled by value in the
	// switch below.
	if sop, ok := v.(vault.ScopeOptionProvider); ok {
		for _, opt := range sop.GetScopeOptions() {
			options = append(options, components.Option{
				Label:       opt.Label,
				Value:       opt.Value,
				Description: opt.Description,
			})
		}
	} else if _, ok := v.(installSetter); ok {
		// File-backed vaults (git/path) don't supply their own "personal"
		// option, but they can persist a user scope — so offer "Just for me"
		// generically, implemented as a user scope on the caller's own account.
		options = append(options, components.Option{
			Label:       "Just for me",
			Value:       "self",
			Description: "Install only for your account",
		})
	}

	options = append(options,
		components.Option{
			Label:       "Edit scopes",
			Value:       "modify",
			Description: "Add/remove repos, paths, teams, users, bots",
		},
		components.Option{
			Label:       "Remove from installation",
			Value:       "remove",
			Description: "Uninstall (keeps it in vault)",
		},
	)

	// Title, then the original current-installation block, then the menu.
	styledOut.Newline()
	styledOut.Header("Scope for " + assetName)
	displayCurrentTargets(current, installed, styledOut)
	selected, err := ioc.Select("", options)
	if err != nil {
		// If user cancelled, treat it as "keep current" if installed, or "don't install" if not
		if err.Error() == "selection cancelled" {
			if installed {
				styledOut.Info("No changes made")
				return &scopeResult{Inherit: true}, nil
			}
			styledOut.Info("Cancelled")
			return &scopeResult{Remove: true}, nil
		}
		return nil, err
	}

	switch selected.Value {
	case "keep": // Keep current settings
		// Inherit:true makes the downstream flow take the no-mutation
		// branch (inheritLockFile is a no-op for Sleuth vaults), so the
		// server's existing scope — including identity-dependent kinds
		// like user/team/bot that aren't visible in the stripped lockfile
		// view — is preserved verbatim.
		styledOut.Success(fmt.Sprintf("%s v%s - no changes made", assetName, version))
		return &scopeResult{Inherit: true}, nil

	case "global": // Make it available globally
		styledOut.Success("Set to global installation")
		return &scopeResult{Scopes: []lockfile.Scope{}}, nil

	case "self": // Just for me — a user scope on the caller's own account.
		// "me" is resolved to the caller's email by resolveSelfUserScopes inside
		// bulkSetInstallTargets; the user kind routes through the bulk setter
		// (hasIdentityScope) as a replace, so the asset ends up scoped to just
		// the caller.
		return &scopeResult{Targets: []vault.InstallTarget{{Kind: vault.InstallKindUser, User: meScopeAlias}}}, nil

	case "modify": // Add/modify scopes
		// Identity scopes (team/user/bot) are only offered when the vault can
		// persist them — i.e. it implements the bulk SetAssetInstallations
		// setter (the Sleuth/skills.io vault). File-backed vaults stay
		// repo/path-only.
		_, allowIdentity := v.(installSetter)
		working, added, removed, err := modifyScopes(current, allowIdentity, styledOut, ioc)
		if err != nil {
			return nil, err
		}
		// Sleuth-class vaults apply the edit as a precise diff: removed installs
		// go through uninstallAssetTargets (by GID) and additions through an
		// append, so kept installs we can't re-resolve are never touched.
		if _, ok := v.(targetUninstaller); ok {
			return &scopeResult{Edited: true, Added: added, Removed: removed}, nil
		}
		// File-backed vaults replace their repo/path set with the edited working set.
		return &scopeResult{Scopes: targetsToScopes(working), Targets: working}, nil

	case "remove": // Remove from installation
		styledOut.Info("Removing from installation (will remain available in vault)")
		return &scopeResult{Remove: true}, nil

	default:
		// Check if the selection matches a vault-specific scope option
		if sop, ok := v.(vault.ScopeOptionProvider); ok {
			for _, opt := range sop.GetScopeOptions() {
				if selected.Value == opt.Value {
					styledOut.Success("Set to " + opt.Label)
					return &scopeResult{ScopeEntity: selected.Value}, nil
				}
			}
		}
		return nil, errors.New("invalid selection")
	}
}

// addIdentityScopes prompts for a comma-separated list of identity values
// (team names, user emails, or bot names), builds a target for each via build,
// appends them to working, and reports each. Returns true if any were added.
// Extracted from modifyScopes so the three near-identical add-team/user/bot
// cases don't each carry their own prompt+loop.
func addIdentityScopes(ioc *components.IOContext, styledOut *ui.Output, label, def, what string, build func(string) vault.InstallTarget, working *[]vault.InstallTarget) bool {
	vals, err := promptForScopeList(ioc, label, def, what)
	if err != nil {
		styledOut.Error(err.Error())
		return false
	}
	for _, v := range vals {
		t := build(v)
		*working = append(*working, t)
		styledOut.Success("Added " + formatTarget(t))
	}
	return len(vals) > 0
}

// removeScopesInteractive shows a multi-select of the current scopes and
// returns the set with the chosen ones removed, plus whether anything changed.
// Extracted from modifyScopes to keep that function's complexity in check.
func removeScopesInteractive(ioc *components.IOContext, styledOut *ui.Output, working []vault.InstallTarget) ([]vault.InstallTarget, bool) {
	if len(working) == 0 {
		styledOut.Warning("No scopes to remove")
		return working, false
	}
	// Build a multi-select list with indices as values, so several scopes can
	// be removed in one pass.
	scopeOptions := make([]components.MultiSelectOption, len(working))
	for i, t := range working {
		scopeOptions[i] = components.MultiSelectOption{Label: formatTarget(t), Value: strconv.Itoa(i)}
	}
	chosen, err := ioc.MultiSelect("Which scope(s) would you like to remove?", scopeOptions)
	if err != nil {
		// User pressed esc or cancelled.
		return working, false
	}
	removeIdx := make(map[int]bool)
	for _, opt := range chosen {
		if !opt.Selected {
			continue
		}
		idx, err := strconv.Atoi(opt.Value)
		if err != nil {
			continue
		}
		removeIdx[idx] = true
	}
	if len(removeIdx) == 0 {
		return working, false
	}
	var kept []vault.InstallTarget
	for i, t := range working {
		if removeIdx[i] {
			styledOut.Success("Removed " + formatTarget(t))
		} else {
			kept = append(kept, t)
		}
	}
	return kept, true
}

// modifyScopes runs the interactive scope editor over a kind-aware target
// list. allowIdentity gates the team/user/bot actions to vaults that can
// persist them. It returns the edited working set plus the diff against the
// original — added (newly introduced) and removed (dropped, still carrying the
// original's server EntityID). On cancel it returns the original with empty
// diffs, so nothing is applied.
func modifyScopes(current []vault.InstallTarget, allowIdentity bool, styledOut *ui.Output, ioc *components.IOContext) (working, added, removed []vault.InstallTarget, err error) {
	// Clone current state (so we can cancel without side effects)
	working = append([]vault.InstallTarget(nil), current...)
	original := append([]vault.InstallTarget(nil), current...)

	// Once the user has added a scope this session, default the menu to "Done"
	// instead of the first option — adding one scope is usually the whole task.
	changedScope := false

	for {
		// Build action menu with Value fields
		options := []components.Option{
			{Label: "Add a repo scope", Value: "add-repo", Description: "entire repository"},
			{Label: "Add a path scope", Value: "add-path", Description: "specific paths within a repository"},
		}
		if allowIdentity {
			options = append(options,
				components.Option{Label: "Add a team scope", Value: "add-team", Description: "everyone on a team"},
				components.Option{Label: "Add a user scope", Value: "add-user", Description: "a single user, by email (entering \"me\" resolves to your email address )"},
				components.Option{Label: "Add a bot scope", Value: "add-bot", Description: "a single bot, by name"},
			)
		}
		options = append(options,
			components.Option{Label: "Remove a scope", Value: "remove", Description: "pick from current list"},
			components.Option{Label: "Remove all scopes", Value: "remove-all", Description: "clear every scope"},
			components.Option{Label: "Done", Value: "done"},
		)

		// Default to the first option (index 0) on first entry, but to "Done"
		// (always the last option) once at least one scope has been added.
		defaultIdx := 0
		if changedScope {
			defaultIdx = len(options) - 1
		}
		selected, err := ioc.SelectWithDefault("", options, defaultIdx)
		if err != nil {
			// If user cancelled, return original unchanged state
			if err.Error() == "selection cancelled" {
				styledOut.Info("Changes cancelled")
				return original, nil, nil, nil
			}
			return nil, nil, nil, err
		}

		switch selected.Value {
		case "add-repo":
			repoURL, err := promptForRepoURL(ioc)
			if err != nil {
				styledOut.Error(fmt.Sprintf("Failed to add repo scope: %v", err))
				continue
			}
			t := vault.InstallTarget{Kind: vault.InstallKindRepo, Repo: repoURL}
			working = append(working, t)
			changedScope = true
			styledOut.Success("Added " + formatTarget(t))

		case "add-path":
			repoURL, err := promptForRepoURL(ioc)
			if err != nil {
				styledOut.Error(fmt.Sprintf("Failed to add path scope: %v", err))
				continue
			}
			paths, err := promptForRepositoryPaths(styledOut, repoURL, ioc)
			if err != nil {
				styledOut.Error(fmt.Sprintf("Failed to collect paths: %v", err))
				continue
			}
			t := vault.InstallTarget{Kind: vault.InstallKindPath, Repo: repoURL, Paths: paths}
			working = append(working, t)
			changedScope = true
			styledOut.Success("Added " + formatTarget(t))

		case "add-team":
			if addIdentityScopes(ioc, styledOut, "Team name(s) — comma-separated", "", "team name",
				func(v string) vault.InstallTarget { return vault.InstallTarget{Kind: vault.InstallKindTeam, Team: v} },
				&working) {
				changedScope = true
			}

		case "add-user":
			// Prefill "me" so the common case (assign to yourself) is just
			// Enter; "me" resolves to your account at save time. Accepts a
			// comma-separated list so you can add several users at once.
			if addIdentityScopes(ioc, styledOut, "User email(s) — comma-separated, 'me' = you", "me", "user email",
				func(v string) vault.InstallTarget { return vault.InstallTarget{Kind: vault.InstallKindUser, User: v} },
				&working) {
				changedScope = true
			}

		case "add-bot":
			if addIdentityScopes(ioc, styledOut, "Bot name(s) — comma-separated", "", "bot name",
				func(v string) vault.InstallTarget { return vault.InstallTarget{Kind: vault.InstallKindBot, Bot: v} },
				&working) {
				changedScope = true
			}

		case "remove":
			if next, changed := removeScopesInteractive(ioc, styledOut, working); changed {
				working = next
				changedScope = true
			}

		case "remove-all":
			if len(working) == 0 {
				styledOut.Warning("No scopes to remove")
				continue
			}
			working = working[:0]
			changedScope = true
			styledOut.Success("Removed all scopes")

		case "done":
			// Preview the diff. No changes → nothing to apply.
			if !displayScopeChanges(original, working, styledOut) {
				return working, nil, nil, nil
			}
			// Ask for confirmation (default to yes).
			confirmed, cerr := ioc.Confirm("Continue with these changes?", true)
			if cerr != nil || !confirmed {
				styledOut.Info("Changes cancelled")
				return original, nil, nil, nil // unchanged
			}
			added, removed = diffTargets(original, working)
			return working, added, removed, nil
		}
	}
}

// promptForRepoURL prompts for a repository URL and normalizes bare "owner/repo"
// slugs to full GitHub URLs.
func promptForRepoURL(ioc *components.IOContext) (string, error) {
	repoURL, err := ioc.Input("Repository URL (e.g., github.com/user/repo or full URL)", "")
	if err != nil {
		return "", err
	}

	repoURL = strings.TrimSpace(repoURL)
	if repoURL == "" {
		return "", errors.New("repository URL is required")
	}

	// Normalize scheme-less input to a full URL. The server matches repos by
	// exact URL string and stores them with the scheme (https://github.com/...),
	// so anything without a scheme is rejected as "not found".
	if !strings.Contains(repoURL, "://") && !strings.HasPrefix(repoURL, "git@") {
		parts := strings.Split(repoURL, "/")
		if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
			// Bare "owner/repo" slug — assume GitHub.
			repoURL = "https://github.com/" + repoURL
		} else {
			// Host-prefixed but scheme-less, e.g. "github.com/owner/repo".
			repoURL = "https://" + repoURL
		}
	}

	return repoURL, nil
}

// promptForScopeList prompts for one or more comma-separated identity values
// (team names, user emails, or bot names), trimming each and dropping blanks.
// def is the prefilled default; what names the value for the "required" error.
func promptForScopeList(ioc *components.IOContext, label, def, what string) ([]string, error) {
	raw, err := ioc.Input(label, def)
	if err != nil {
		return nil, err
	}
	var vals []string
	for part := range strings.SplitSeq(raw, ",") {
		if v := strings.TrimSpace(part); v != "" {
			vals = append(vals, v)
		}
	}
	if len(vals) == 0 {
		return nil, fmt.Errorf("%s is required", what)
	}
	return vals, nil
}

// promptForRepositoryPaths collects one or more paths for a repository
func promptForRepositoryPaths(styledOut *ui.Output, repoURL string, ioc *components.IOContext) ([]string, error) {
	var paths []string

	for {
		path, err := ioc.Input("Path within repository (e.g., backend/services)", "")
		if err != nil {
			return nil, err
		}

		path = strings.TrimSpace(path)
		if path != "" {
			paths = append(paths, path)
		}

		if len(paths) == 0 {
			styledOut.Warning("At least one path is required when not installing for entire repository")
			continue
		}

		// Ask if they want to add another path (default to no)
		addAnother, err := ioc.Confirm("Add another path in this repository?", false)
		if err != nil || !addAnother {
			break
		}
	}

	return paths, nil
}
