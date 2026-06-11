package commands

import (
	"fmt"
	"slices"
	"strings"

	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/ui"
	"github.com/sleuth-io/sx/internal/vault"
)

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

// targetKey returns a structural identity for an install target, independent of
// its human-facing label. Diff and preview maps key on this rather than
// formatTarget so two distinct targets that happen to render to the same string
// don't collapse, and a path target whose Paths differ only in order isn't
// mistaken for a different target.
func targetKey(t vault.InstallTarget) string {
	paths := append([]string(nil), t.Paths...)
	slices.Sort(paths)
	return strings.Join([]string{
		string(t.Kind),
		t.Repo,
		strings.Join(paths, "\x00"),
		t.Team,
		t.User,
		t.Bot,
	}, "\x1f")
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
		case vault.InstallKindOrg, vault.InstallKindRepo, vault.InstallKindPath:
			// not identity scopes
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

// diffTargets computes the change between the original install set and the
// edited working set, keyed by structural identity (targetKey). removed entries
// are taken from original (so they keep the server EntityID needed to uninstall
// by GID); added entries are taken from working.
func diffTargets(original, working []vault.InstallTarget) (added, removed []vault.InstallTarget) {
	originalKeys := make(map[string]bool, len(original))
	for _, t := range original {
		originalKeys[targetKey(t)] = true
	}
	workingKeys := make(map[string]bool, len(working))
	for _, t := range working {
		workingKeys[targetKey(t)] = true
	}
	for _, t := range original {
		if !workingKeys[targetKey(t)] {
			removed = append(removed, t)
		}
	}
	for _, t := range working {
		if !originalKeys[targetKey(t)] {
			added = append(added, t)
		}
	}
	return added, removed
}

// displayScopeChanges shows a diff-style preview of scope changes.
// Returns true if changes were detected, false otherwise.
func displayScopeChanges(before, after []vault.InstallTarget, styledOut *ui.Output) bool {
	// Map structural key → human label so distinct targets that render alike
	// don't collapse, while the preview still shows the friendly label.
	beforeLabels := make(map[string]string, len(before))
	for _, t := range before {
		beforeLabels[targetKey(t)] = formatTarget(t)
	}
	afterLabels := make(map[string]string, len(after))
	for _, t := range after {
		afterLabels[targetKey(t)] = formatTarget(t)
	}

	var removed, added []string
	for key, label := range beforeLabels {
		if _, ok := afterLabels[key]; !ok {
			removed = append(removed, label)
		}
	}
	for key, label := range afterLabels {
		if _, ok := beforeLabels[key]; !ok {
			added = append(added, label)
		}
	}

	if len(removed) == 0 && len(added) == 0 {
		return false
	}

	// Render added and removed symmetrically as a diff preview: a red "- " for
	// removals, a green "+ " for additions. Don't use Success() here — its ✓
	// reads as "already done", but this is a preview shown BEFORE the
	// "Continue?" confirmation. Sort each list so the preview is stable.
	slices.Sort(removed)
	slices.Sort(added)
	styledOut.Newline()
	styledOut.Info("Changes to apply:")
	for _, key := range removed {
		styledOut.Printf("  %s\n", styledOut.ErrorText("- "+key))
	}
	for _, key := range added {
		styledOut.Printf("  %s\n", styledOut.SuccessText("+ "+key))
	}
	return true
}
