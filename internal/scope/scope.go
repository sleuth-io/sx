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

// Matches checks if an artifact's scope matches the current scope
func (m *Matcher) Matches(artifact *lockfile.Artifact) bool {
	artifactScope := artifact.GetScope()

	switch artifactScope {
	case "global":
		// Global artifacts always match
		return true

	case "repo":
		// Repo-scoped artifacts match if we're in the same repo
		if m.currentScope.Type == "global" {
			return false
		}
		return m.matchesRepo(artifact.Repo)

	case "path":
		// Path-scoped artifacts match if we're in the same repo and path
		if m.currentScope.Type != "path" {
			return false
		}
		return m.matchesRepo(artifact.Repo) && m.matchesPath(artifact.Path)

	default:
		// Unknown scope, default to not matching
		return false
	}
}

// matchesRepo checks if the artifact's repo matches the current repo
func (m *Matcher) matchesRepo(artifactRepo string) bool {
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

// FilterArtifacts filters artifacts based on the current scope
func FilterArtifacts(artifacts []lockfile.Artifact, currentScope *Scope) []lockfile.Artifact {
	matcher := NewMatcher(currentScope)

	var matched []lockfile.Artifact
	for _, artifact := range artifacts {
		if matcher.Matches(&artifact) {
			matched = append(matched, artifact)
		}
	}

	return matched
}

// GetInstallBase returns the installation base directory for a given artifact scope
// Returns the appropriate base directory based on scope precedence:
// 1. Path-specific: {repo-root}/{path}/.claude/
// 2. Repository-specific: {repo-root}/.claude/
// 3. Global: ~/.claude/
func GetInstallBase(artifact *lockfile.Artifact, repoRoot, globalBase string) string {
	switch artifact.GetScope() {
	case "path":
		// Path-specific installation
		return filepath.Join(repoRoot, artifact.Path, ".claude")
	case "repo":
		// Repository-specific installation
		return filepath.Join(repoRoot, ".claude")
	default:
		// Global installation
		return globalBase
	}
}
