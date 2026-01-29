package commands

import (
	"errors"
	"strings"

	"github.com/sleuth-io/sx/internal/lockfile"
)

// addOptions contains flags for non-interactive mode
type addOptions struct {
	Yes         bool
	NoInstall   bool
	Name        string
	Type        string
	Version     string
	ScopeGlobal bool
	ScopeRepos  []string
}

// isNonInteractive returns true if any non-interactive flag is set
func (o addOptions) isNonInteractive() bool {
	return o.Yes || o.Name != "" || o.Type != "" || o.Version != "" || o.ScopeGlobal || len(o.ScopeRepos) > 0
}

// getScopes returns the scopes based on flags
// Returns: (scopes, error)
// - ScopeGlobal: empty slice (global install)
// - ScopeRepos: slice with repo scopes (parsed from "repo#path1,path2" format)
// - Neither + NoInstall: nil (vault only, no lock file update)
// - Neither + Yes: empty slice (default to global)
func (o addOptions) getScopes() ([]lockfile.Scope, error) {
	if o.ScopeGlobal && len(o.ScopeRepos) > 0 {
		return nil, errors.New("cannot use --scope-global with --scope-repo")
	}
	if o.ScopeGlobal {
		return []lockfile.Scope{}, nil // Empty = global
	}
	if len(o.ScopeRepos) > 0 {
		scopes := make([]lockfile.Scope, len(o.ScopeRepos))
		for i, repoSpec := range o.ScopeRepos {
			repo, paths := parseRepoSpec(repoSpec)
			scopes[i] = lockfile.Scope{Repo: repo, Paths: paths}
		}
		return scopes, nil
	}
	if o.Yes {
		return []lockfile.Scope{}, nil // Default to global with --yes
	}
	return nil, nil // No scope flags = vault only (with --no-install)
}

// parseRepoSpec parses "repo#path1,path2" format
// Returns repo URL and slice of paths (nil if no paths specified)
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
