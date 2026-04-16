package mgmt

import (
	"sort"

	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/scope"
)

// OverlayInstallations returns a new lock file with team/user installations
// from ifile layered on top of each matching asset's existing Scopes. When
// ifile is nil or empty, the base lock file is returned unchanged (the fast
// path — git vaults with no team/user installs pay zero overlay cost).
//
// This is the client-side mirror of skills.new's server-side lock-file
// generation: every team/user installation visible to the given actor is
// flattened into the lockfile.Scope format that the existing sx install
// pipeline already understands. See sleuth/apps/issues/skills/storage.py:851
// for the reference implementation.
//
// Overlay rules per installation matching this asset:
//   - kind=user, user matches actor.Email → mark asset global (clear Scopes)
//   - kind=user, user does not match      → drop (row belongs to another caller)
//   - kind=team, actor is team member     → append Scope{Repo: url} for each team repo
//   - kind=team, actor is not team member → drop
//
// After layering: if any row marked the asset global, Scopes is nil;
// otherwise the Scopes slice is deduplicated and collapsed so a bare repo
// entry wins over a path-restricted entry for the same repo.
func OverlayInstallations(base *lockfile.LockFile, teams *TeamsFile, ifile *InstallationsFile, actor Actor) *lockfile.LockFile {
	if base == nil {
		return nil
	}
	if ifile == nil || len(ifile.Installations) == 0 {
		return base
	}

	result := *base
	result.Assets = make([]lockfile.Asset, len(base.Assets))
	copy(result.Assets, base.Assets)

	for i := range result.Assets {
		applyOverlayToAsset(&result.Assets[i], teams, ifile, actor)
	}
	return &result
}

// applyOverlayToAsset mutates a single asset's Scopes according to the
// overlay rules.
func applyOverlayToAsset(asset *lockfile.Asset, teams *TeamsFile, ifile *InstallationsFile, actor Actor) {
	installations := ifile.ForAsset(asset.Name)
	if len(installations) == 0 {
		return
	}

	// If the asset is already org-wide (no scopes), there's nothing an
	// additive team/user overlay can do to restrict it — leave it alone.
	if len(asset.Scopes) == 0 {
		return
	}

	addedScopes := make([]lockfile.Scope, 0)
	becameGlobal := false

	actorEmail := NormalizeEmail(actor.Email)

	for _, ins := range installations {
		switch ins.Kind {
		case InstallKindUser:
			if ins.User == actorEmail && actorEmail != "" {
				becameGlobal = true
			}
		case InstallKindTeam:
			if teams == nil {
				continue
			}
			team, err := teams.FindTeam(ins.Team)
			if err != nil {
				continue
			}
			if actorEmail == "" || !team.IsMember(actorEmail) {
				continue
			}
			for _, repoURL := range team.Repositories {
				addedScopes = append(addedScopes, lockfile.Scope{Repo: repoURL})
			}
		}
	}

	if becameGlobal {
		asset.Scopes = nil
		return
	}
	if len(addedScopes) == 0 {
		return
	}

	merged := make([]lockfile.Scope, 0, len(asset.Scopes)+len(addedScopes))
	merged = append(merged, asset.Scopes...)
	merged = append(merged, addedScopes...)
	asset.Scopes = mergeScopes(merged)
}

// mergeScopes dedupes scope entries after URL normalization and collapses
// path-restricted entries into bare-repo entries when both are present for
// the same repo (same rule as skills.new storage.py:894-895).
func mergeScopes(in []lockfile.Scope) []lockfile.Scope {
	type key struct{ repo string }
	type agg struct {
		repo     string // original, unnormalized
		pathWide bool
		paths    []string
		seen     map[string]struct{}
	}
	byRepo := make(map[key]*agg)
	order := make([]key, 0, len(in))

	for _, s := range in {
		k := key{scope.NormalizeRepoURL(s.Repo)}
		a, ok := byRepo[k]
		if !ok {
			a = &agg{repo: s.Repo, seen: make(map[string]struct{})}
			byRepo[k] = a
			order = append(order, k)
		}
		if len(s.Paths) == 0 {
			a.pathWide = true
			continue
		}
		for _, p := range s.Paths {
			if _, dup := a.seen[p]; dup {
				continue
			}
			a.seen[p] = struct{}{}
			a.paths = append(a.paths, p)
		}
	}

	out := make([]lockfile.Scope, 0, len(order))
	for _, k := range order {
		a := byRepo[k]
		if a.pathWide {
			out = append(out, lockfile.Scope{Repo: a.repo})
			continue
		}
		sort.Strings(a.paths)
		out = append(out, lockfile.Scope{Repo: a.repo, Paths: a.paths})
	}
	return out
}
