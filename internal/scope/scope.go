package scope

import (
	"net/url"
	"path/filepath"
	"slices"
	"strings"

	"github.com/sleuth-io/sx/internal/lockfile"
)

// Re-export scope type constants from lockfile for convenience
const (
	TypeGlobal = lockfile.ScopeGlobal
	TypeRepo   = lockfile.ScopeRepo
	TypePath   = lockfile.ScopePath
)

// Matcher matches assets based on scope
type Matcher struct {
	currentScope *Scope
}

// Scope represents the current working context
type Scope struct {
	Type     lockfile.ScopeType // TypeGlobal, TypeRepo, or TypePath
	RepoURL  string             // Repository URL (if in a repo)
	RepoPath string             // Path relative to repo root (if applicable)
}

// NewMatcher creates a new scope matcher
func NewMatcher(currentScope *Scope) *Matcher {
	return &Matcher{
		currentScope: currentScope,
	}
}

// MatchesAsset checks if an asset should be installed in the current scope
// An asset matches if:
// - It's global (no scopes) OR
// - It has a scope entry that matches the current context
func (m *Matcher) MatchesAsset(asset *lockfile.Asset) bool {
	// Global assets (no repositories) always match
	if asset.IsGlobal() {
		return true
	}

	// Check each repository entry to see if any match
	for _, repo := range asset.Scopes {
		if m.matchesRepository(&repo) {
			return true
		}
	}

	return false
}

// matchesRepository checks if a repository entry matches the current scope
func (m *Matcher) matchesRepository(repo *lockfile.Scope) bool {
	// If we're in global scope, repository-specific assets don't match
	if m.currentScope.Type == TypeGlobal {
		return false
	}

	// Check if repo URL matches
	if !m.matchesRepoURL(repo.Repo) {
		return false
	}

	// If repository has no paths, it matches the entire repo
	if len(repo.Paths) == 0 {
		return true
	}

	// When at repo root, include ALL path-scoped assets for this repo
	// They will be installed to their respective paths
	if m.currentScope.Type == TypeRepo {
		return true
	}

	// If we're in a specific path, check if current path matches any of them
	return slices.ContainsFunc(repo.Paths, m.matchesPath)
}

// matchesRepoURL checks if the asset's repo matches the current repo
func (m *Matcher) matchesRepoURL(assetRepo string) bool {
	if m.currentScope.RepoURL == "" || assetRepo == "" {
		return false
	}

	// Normalize both URLs for comparison
	currentNormalized := NormalizeRepoURL(m.currentScope.RepoURL)
	assetNormalized := NormalizeRepoURL(assetRepo)

	return currentNormalized == assetNormalized
}

// matchesPath checks if the asset's path matches the current path
func (m *Matcher) matchesPath(assetPath string) bool {
	if m.currentScope.RepoPath == "" || assetPath == "" {
		return false
	}

	// Normalize paths
	currentPath := normalizeRepoPath(m.currentScope.RepoPath)
	assetPath = normalizeRepoPath(assetPath)

	// Check if current path is within or equal to asset path
	// For example, if asset is scoped to "services/api"
	// and we're in "services/api/handlers", it should match
	return strings.HasPrefix(currentPath, assetPath) || currentPath == assetPath
}

// MatchRepoURLs checks if two repository URLs refer to the same repository
func MatchRepoURLs(url1, url2 string) bool {
	return NormalizeRepoURL(url1) == NormalizeRepoURL(url2)
}

// NormalizeRepoURL normalizes a repository URL for comparison
func NormalizeRepoURL(repoURL string) string {
	// Normalize and clean the URL
	cleaned := strings.TrimSpace(strings.ToLower(repoURL))
	cleaned = strings.TrimSuffix(cleaned, ".git")

	// Extract host for checking if we should normalize SSH
	host := extractHost(cleaned)

	// Only normalize SSH URLs for known public git services
	if strings.HasPrefix(cleaned, "git@") && isKnownGitService(host) {
		// Extract host and path from git@host:owner/repo format
		cleaned = strings.TrimPrefix(cleaned, "git@")
		cleaned = strings.Replace(cleaned, ":", "/", 1) // Replace first : with /
		return cleaned
	}

	// Handle HTTPS URLs
	u, err := url.Parse(cleaned)
	if err != nil {
		// If parsing fails, return as-is
		return cleaned
	}

	// Return host + path (e.g., "github.com/owner/repo")
	return strings.TrimPrefix(u.Host+u.Path, "/")
}

// isKnownGitService checks if a host is a known public git service
func isKnownGitService(host string) bool {
	knownServices := []string{
		"github.com",
		"gitlab.com",
		"bitbucket.org",
		"codeberg.org",
	}

	for _, service := range knownServices {
		if strings.Contains(host, service) {
			return true
		}
	}
	return false
}

// extractHost extracts the host from a git URL
func extractHost(repoURL string) string {
	if after, ok := strings.CutPrefix(repoURL, "git@"); ok {
		// git@github.com:owner/repo
		parts := strings.SplitN(after, ":", 2)
		if len(parts) > 0 {
			return parts[0]
		}
	}

	u, err := url.Parse(repoURL)
	if err == nil {
		return u.Host
	}

	return ""
}

// normalizeRepoPath normalizes a repository-relative path
func normalizeRepoPath(path string) string {
	// Clean the path
	cleaned := filepath.Clean(path)

	// Remove leading slash or ./
	cleaned = strings.TrimPrefix(cleaned, "/")
	cleaned = strings.TrimPrefix(cleaned, "./")

	// Convert to forward slashes
	cleaned = filepath.ToSlash(cleaned)

	return cleaned
}

// GetInstallLocations returns all installation base directories for an asset in the current context
// An asset can have multiple installation locations if it has multiple repository entries
func GetInstallLocations(asset *lockfile.Asset, currentScope *Scope, repoRoot, globalBase string) []string {
	var locations []string

	// If global asset (no repositories), install to global base
	if asset.IsGlobal() {
		return []string{globalBase}
	}

	matcher := NewMatcher(currentScope)

	// Check each repository entry
	for _, repo := range asset.Scopes {
		if !matcher.matchesRepository(&repo) {
			continue
		}

		// If repository has paths, install to each path
		if len(repo.Paths) > 0 {
			for _, path := range repo.Paths {
				if matcher.matchesPath(path) {
					locations = append(locations, filepath.Join(repoRoot, path, ".claude"))
				}
			}
		} else {
			// No paths = entire repo
			locations = append(locations, filepath.Join(repoRoot, ".claude"))
		}
	}

	return locations
}
