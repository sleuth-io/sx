package commands

import (
	"fmt"
	"strings"

	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/ui"
	"github.com/sleuth-io/sx/internal/vault"
)

// formatRepository formats a repository entry for display
func formatRepository(repo lockfile.Scope) string {
	if len(repo.Paths) == 0 {
		return repo.Repo + " (entire repository)"
	}
	return fmt.Sprintf("%s → %s", repo.Repo, strings.Join(repo.Paths, ", "))
}

// formatTarget formats a kind-aware install target for display in the scope
// editor.
func formatTarget(t vault.InstallTarget) string {
	switch t.Kind {
	case vault.InstallKindRepo:
		return t.Repo + " (entire repository)"
	case vault.InstallKindPath:
		return fmt.Sprintf("%s → %s", t.Repo, strings.Join(t.Paths, ", "))
	case vault.InstallKindTeam:
		return "team: " + t.Team
	case vault.InstallKindUser:
		return "user: " + t.User
	case vault.InstallKindBot:
		return "bot: " + t.Bot
	case vault.InstallKindOrg:
		return "global (org-wide)"
	}
	return string(t.Kind)
}

// scopesToTargets converts the repo/path scopes the rest of the add flow uses
// into kind-aware install targets for the editor.
func scopesToTargets(scopes []lockfile.Scope) []vault.InstallTarget {
	targets := make([]vault.InstallTarget, 0, len(scopes))
	for _, s := range scopes {
		if len(s.Paths) > 0 {
			targets = append(targets, vault.InstallTarget{Kind: vault.InstallKindPath, Repo: s.Repo, Paths: s.Paths})
		} else {
			targets = append(targets, vault.InstallTarget{Kind: vault.InstallKindRepo, Repo: s.Repo})
		}
	}
	return targets
}

// targetsToScopes extracts the repo/path targets back into lockfile scopes, so
// vaults that can't persist identity scopes still get the repo/path subset.
// Team/user/bot targets are dropped here (they ride along in scopeResult.Targets).
func targetsToScopes(targets []vault.InstallTarget) []lockfile.Scope {
	scopes := []lockfile.Scope{}
	for _, t := range targets {
		if t.Kind == vault.InstallKindRepo || t.Kind == vault.InstallKindPath {
			scopes = append(scopes, lockfile.Scope{Repo: t.Repo, Paths: t.Paths})
		}
	}
	return scopes
}

// hasIdentityScope reports whether any target is a team/user/bot scope, i.e.
// a kind the repo/path lockfile model can't represent and that must be
// persisted through SetAssetInstallations.
func hasIdentityScope(targets []vault.InstallTarget) bool {
	for _, t := range targets {
		switch t.Kind {
		case vault.InstallKindTeam, vault.InstallKindUser, vault.InstallKindBot:
			return true
		}
	}
	return false
}

// displayCurrentTargets shows the asset's real current installation as
// kind-aware targets. installed distinguishes "not installed" from "installed
// globally" (an empty target set). Unlike displayCurrentInstallation it renders
// team/user/bot scopes too, so the line reflects the authoritative server view.
func displayCurrentTargets(targets []vault.InstallTarget, installed bool, styledOut *ui.Output) {
	styledOut.Newline()
	styledOut.Println("Current installation:")

	if !installed {
		styledOut.Println("  Not installed (available in vault only)")
		return
	}

	if len(targets) == 0 {
		styledOut.Println("  → Global (available in all projects)")
		return
	}

	items := make([]string, len(targets))
	for i, t := range targets {
		items[i] = formatTarget(t)
	}
	styledOut.List(items)
}

// displayCurrentInstallation shows the current installation state of an asset
func displayCurrentInstallation(currentRepos []lockfile.Scope, styledOut *ui.Output) {
	styledOut.Newline()
	styledOut.Println("Current installation:")

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

// diffTargets computes the change between the original install set and the
// edited working set, keyed by display identity (formatTarget). removed entries
// are taken from original (so they keep the server EntityID needed to uninstall
// by GID); added entries are taken from working.
func diffTargets(original, working []vault.InstallTarget) (added, removed []vault.InstallTarget) {
	originalKeys := make(map[string]bool, len(original))
	for _, t := range original {
		originalKeys[formatTarget(t)] = true
	}
	workingKeys := make(map[string]bool, len(working))
	for _, t := range working {
		workingKeys[formatTarget(t)] = true
	}
	for _, t := range original {
		if !workingKeys[formatTarget(t)] {
			removed = append(removed, t)
		}
	}
	for _, t := range working {
		if !originalKeys[formatTarget(t)] {
			added = append(added, t)
		}
	}
	return added, removed
}

// displayScopeChanges shows a diff-style preview of scope changes.
// Returns true if changes were detected, false otherwise.
func displayScopeChanges(before, after []vault.InstallTarget, styledOut *ui.Output) bool {
	beforeSet := make(map[string]bool, len(before))
	for _, t := range before {
		beforeSet[formatTarget(t)] = true
	}
	afterSet := make(map[string]bool, len(after))
	for _, t := range after {
		afterSet[formatTarget(t)] = true
	}

	var removed, added []string
	for key := range beforeSet {
		if !afterSet[key] {
			removed = append(removed, key)
		}
	}
	for key := range afterSet {
		if !beforeSet[key] {
			added = append(added, key)
		}
	}

	if len(removed) == 0 && len(added) == 0 {
		return false
	}

	styledOut.Newline()
	styledOut.Info("Changes to apply:")
	for _, key := range removed {
		styledOut.Printf("  - Removed: %s\n", styledOut.MutedText(key))
	}
	for _, key := range added {
		styledOut.Success("Added: " + key)
	}
	return true
}
