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
	ScopeEntity string // vault-specific (e.g., "personal"), empty for standard scoping
	Remove      bool   // User chose "remove from installation"
	Inherit     bool   // Preserve existing installations (no scope flags provided)
}

// promptForRepositoriesWithUI prompts user for repository configurations using new UI
// Takes currentRepos (nil if not installed, empty slice if global, or list of repos)
// Returns scopeResult with Remove=true if user chooses not to install
func promptForRepositoriesWithUI(assetName, version string, currentRepos []lockfile.Scope, v vault.Vault, styledOut *ui.Output, ioc *components.IOContext) (*scopeResult, error) {
	// Display current state
	displayCurrentInstallation(currentRepos, styledOut)

	styledOut.Newline()

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

	options = append(options, []components.Option{
		{
			Label:       "Make it available globally",
			Value:       "global",
			Description: "Install in all projects (removes repository restrictions)",
		},
		{
			Label:       "Add/modify repository-specific installations",
			Value:       "modify",
			Description: "Add repositories, remove existing ones, or change paths",
		},
	}...)

	// Add vault-specific scope options (e.g., "Just for me" for Sleuth vaults)
	if sop, ok := v.(vault.ScopeOptionProvider); ok {
		for _, opt := range sop.GetScopeOptions() {
			options = append(options, components.Option{
				Label:       opt.Label,
				Value:       opt.Value,
				Description: opt.Description,
			})
		}
	}

	options = append(options, components.Option{
		Label:       "Remove from installation",
		Value:       "remove",
		Description: "Uninstall this asset (keeps it in vault)",
	})

	// Show selection menu
	selected, err := ioc.Select("What would you like to do?", options)
	if err != nil {
		// If user cancelled, treat it as "keep current" if installed, or "don't install" if not
		if err.Error() == "selection cancelled" {
			if currentRepos != nil {
				styledOut.Info("No changes made")
				return &scopeResult{Scopes: currentRepos}, nil
			}
			styledOut.Info("Cancelled")
			return &scopeResult{Remove: true}, nil
		}
		return nil, err
	}

	switch selected.Value {
	case "keep": // Keep current settings
		styledOut.Success(fmt.Sprintf("%s v%s - no changes made", assetName, version))
		return &scopeResult{Scopes: currentRepos}, nil

	case "global": // Make it available globally
		styledOut.Success("Set to global installation")
		return &scopeResult{Scopes: []lockfile.Scope{}}, nil

	case "modify": // Add/modify repository-specific installations
		if currentRepos == nil {
			currentRepos = []lockfile.Scope{}
		}
		scopes, err := modifyRepositories(currentRepos, styledOut, ioc)
		if err != nil {
			return nil, err
		}
		return &scopeResult{Scopes: scopes}, nil

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

// modifyRepositories allows interactive modification of repository list
// Returns the modified list of repositories
func modifyRepositories(currentRepos []lockfile.Scope, styledOut *ui.Output, ioc *components.IOContext) ([]lockfile.Scope, error) {
	// Clone current state (so we can cancel without side effects)
	workingRepos := make([]lockfile.Scope, len(currentRepos))
	copy(workingRepos, currentRepos)

	// Save original for comparison later
	originalRepos := make([]lockfile.Scope, len(currentRepos))
	copy(originalRepos, currentRepos)

	for {
		// Display current list
		displayRepositoryList(workingRepos, styledOut)

		// Build action menu with Value fields
		options := []components.Option{
			{Label: "Add new repository", Value: "add", Description: "Add another repository to the installation list"},
			{Label: "Remove repository", Value: "remove", Description: "Remove an existing repository from the list"},
			{Label: "Modify repository paths", Value: "modify", Description: "Change which paths within a repository are included"},
			{Label: "Done with modifications", Value: "done", Description: "Continue to preview changes"},
		}

		// Show selection menu (default to "Done")
		selected, err := ioc.SelectWithDefault("Actions", options, len(options)-1)
		if err != nil {
			// If user cancelled, return original unchanged state
			if err.Error() == "selection cancelled" {
				styledOut.Info("Changes cancelled")
				return originalRepos, nil
			}
			return nil, err
		}

		switch selected.Value {
		case "add": // Add new repository
			repo, err := promptForNewRepository(styledOut, ioc)
			if err != nil {
				styledOut.Error(fmt.Sprintf("Failed to add repository: %v", err))
				continue
			}
			workingRepos = append(workingRepos, repo)
			styledOut.Success("Added " + formatRepository(repo))

		case "remove": // Remove repository
			if len(workingRepos) == 0 {
				styledOut.Warning("No repositories to remove")
				continue
			}

			// Build selection list with indices as values
			repoOptions := make([]components.Option, len(workingRepos))
			for i, repo := range workingRepos {
				repoOptions[i] = components.Option{
					Label: formatRepository(repo),
					Value: strconv.Itoa(i),
				}
			}

			selectedRepo, err := ioc.Select("Which repository would you like to remove?", repoOptions)
			if err != nil {
				// User pressed esc or cancelled
				continue
			}

			// Parse index from Value
			var idx int
			if _, err := fmt.Sscanf(selectedRepo.Value, "%d", &idx); err != nil {
				continue
			}

			removed := workingRepos[idx]
			workingRepos = append(workingRepos[:idx], workingRepos[idx+1:]...)
			styledOut.Success("Removed " + formatRepository(removed))

		case "modify": // Modify repository paths
			if len(workingRepos) == 0 {
				styledOut.Warning("No repositories to modify")
				continue
			}

			// Build selection list with indices as values
			repoOptions := make([]components.Option, len(workingRepos))
			for i, repo := range workingRepos {
				repoOptions[i] = components.Option{
					Label: formatRepository(repo),
					Value: strconv.Itoa(i),
				}
			}

			selectedRepo, err := ioc.Select("Which repository would you like to modify?", repoOptions)
			if err != nil {
				// User pressed esc or cancelled
				continue
			}

			// Parse index from Value
			var idx int
			if _, err := fmt.Sscanf(selectedRepo.Value, "%d", &idx); err != nil {
				continue
			}

			// Ask if they want entire repo or specific paths (default to yes)
			entireRepo, err := ioc.Confirm("Do you want to install for the entire repository?", true)
			if err != nil {
				continue
			}

			var paths []string
			if !entireRepo {
				// Collect new paths (replaces old ones)
				paths, err = promptForRepositoryPaths(styledOut, workingRepos[idx].Repo, ioc)
				if err != nil {
					styledOut.Error(fmt.Sprintf("Failed to collect paths: %v", err))
					continue
				}
			}

			workingRepos[idx].Paths = paths
			styledOut.Success("Updated " + formatRepository(workingRepos[idx]))

		case "done": // Done with modifications
			// Preview changes if any
			if displayRepositoryChanges(originalRepos, workingRepos, styledOut) {
				// Ask for confirmation (default to yes)
				confirmed, err := ioc.Confirm("Continue with these changes?", true)
				if err != nil || !confirmed {
					styledOut.Info("Changes cancelled")
					return originalRepos, nil // Return original, unchanged
				}
			}

			return workingRepos, nil
		}
	}
}

// promptForNewRepository prompts for a new repository with URL and paths
func promptForNewRepository(styledOut *ui.Output, ioc *components.IOContext) (lockfile.Scope, error) {
	// Prompt for repository URL
	repoURL, err := ioc.Input("Repository URL (e.g., github.com/user/repo or full URL)", "")
	if err != nil {
		return lockfile.Scope{}, err
	}

	repoURL = strings.TrimSpace(repoURL)
	if repoURL == "" {
		return lockfile.Scope{}, errors.New("repository URL is required")
	}

	// If it's just a slug (e.g., "user/repo"), convert to full GitHub URL
	if !strings.Contains(repoURL, "://") && !strings.HasPrefix(repoURL, "git@") {
		parts := strings.Split(repoURL, "/")
		if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
			repoURL = "https://github.com/" + repoURL
		}
	}

	// Ask if entire repository or specific paths (default to yes)
	entireRepo, err := ioc.Confirm("Do you want to install for the entire repository?", true)
	if err != nil {
		return lockfile.Scope{}, err
	}

	var paths []string
	if !entireRepo {
		// Collect paths
		paths, err = promptForRepositoryPaths(styledOut, repoURL, ioc)
		if err != nil {
			return lockfile.Scope{}, err
		}
	}

	return lockfile.Scope{
		Repo:  repoURL,
		Paths: paths,
	}, nil
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
