package manifest

import (
	"sort"

	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/mgmt"
	"github.com/sleuth-io/sx/internal/scope"
)

// Resolve produces a lockfile.LockFile from the manifest, flattening
// team/user scopes into repo scopes based on the caller's identity. The
// resulting lock file is the per-user, machine-generated artifact written
// to the caller's local lockfile cache; the manifest in the vault remains
// the source of truth.
//
// Resolution rules per scope on each asset:
//   - kind=org → asset is global (no scope restrictions in the lock).
//   - kind=repo → one lockfile.Scope with no paths.
//   - kind=path → one lockfile.Scope with paths.
//   - kind=user, user matches actor.Email → asset is global (mirrors the
//     server-side behavior: a user-specific install makes the asset
//     available anywhere that user works).
//   - kind=user, user does not match → scope is silently dropped (belongs
//     to another caller).
//   - kind=team, actor is a member → one lockfile.Scope per repository
//     owned by the team (empty paths, i.e. full-repo scope).
//   - kind=team, actor is not a member → scope is silently dropped.
//
// After accumulating scopes for an asset: if any row produced a global
// verdict, the asset's Scopes is nil. Otherwise repo-wide and
// path-restricted entries are deduped per normalized repo URL — a
// repo-wide entry wins over path-restricted entries for the same repo.
func Resolve(m *Manifest, actor mgmt.Actor) *lockfile.LockFile {
	if m == nil {
		return nil
	}

	actorEmail := mgmt.NormalizeEmail(actor.Email)

	out := &lockfile.LockFile{
		LockVersion: "1.0",
		Version:     "1",
		CreatedBy:   m.CreatedBy,
	}

	if len(m.Assets) == 0 {
		return out
	}

	out.Assets = make([]lockfile.Asset, 0, len(m.Assets))
	for i := range m.Assets {
		src := m.Assets[i]
		dst := lockfile.Asset{
			Name:         src.Name,
			Version:      src.Version,
			Type:         src.Type,
			Clients:      append([]string(nil), src.Clients...),
			Dependencies: resolveDependencies(src.Dependencies),
			SourceHTTP:   resolveSourceHTTP(src.SourceHTTP),
			SourcePath:   resolveSourcePath(src.SourcePath),
			SourceGit:    resolveSourceGit(src.SourceGit),
		}

		resolved, drop := resolveScopes(src.Scopes, m, actorEmail)
		if drop {
			continue
		}
		dst.Scopes = resolved
		out.Assets = append(out.Assets, dst)
	}
	return out
}

// resolveScopes applies the rules above to a single asset's scopes. The
// drop return is currently always false — no rule in the current spec
// excludes an asset entirely. It exists as a future hook if we add a
// "visible only to these users" rule later.
func resolveScopes(in []Scope, m *Manifest, actorEmail string) (_ []lockfile.Scope, drop bool) {
	if len(in) == 0 {
		return nil, false
	}

	becameGlobal := false
	accumulated := make([]lockfile.Scope, 0, len(in))

	for _, s := range in {
		switch s.Kind {
		case ScopeKindOrg:
			becameGlobal = true
		case ScopeKindRepo:
			accumulated = append(accumulated, lockfile.Scope{Repo: s.Repo})
		case ScopeKindPath:
			accumulated = append(accumulated, lockfile.Scope{
				Repo:  s.Repo,
				Paths: append([]string(nil), s.Paths...),
			})
		case ScopeKindUser:
			if actorEmail != "" && mgmt.NormalizeEmail(s.User) == actorEmail {
				becameGlobal = true
			}
		case ScopeKindTeam:
			team, err := m.FindTeam(s.Team)
			if err != nil || team == nil {
				continue
			}
			if actorEmail == "" || !team.IsMember(actorEmail) {
				continue
			}
			for _, repoURL := range team.Repositories {
				accumulated = append(accumulated, lockfile.Scope{Repo: repoURL})
			}
		}
	}

	if becameGlobal {
		return nil, false
	}
	if len(accumulated) == 0 {
		// No scope applied to this actor. The manifest's intent was
		// scoped but this caller is outside every scope, so the lock
		// entry is empty-scoped. To avoid flipping this into global,
		// return an empty-but-non-nil slice so the asset is present
		// but unscoped. Downstream install code already handles the
		// "empty scopes" case as global, so we drop the asset
		// instead: it is not visible to this caller.
		return nil, true
	}
	return mergeScopes(accumulated), false
}

// mergeScopes dedupes on normalized repo URL and collapses path-restricted
// entries into a bare-repo entry when both are present for the same repo.
func mergeScopes(in []lockfile.Scope) []lockfile.Scope {
	type key struct{ repo string }
	type agg struct {
		repo     string
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

func resolveDependencies(in []Dependency) []lockfile.Dependency {
	if len(in) == 0 {
		return nil
	}
	out := make([]lockfile.Dependency, len(in))
	for i, d := range in {
		out[i] = lockfile.Dependency{Name: d.Name, Version: d.Version}
	}
	return out
}

func resolveSourceHTTP(in *SourceHTTP) *lockfile.SourceHTTP {
	if in == nil {
		return nil
	}
	return &lockfile.SourceHTTP{
		URL:    in.URL,
		Hashes: copyStringMap(in.Hashes),
		Size:   in.Size,
	}
}

func resolveSourcePath(in *SourcePath) *lockfile.SourcePath {
	if in == nil {
		return nil
	}
	return &lockfile.SourcePath{Path: in.Path}
}

func resolveSourceGit(in *SourceGit) *lockfile.SourceGit {
	if in == nil {
		return nil
	}
	return &lockfile.SourceGit{
		URL:          in.URL,
		Ref:          in.Ref,
		Subdirectory: in.Subdirectory,
	}
}
