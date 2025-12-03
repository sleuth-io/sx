package scope

import (
	"net/url"
	"path/filepath"
	"strings"

	"github.com/sleuth-io/skills/internal/lockfile"
)

// Matcher matches artifacts based on scope
type Matcher struct {
	currentScope *Scope
}

// Scope represents the current working context
type Scope struct {
	Type     string // "global", "repo", or "path"
	RepoURL  string // Repository URL (if in a repo)
	RepoPath string // Path relative to repo root (if applicable)
}

// NewMatcher creates a new scope matcher
func NewMatcher(currentScope *Scope) *Matcher {
	return &Matcher{
		currentScope: currentScope,
	}
}

// MatchesArtifact checks if an artifact should be installed in the current scope
// An artifact matches if:
// - It's global (no repositories) OR
// - It has a repository entry that matches the current context
func (m *Matcher) MatchesArtifact(artifact *lockfile.Artifact) bool {
	// Global artifacts (no repositories) always match
	if artifact.IsGlobal() {
		return true
	}

	// Check each repository entry to see if any match
	for _, repo := range artifact.Repositories {
		if m.matchesRepository(&repo) {
			return true
		}
	}

	return false
}

// matchesRepository checks if a repository entry matches the current scope
func (m *Matcher) matchesRepository(repo *lockfile.Repository) bool {
	// If we're in global scope, repository-specific artifacts don't match
	if m.currentScope.Type == "global" {
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

	// If repository has paths, check if current path matches any of them
	if m.currentScope.Type != "path" {
		return false
	}

	for _, path := range repo.Paths {
		if m.matchesPath(path) {
			return true
		}
	}

	return false
}

// matchesRepoURL checks if the artifact's repo matches the current repo
func (m *Matcher) matchesRepoURL(artifactRepo string) bool {
	if m.currentScope.RepoURL == "" || artifactRepo == "" {
		return false
	}

	// Normalize both URLs for comparison
	currentNormalized := normalizeRepoURL(m.currentScope.RepoURL)
	artifactNormalized := normalizeRepoURL(artifactRepo)

	return currentNormalized == artifactNormalized
}

// matchesPath checks if the artifact's path matches the current path
func (m *Matcher) matchesPath(artifactPath string) bool {
	if m.currentScope.RepoPath == "" || artifactPath == "" {
		return false
	}

	// Normalize paths
	currentPath := normalizeRepoPath(m.currentScope.RepoPath)
	artifactPath = normalizeRepoPath(artifactPath)

	// Check if current path is within or equal to artifact path
	// For example, if artifact is scoped to "services/api"
	// and we're in "services/api/handlers", it should match
	return strings.HasPrefix(currentPath, artifactPath) || currentPath == artifactPath
}

// normalizeRepoURL normalizes a repository URL for comparison
func normalizeRepoURL(repoURL string) string {
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
	if strings.HasPrefix(repoURL, "git@") {
		// git@github.com:owner/repo
		parts := strings.SplitN(strings.TrimPrefix(repoURL, "git@"), ":", 2)
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

// GetInstallLocations returns all installation base directories for an artifact in the current context
// An artifact can have multiple installation locations if it has multiple repository entries
func GetInstallLocations(artifact *lockfile.Artifact, currentScope *Scope, repoRoot, globalBase string) []string {
	var locations []string

	// If global artifact (no repositories), install to global base
	if artifact.IsGlobal() {
		return []string{globalBase}
	}

	matcher := NewMatcher(currentScope)

	// Check each repository entry
	for _, repo := range artifact.Repositories {
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
