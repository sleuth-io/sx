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
}

// promptForRepositories prompts user for repository configurations and returns them
// Takes currentRepos (nil if not installed, empty slice if global, or list of repos)
// Returns scopeResult with Remove=true if user chooses not to install
func promptForRepositories(out *outputHelper, assetName, version string, currentRepos []lockfile.Scope, v vault.Vault) (*scopeResult, error) {
	// Use the new UI components (they automatically fall back to simple text in non-TTY)
	styledOut := ui.NewOutput(out.cmd.OutOrStdout(), out.cmd.ErrOrStderr())
	ioc := components.NewIOContext(out.cmd.InOrStdin(), out.cmd.OutOrStdout())
	return promptForRepositoriesWithUI(assetName, version, currentRepos, v, styledOut, ioc)
}

// promptForRepositoriesWithUI prompts user for repository configurations using new UI
// Takes currentRepos (nil if not installed, empty slice if global, or list of repos)
// Returns scopeResult with Remove=true if user chooses not to install
func promptForRepositoriesWithUI(assetName, version string, currentRepos []lockfile.Scope, v vault.Vault, styledOut *ui.Output, ioc *components.IOContext) (*scopeResult, error) {
	// Build options based on current state with Value field for switch
	var options []components.Option

	// Only show "Keep current" if already installed
	if currentRepos != nil {
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
	displayCurrentInstallation(currentRepos, styledOut)
	selected, err := ioc.Select("", options)
	if err != nil {
		// If user cancelled, treat it as "keep current" if installed, or "don't install" if not
		if err.Error() == "selection cancelled" {
			if currentRepos != nil {
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

	case "modify": // Add/modify scopes
		// Identity scopes (team/user/bot) are only offered when the vault can
		// persist them — i.e. it implements the bulk SetAssetInstallations
		// setter (the Sleuth/skills.io vault). File-backed vaults stay
		// repo/path-only.
		_, allowIdentity := v.(installSetter)
		targets, appendMode, err := modifyScopes(scopesToTargets(currentRepos), allowIdentity, styledOut, ioc)
		if err != nil {
			return nil, err
		}
		return &scopeResult{Scopes: targetsToScopes(targets), Targets: targets, Append: appendMode}, nil

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

// modifyScopes runs the interactive scope editor over a kind-aware target
// list. allowIdentity gates the team/user/bot actions to vaults that can
// persist them. Returns the edited list (or the original on cancel).
func modifyScopes(current []vault.InstallTarget, allowIdentity bool, styledOut *ui.Output, ioc *components.IOContext) ([]vault.InstallTarget, bool, error) {
	// Clone current state (so we can cancel without side effects)
	working := append([]vault.InstallTarget(nil), current...)
	original := append([]vault.InstallTarget(nil), current...)

	// Once the user has added a scope this session, default the menu to "Done"
	// instead of the first option — adding one scope is usually the whole task.
	addedScope := false

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
		if addedScope {
			defaultIdx = len(options) - 1
		}
		selected, err := ioc.SelectWithDefault("", options, defaultIdx)
		if err != nil {
			// If user cancelled, return original unchanged state
			if err.Error() == "selection cancelled" {
				styledOut.Info("Changes cancelled")
				return original, false, nil
			}
			return nil, false, err
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
			addedScope = true
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
			addedScope = true
			styledOut.Success("Added " + formatTarget(t))

		case "add-team":
			names, err := promptForScopeList(ioc, "Team name(s) — comma-separated", "", "team name")
			if err != nil {
				styledOut.Error(err.Error())
				continue
			}
			for _, name := range names {
				t := vault.InstallTarget{Kind: vault.InstallKindTeam, Team: name}
				working = append(working, t)
				addedScope = true
				styledOut.Success("Added " + formatTarget(t))
			}

		case "add-user":
			// Prefill "me" so the common case (assign to yourself) is just
			// Enter; "me" resolves to your account at save time. Accepts a
			// comma-separated list so you can add several users at once.
			emails, err := promptForScopeList(ioc, "User email(s) — comma-separated, 'me' = you", "me", "user email")
			if err != nil {
				styledOut.Error(err.Error())
				continue
			}
			for _, email := range emails {
				t := vault.InstallTarget{Kind: vault.InstallKindUser, User: email}
				working = append(working, t)
				addedScope = true
				styledOut.Success("Added " + formatTarget(t))
			}

		case "add-bot":
			names, err := promptForScopeList(ioc, "Bot name(s) — comma-separated", "", "bot name")
			if err != nil {
				styledOut.Error(err.Error())
				continue
			}
			for _, name := range names {
				t := vault.InstallTarget{Kind: vault.InstallKindBot, Bot: name}
				working = append(working, t)
				addedScope = true
				styledOut.Success("Added " + formatTarget(t))
			}

		case "remove":
			if len(working) == 0 {
				styledOut.Warning("No scopes to remove")
				continue
			}

			// Build selection list with indices as values
			scopeOptions := make([]components.Option, len(working))
			for i, t := range working {
				scopeOptions[i] = components.Option{
					Label: formatTarget(t),
					Value: strconv.Itoa(i),
				}
			}

			selectedScope, err := ioc.Select("Which scope would you like to remove?", scopeOptions)
			if err != nil {
				// User pressed esc or cancelled
				continue
			}

			// Parse index from Value
			var idx int
			if _, err := fmt.Sscanf(selectedScope.Value, "%d", &idx); err != nil {
				continue
			}

			removed := working[idx]
			working = append(working[:idx], working[idx+1:]...)
			styledOut.Success("Removed " + formatTarget(removed))

		case "remove-all":
			if len(working) == 0 {
				styledOut.Warning("No scopes to remove")
				continue
			}
			working = working[:0]
			styledOut.Success("Removed all scopes")

		case "done":
			// Preview changes if any
			if displayScopeChanges(original, working, styledOut) {
				// Ask append-vs-replace before the final confirmation, so the
				// preview, the apply mode, and the confirm read top-to-bottom.
				// Only meaningful when the vault merges server-side (Sleuth)
				// and there are scopes to apply.
				appendMode := false
				if allowIdentity && len(working) > 0 {
					appendMode, err = askAppendOrReplace(ioc)
					if err != nil {
						return nil, false, err
					}
					if appendMode {
						styledOut.Success("Append")
					} else {
						styledOut.Success("Replace")
					}
				}

				// Ask for confirmation (default to yes)
				confirmed, err := ioc.Confirm("Continue with these changes?", true)
				if err != nil || !confirmed {
					styledOut.Info("Changes cancelled")
					return original, false, nil // Return original, unchanged
				}

				return working, appendMode, nil
			}

			return working, false, nil
		}
	}
}

// askAppendOrReplace asks whether the chosen scopes should be merged with the
// asset's existing scopes (append) or replace them entirely. Defaults to
// append, including when the prompt is cancelled, since that's the
// non-destructive choice.
func askAppendOrReplace(ioc *components.IOContext) (bool, error) {
	options := []components.Option{
		{Label: "Append to existing scopes", Value: "append", Description: "keep current installs and add these"},
		{Label: "Replace existing scopes", Value: "replace", Description: "remove current installs, set only these"},
	}
	selected, err := ioc.SelectWithDefault("Apply these scopes how?", options, 0)
	if err != nil {
		if err.Error() == "selection cancelled" {
			return true, nil
		}
		return false, err
	}
	return selected.Value == "append", nil
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

	// If it's just a slug (e.g., "user/repo"), convert to full GitHub URL
	if !strings.Contains(repoURL, "://") && !strings.HasPrefix(repoURL, "git@") {
		parts := strings.Split(repoURL, "/")
		if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
			repoURL = "https://github.com/" + repoURL
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
	for _, part := range strings.Split(raw, ",") {
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
