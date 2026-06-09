package commands

import (
	"strings"

	"github.com/sleuth-io/sx/internal/lockfile"
)

// addOptions contains flags for non-interactive mode
type addOptions struct {
	Yes       bool
	NoInstall bool
	Browse    bool
	Name      string
	Type      string
	Version   string

	// Unified scope flags (shared vocabulary with `sx install`, resolved by
	// resolveScopeFlags). When any is set, the scope is pre-filled as if the
	// user navigated the menu to it, then the same confirmation is shown
	// (unless --yes). Each kind is repeatable.
	Org        bool
	Repos      []string
	Paths      []string
	Teams      []string
	Users      []string
	Bots       []string
	AddToScope bool // --add-to-scope: append instead of replace

	// Legacy scope flags — forwarded into the unified set (see toScopeFlags).
	// Kept as deprecated aliases so existing scripts keep working.
	ScopeGlobal bool
	ScopeRepos  []string
	Scope       string // --scope: vault-specific scope entity (e.g., "personal")
}

// isNonInteractive returns true if any non-interactive flag is set
func (o addOptions) isNonInteractive() bool {
	return o.Yes || o.Name != "" || o.Type != "" || o.Version != "" || o.hasScopeFlags()
}

// hasScopeFlags reports whether the user named any scope target (unified or
// legacy). When true, the add flow pre-fills that scope and asks for
// confirmation instead of opening the interactive menu.
func (o addOptions) hasScopeFlags() bool {
	return o.Org || len(o.Repos) > 0 || len(o.Paths) > 0 || len(o.Teams) > 0 ||
		len(o.Users) > 0 || len(o.Bots) > 0 ||
		o.ScopeGlobal || len(o.ScopeRepos) > 0 || o.Scope != ""
}

// toScopeFlags folds the unified and legacy flags into the single scopeFlags
// struct resolveScopeFlags understands. Legacy mappings: --scope-global → --org,
// --scope-repo → --repo (or --path when it carries a #path spec), and
// --scope personal → --user me.
func (o addOptions) toScopeFlags() scopeFlags {
	f := scopeFlags{
		Org:   o.Org || o.ScopeGlobal,
		Repos: append([]string(nil), o.Repos...),
		Paths: append([]string(nil), o.Paths...),
		Teams: append([]string(nil), o.Teams...),
		Users: append([]string(nil), o.Users...),
		Bots:  append([]string(nil), o.Bots...),
		Add:   o.AddToScope,
	}
	for _, spec := range o.ScopeRepos {
		if _, paths := parseRepoSpec(spec); len(paths) > 0 {
			f.Paths = append(f.Paths, spec)
		} else {
			f.Repos = append(f.Repos, spec)
		}
	}
	if o.Scope == "personal" {
		f.Users = append(f.Users, "me")
	}
	return f
}

// getScopes returns the scopes based on flags
// Returns: (*scopeResult, error)
// - Scope: vault-specific scope entity (e.g., "personal")
// - ScopeGlobal: empty slice (global install)
// - ScopeRepos: slice with repo scopes (parsed from "repo#path1,path2" format)
// - Neither + NoInstall: remove (vault only, no lock file update)
// - Neither + Yes (no scope flags): Inherit=true (preserve existing installations)
//
// Note: Validation of mutually exclusive flags (--scope-global with --scope-repo, --scope)
// is performed in runAddWithFlags for early error reporting. This function
// assumes valid input.
func (o addOptions) getScopes() (*scopeResult, error) {
	if o.Scope != "" {
		return &scopeResult{ScopeEntity: o.Scope}, nil
	}
	if o.ScopeGlobal {
		return &scopeResult{Scopes: []lockfile.Scope{}}, nil
	}
	if len(o.ScopeRepos) > 0 {
		scopes := make([]lockfile.Scope, len(o.ScopeRepos))
		for i, repoSpec := range o.ScopeRepos {
			repo, paths := parseRepoSpec(repoSpec)
			scopes[i] = lockfile.Scope{Repo: repo, Paths: paths}
		}
		return &scopeResult{Scopes: scopes}, nil
	}
	if o.Yes {
		return &scopeResult{Inherit: true}, nil // No scope flags: inherit existing installations
	}
	return &scopeResult{Remove: true}, nil // No scope flags = vault only (with --no-install)
}

// parseRepoSpec parses "repo#path1,path2" format
// Returns repo URL and slice of paths (nil if no paths specified)
//
// Note: Uses # as delimiter, so repo URLs containing # (e.g., URL fragments)
// are not supported. Standard git remote URLs (SSH, HTTPS) don't use fragments.
func parseRepoSpec(spec string) (string, []string) {
	repo, pathStr, found := strings.Cut(spec, "#")
	if !found {
		return spec, nil
	}
	if pathStr == "" {
		return repo, nil
	}
	paths := strings.Split(pathStr, ",")
	// Trim whitespace from paths
	for i := range paths {
		paths[i] = strings.TrimSpace(paths[i])
	}
	return repo, paths
}
