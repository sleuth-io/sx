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
	// Parse URL
	u, err := url.Parse(repoURL)
	if err != nil {
		// If parsing fails, use the original URL
		return strings.ToLower(strings.TrimSuffix(repoURL, ".git"))
	}

	// Remove .git suffix
	path := strings.TrimSuffix(u.Path, ".git")

	// Reconstruct normalized URL (host + path, lowercase)
	normalized := strings.ToLower(u.Host + path)

	return normalized
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
