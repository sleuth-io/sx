package commands

import (
	"fmt"
	"strings"

	"github.com/sleuth-io/skills/internal/lockfile"
	"github.com/sleuth-io/skills/internal/ui"
)

// formatRepository formats a repository entry for display
func formatRepository(repo lockfile.Repository) string {
	if len(repo.Paths) == 0 {
		return fmt.Sprintf("%s (entire repository)", repo.Repo)
	}
	return fmt.Sprintf("%s → %s", repo.Repo, strings.Join(repo.Paths, ", "))
}

// formatPaths formats a list of paths for display
func formatPaths(paths []string) string {
	if len(paths) == 0 {
		return "(entire repository)"
	}
	return strings.Join(paths, ", ")
}

// displayCurrentInstallation shows the current installation state of an artifact
func displayCurrentInstallation(currentRepos []lockfile.Repository, styledOut *ui.Output) {
	styledOut.Newline()
	styledOut.Info("Current installation:")

	if currentRepos == nil {
		styledOut.Println("  Not installed (available in vault only)")
		return
	}

	if len(currentRepos) == 0 {
		// Global installation
		styledOut.Println("  → Global (available in all projects)")
		return
	}

	// Repository-specific installations
	styledOut.Println("  → Repository-specific")
	items := make([]string, len(currentRepos))
	for i, repo := range currentRepos {
		items[i] = formatRepository(repo)
	}
	styledOut.List(items)
}

// displayRepositoryList shows a numbered list of current repositories
func displayRepositoryList(repos []lockfile.Repository, styledOut *ui.Output) {
	styledOut.Newline()
	if len(repos) == 0 {
		styledOut.Muted("  (none - currently global or not installed)")
		return
	}

	styledOut.Println("Current repositories:")
	for i, repo := range repos {
		styledOut.Printf("  %d. %s\n", i+1, formatRepository(repo))
	}
}

// repositoriesEqual checks if two repository slices are equal
func repositoriesEqual(a, b []lockfile.Repository) bool {
	if len(a) != len(b) {
		return false
	}

	// Create maps for comparison
	aMap := make(map[string][]string)
	for _, repo := range a {
		aMap[repo.Repo] = repo.Paths
	}

	bMap := make(map[string][]string)
	for _, repo := range b {
		bMap[repo.Repo] = repo.Paths
	}

	// Compare maps
	for repo, paths := range aMap {
		bPaths, exists := bMap[repo]
		if !exists || len(paths) != len(bPaths) {
			return false
		}

		// Compare paths (order matters)
		for i := range paths {
			if paths[i] != bPaths[i] {
				return false
			}
		}
	}

	return true
}

// displayRepositoryChanges shows a diff-style preview of repository changes
// Returns true if changes were detected, false otherwise
func displayRepositoryChanges(before, after []lockfile.Repository, styledOut *ui.Output) bool {
	// Check if there are any changes
	if repositoriesEqual(before, after) {
		return false
	}

	styledOut.Newline()
	styledOut.Info("Changes to apply:")

	// Build maps for easier comparison
	beforeMap := make(map[string][]string)
	for _, repo := range before {
		beforeMap[repo.Repo] = repo.Paths
	}

	afterMap := make(map[string][]string)
	for _, repo := range after {
		afterMap[repo.Repo] = repo.Paths
	}

	// Find removed repositories
	for repo, paths := range beforeMap {
		if _, exists := afterMap[repo]; !exists {
			repoFormatted := formatRepository(lockfile.Repository{Repo: repo, Paths: paths})
			styledOut.Printf("  - Removed: %s\n", styledOut.MutedText(repoFormatted))
		}
	}

	// Find added repositories
	for repo, paths := range afterMap {
		if _, exists := beforeMap[repo]; !exists {
			repoFormatted := formatRepository(lockfile.Repository{Repo: repo, Paths: paths})
			styledOut.Success(fmt.Sprintf("Added: %s", repoFormatted))
		}
	}

	// Find modified repositories (same repo, different paths)
	for repo, afterPaths := range afterMap {
		if beforePaths, exists := beforeMap[repo]; exists {
			// Check if paths changed
			if len(beforePaths) != len(afterPaths) {
				styledOut.Warning(fmt.Sprintf("Modified: %s", repo))
				styledOut.Printf("      Before: %s\n", formatPaths(beforePaths))
				styledOut.Printf("      After:  %s\n", formatPaths(afterPaths))
				continue
			}

			pathsChanged := false
			for i := range beforePaths {
				if beforePaths[i] != afterPaths[i] {
					pathsChanged = true
					break
				}
			}

			if pathsChanged {
				styledOut.Warning(fmt.Sprintf("Modified: %s", repo))
				styledOut.Printf("      Before: %s\n", formatPaths(beforePaths))
				styledOut.Printf("      After:  %s\n", formatPaths(afterPaths))
			}
		}
	}

	return true
}
