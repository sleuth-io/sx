package lockfile

import (
	"fmt"
	"regexp"

	"github.com/Masterminds/semver/v3"
)

var (
	// gitCommitSHARegex matches full 40-character Git commit SHAs
	gitCommitSHARegex = regexp.MustCompile(`^[0-9a-f]{40}$`)

	// nameRegex matches valid artifact names (alphanumeric, dashes, underscores)
	nameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
)

// Validate validates the entire lock file
func (lf *LockFile) Validate() error {
	// Validate top-level fields
	if lf.LockVersion == "" {
		return fmt.Errorf("lock-version is required")
	}

	if lf.Version == "" {
		return fmt.Errorf("version is required")
	}

	if lf.CreatedBy == "" {
		return fmt.Errorf("created-by is required")
	}

	// Validate each artifact
	names := make(map[string]bool)
	for i, artifact := range lf.Artifacts {
		if err := artifact.Validate(); err != nil {
			return fmt.Errorf("artifact %d (%s): %w", i, artifact.Name, err)
		}

		// Check for duplicate artifacts (name@version must be unique)
		key := artifact.Key()
		if names[key] {
			return fmt.Errorf("duplicate artifact: %s", key)
		}
		names[key] = true
	}

	// Validate dependencies reference existing artifacts
	artifactMap := make(map[string]*Artifact)
	for i := range lf.Artifacts {
		artifactMap[lf.Artifacts[i].Name] = &lf.Artifacts[i]
	}

	for i, artifact := range lf.Artifacts {
		for _, dep := range artifact.Dependencies {
			if err := validateDependency(&dep, artifactMap, &artifact); err != nil {
				return fmt.Errorf("artifact %d (%s): dependency %s: %w", i, artifact.Name, dep.Name, err)
			}
		}
	}

	return nil
}

// Validate validates a single artifact
func (a *Artifact) Validate() error {
	// Validate required fields
	if a.Name == "" {
		return fmt.Errorf("name is required")
	}

	if !nameRegex.MatchString(a.Name) {
		return fmt.Errorf("name must contain only alphanumeric characters, dashes, and underscores")
	}

	if a.Version == "" {
		return fmt.Errorf("version is required")
	}

	// Validate semantic version
	if _, err := semver.NewVersion(a.Version); err != nil {
		return fmt.Errorf("invalid semantic version %q: %w", a.Version, err)
	}

	if a.Type == "" {
		return fmt.Errorf("type is required")
	}

	if !a.Type.IsValid() {
		return fmt.Errorf("invalid artifact type: %s", a.Type)
	}

	// Validate exactly one source is specified
	sourceCount := 0
	if a.SourceHTTP != nil {
		sourceCount++
	}
	if a.SourcePath != nil {
		sourceCount++
	}
	if a.SourceGit != nil {
		sourceCount++
	}

	if sourceCount == 0 {
		return fmt.Errorf("exactly one source must be specified (http, path, or git)")
	}
	if sourceCount > 1 {
		return fmt.Errorf("only one source type can be specified")
	}

	// Validate source-specific requirements
	if a.SourceHTTP != nil {
		if err := a.SourceHTTP.Validate(); err != nil {
			return fmt.Errorf("source-http: %w", err)
		}
	}
	if a.SourcePath != nil {
		if err := a.SourcePath.Validate(); err != nil {
			return fmt.Errorf("source-path: %w", err)
		}
	}
	if a.SourceGit != nil {
		if err := a.SourceGit.Validate(); err != nil {
			return fmt.Errorf("source-git: %w", err)
		}
	}

	// Validate repositories
	for i, repo := range a.Repositories {
		if err := repo.Validate(); err != nil {
			return fmt.Errorf("repositories[%d]: %w", i, err)
		}
	}

	return nil
}

// Validate validates a Repository entry
func (r *Repository) Validate() error {
	if r.Repo == "" {
		return fmt.Errorf("repo is required")
	}

	return nil
}

// Validate validates an HTTP source
func (s *SourceHTTP) Validate() error {
	if s.URL == "" {
		return fmt.Errorf("url is required")
	}

	// Hashes are required for HTTP sources
	if len(s.Hashes) == 0 {
		return fmt.Errorf("hashes are required for HTTP sources")
	}

	// Validate hash algorithms
	for algo := range s.Hashes {
		if algo != "sha256" && algo != "sha512" {
			return fmt.Errorf("unsupported hash algorithm: %s (must be sha256 or sha512)", algo)
		}
	}

	return nil
}

// Validate validates a path source
func (s *SourcePath) Validate() error {
	if s.Path == "" {
		return fmt.Errorf("path is required")
	}
	return nil
}

// Validate validates a Git source
func (s *SourceGit) Validate() error {
	if s.URL == "" {
		return fmt.Errorf("url is required")
	}

	if s.Ref == "" {
		return fmt.Errorf("ref is required")
	}

	// In lock files, ref must be a full commit SHA
	if !gitCommitSHARegex.MatchString(s.Ref) {
		return fmt.Errorf("ref must be a full 40-character commit SHA (got %q)", s.Ref)
	}

	return nil
}

// validateDependency validates a dependency reference
func validateDependency(dep *Dependency, artifactMap map[string]*Artifact, parent *Artifact) error {
	if dep.Name == "" {
		return fmt.Errorf("dependency name is required")
	}

	// Check if dependency exists in lock file
	artifact, exists := artifactMap[dep.Name]
	if !exists {
		return fmt.Errorf("dependency not found in lock file")
	}

	// If version is specified, it must match
	if dep.Version != "" && dep.Version != artifact.Version {
		return fmt.Errorf("dependency version %q does not match artifact version %q", dep.Version, artifact.Version)
	}

	// Check for self-dependency
	if dep.Name == parent.Name {
		return fmt.Errorf("artifact cannot depend on itself")
	}

	return nil
}

// ValidateDependencies checks for circular dependencies using DFS
func (lf *LockFile) ValidateDependencies() error {
	// Build dependency graph
	graph := make(map[string][]string)
	for _, artifact := range lf.Artifacts {
		deps := make([]string, 0, len(artifact.Dependencies))
		for _, dep := range artifact.Dependencies {
			deps = append(deps, dep.Name)
		}
		graph[artifact.Name] = deps
	}

	// Check each artifact for circular dependencies
	for _, artifact := range lf.Artifacts {
		visited := make(map[string]bool)
		recStack := make(map[string]bool)

		if hasCycle(artifact.Name, graph, visited, recStack) {
			return fmt.Errorf("circular dependency detected involving %s", artifact.Name)
		}
	}

	return nil
}

// hasCycle detects cycles in the dependency graph using DFS
func hasCycle(node string, graph map[string][]string, visited, recStack map[string]bool) bool {
	visited[node] = true
	recStack[node] = true

	for _, neighbor := range graph[node] {
		if !visited[neighbor] {
			if hasCycle(neighbor, graph, visited, recStack) {
				return true
			}
		} else if recStack[neighbor] {
			return true
		}
	}

	recStack[node] = false
	return false
}
