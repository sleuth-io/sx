package commands

import (
	"strings"

	"github.com/sleuth-io/sx/internal/lockfile"
)

// addOptions contains flags for non-interactive mode
type addOptions struct {
	Yes         bool
	NoInstall   bool
	Browse      bool
	Name        string
	Type        string
	Version     string
	ScopeGlobal bool
	ScopeRepos  []string
	Scope       string // --scope: vault-specific scope entity (e.g., "personal")
}

// isNonInteractive returns true if any non-interactive flag is set
func (o addOptions) isNonInteractive() bool {
	return o.Yes || o.Name != "" || o.Type != "" || o.Version != "" || o.ScopeGlobal || len(o.ScopeRepos) > 0 || o.Scope != ""
}

// getScopes returns the scopes based on flags
// Returns: (*scopeResult, error)
// - Scope: vault-specific scope entity (e.g., "personal")
// - ScopeGlobal: empty slice (global install)
// - ScopeRepos: slice with repo scopes (parsed from "repo#path1,path2" format)
// - Neither + NoInstall: remove (vault only, no lock file update)
// - Neither + Yes: empty slice (default to global)
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
		return &scopeResult{Scopes: []lockfile.Scope{}}, nil // Default to global with --yes
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
