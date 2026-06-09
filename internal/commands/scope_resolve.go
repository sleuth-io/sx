package commands

import (
	"errors"
	"fmt"

	vaultpkg "github.com/sleuth-io/sx/internal/vault"
)

// scopeMode is the single, command-agnostic way to express how a set of scope
// flags applies: REPLACE makes the named flags the asset's complete scope set
// (anything unnamed is dropped); ADD appends them to whatever scope already
// exists. REPLACE is the default; --add-to-scope selects ADD.
type scopeMode int

const (
	scopeReplace scopeMode = iota
	scopeAdd
)

// scopeFlags is the unified scope-flag set shared by `sx add` and `sx install`.
// Each kind is repeatable. The whole point is that both commands parse their
// flags into this struct and feed it to resolveScopeFlags, so identical flags
// produce identical scope regardless of which command was invoked.
type scopeFlags struct {
	Org   bool     // --org (global, exclusive)
	Repos []string // --repo <url>
	Paths []string // --path <url#p1,p2>
	Teams []string // --team <name>
	Users []string // --user <email>
	Bots  []string // --bot <name>
	Add   bool     // --add-to-scope (append instead of replace)
}

// scopeChange is the resolved outcome of a scopeFlags: a mode plus the ordered
// list of install targets to apply.
type scopeChange struct {
	Mode    scopeMode
	Targets []vaultpkg.InstallTarget
}

// resolveScopeFlags is the single, pure resolver both `sx add` and `sx install`
// run: flags in, (mode + ordered targets) out, with no vault or actor knowledge.
// Actor-dependent checks (user self-only, team existence) and URL normalization
// stay in the vault layer — repo URLs pass through here unchanged.
//
// Rules:
//   - Default mode is REPLACE; --add-to-scope selects ADD.
//   - --org is exclusive: it resolves to a single global target and cannot be
//     combined with any other target or with --add-to-scope.
//   - Within a kind, input order is preserved; across kinds the order is fixed —
//     repos, then paths, then teams, then users, then bots — so commit messages
//     and audit output are stable.
//   - At least one target is required in either mode; bare flags (including a
//     lone --add-to-scope) are an error.
func resolveScopeFlags(f scopeFlags) (scopeChange, error) {
	if f.Org {
		if f.Add {
			return scopeChange{}, errors.New("--org cannot be combined with --add-to-scope")
		}
		if len(f.Repos) > 0 || len(f.Paths) > 0 || len(f.Teams) > 0 || len(f.Users) > 0 || len(f.Bots) > 0 {
			return scopeChange{}, errors.New("--org is exclusive and cannot be combined with other scope targets")
		}
		return scopeChange{
			Mode:    scopeReplace,
			Targets: []vaultpkg.InstallTarget{{Kind: vaultpkg.InstallKindOrg}},
		}, nil
	}

	var targets []vaultpkg.InstallTarget
	for _, repo := range f.Repos {
		targets = append(targets, vaultpkg.InstallTarget{Kind: vaultpkg.InstallKindRepo, Repo: repo})
	}
	for _, spec := range f.Paths {
		repo, paths := parseRepoSpec(spec)
		if repo == "" || len(paths) == 0 {
			return scopeChange{}, fmt.Errorf("--path %q must be in the form repo_url#path1,path2", spec)
		}
		targets = append(targets, vaultpkg.InstallTarget{Kind: vaultpkg.InstallKindPath, Repo: repo, Paths: paths})
	}
	for _, team := range f.Teams {
		targets = append(targets, vaultpkg.InstallTarget{Kind: vaultpkg.InstallKindTeam, Team: team})
	}
	for _, user := range f.Users {
		targets = append(targets, vaultpkg.InstallTarget{Kind: vaultpkg.InstallKindUser, User: user})
	}
	for _, bot := range f.Bots {
		targets = append(targets, vaultpkg.InstallTarget{Kind: vaultpkg.InstallKindBot, Bot: bot})
	}

	if len(targets) == 0 {
		if f.Add {
			return scopeChange{}, errors.New("--add-to-scope requires at least one scope target (--repo/--path/--team/--user/--bot)")
		}
		return scopeChange{}, errors.New("no scope specified: name at least one of --org, --repo, --path, --team, --user, --bot")
	}

	mode := scopeReplace
	if f.Add {
		mode = scopeAdd
	}
	return scopeChange{Mode: mode, Targets: targets}, nil
}
