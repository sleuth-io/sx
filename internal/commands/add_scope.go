package commands

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/ui"
	"github.com/sleuth-io/sx/internal/ui/components"
)

// promptForRepositoriesWithUI prompts user for repository configurations using new UI
// Takes currentRepos (nil if not installed, empty slice if global, or list of repos)
// Returns nil, nil if user chooses not to install (which removes it from lock file if present)
func promptForRepositoriesWithUI(assetName, version string, currentRepos []lockfile.Scope, styledOut *ui.Output, ioc *components.IOContext) ([]lockfile.Scope, error) {
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
		{
			Label:       "Remove from installation",
			Value:       "remove",
			Description: "Uninstall this asset (keeps it in vault)",
		},
	}...)

	// Show selection menu
	selected, err := ioc.Select("What would you like to do?", options)
	if err != nil {
		// If user cancelled, treat it as "keep current" if installed, or "don't install" if not
		if err.Error() == "selection cancelled" {
			if currentRepos != nil {
				styledOut.Info("No changes made")
				return currentRepos, nil
			}
			styledOut.Info("Cancelled")
			return nil, nil
		}
		return nil, err
	}

	switch selected.Value {
	case "keep": // Keep current settings
		styledOut.Success(fmt.Sprintf("%s v%s - no changes made", assetName, version))
		return currentRepos, nil

	case "global": // Make it available globally
		styledOut.Success("Set to global installation")
		return []lockfile.Scope{}, nil // Empty array = global

	case "modify": // Add/modify repository-specific installations
		if currentRepos == nil {
			currentRepos = []lockfile.Scope{}
		}
		return modifyRepositories(currentRepos, styledOut, ioc)

	case "remove": // Remove from installation
		styledOut.Info("Removing from installation (will remain available in vault)")
		return nil, nil // nil means don't install

	default:
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
