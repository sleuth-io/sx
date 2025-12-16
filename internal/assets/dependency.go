package assets

import (
	"fmt"

	"github.com/sleuth-io/skills/internal/lockfile"
)

// DependencyResolver resolves artifact dependencies
type DependencyResolver struct {
	artifacts map[string]*lockfile.Artifact
}

// NewDependencyResolver creates a new dependency resolver
func NewDependencyResolver(lockFile *lockfile.LockFile) *DependencyResolver {
	artifactMap := make(map[string]*lockfile.Artifact)
	for i := range lockFile.Artifacts {
		artifactMap[lockFile.Artifacts[i].Name] = &lockFile.Artifacts[i]
	}

	return &DependencyResolver{
		artifacts: artifactMap,
	}
}

// Resolve resolves dependencies and returns artifacts in topological order
func (r *DependencyResolver) Resolve(artifacts []*lockfile.Artifact) ([]*lockfile.Artifact, error) {
	// Build dependency graph
	graph := make(map[string][]string)
	inDegree := make(map[string]int)
	artifactSet := make(map[string]*lockfile.Artifact)

	// Initialize graph
	for _, artifact := range artifacts {
		graph[artifact.Name] = []string{}
		inDegree[artifact.Name] = 0
		artifactSet[artifact.Name] = artifact
	}

	// Build edges (if A depends on B, then B -> A)
	for _, artifact := range artifacts {
		for _, dep := range artifact.Dependencies {
			// Check if dependency is in the artifact set
			if artifactSet[dep.Name] == nil {
				// Dependency not in the set, try to find it in the full artifacts map
				if r.artifacts[dep.Name] == nil {
					return nil, fmt.Errorf("dependency not found: %s (required by %s)", dep.Name, artifact.Name)
				}
				// Add the dependency to the set
				depArtifact := r.artifacts[dep.Name]
				artifactSet[dep.Name] = depArtifact
				graph[dep.Name] = []string{}
				inDegree[dep.Name] = 0

				// Recursively add dependencies of the dependency
				if err := r.addDependenciesRecursive(depArtifact, graph, inDegree, artifactSet); err != nil {
					return nil, err
				}
			}

			// Add edge: dependency -> artifact
			graph[dep.Name] = append(graph[dep.Name], artifact.Name)
			inDegree[artifact.Name]++
		}
	}

	// Topological sort using Kahn's algorithm
	var result []*lockfile.Artifact
	var queue []string

	// Find nodes with no incoming edges
	for name, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, name)
		}
	}

	// Process queue
	for len(queue) > 0 {
		// Dequeue
		current := queue[0]
		queue = queue[1:]

		// Add to result
		if artifactSet[current] != nil {
			result = append(result, artifactSet[current])
		}

		// Process neighbors
		for _, neighbor := range graph[current] {
			inDegree[neighbor]--
			if inDegree[neighbor] == 0 {
				queue = append(queue, neighbor)
			}
		}
	}

	// Check for cycles
	if len(result) != len(artifactSet) {
		return nil, fmt.Errorf("circular dependency detected")
	}

	return result, nil
}

// addDependenciesRecursive recursively adds dependencies to the graph
func (r *DependencyResolver) addDependenciesRecursive(artifact *lockfile.Artifact, graph map[string][]string, inDegree map[string]int, artifactSet map[string]*lockfile.Artifact) error {
	for _, dep := range artifact.Dependencies {
		if artifactSet[dep.Name] == nil {
			// Dependency not yet added
			if r.artifacts[dep.Name] == nil {
				return fmt.Errorf("dependency not found: %s (required by %s)", dep.Name, artifact.Name)
			}

			depArtifact := r.artifacts[dep.Name]
			artifactSet[dep.Name] = depArtifact
			graph[dep.Name] = []string{}
			inDegree[dep.Name] = 0

			// Recursively add its dependencies
			if err := r.addDependenciesRecursive(depArtifact, graph, inDegree, artifactSet); err != nil {
				return err
			}
		}

		// Add edge if not already present
		if !contains(graph[dep.Name], artifact.Name) {
			graph[dep.Name] = append(graph[dep.Name], artifact.Name)
			inDegree[artifact.Name]++
		}
	}

	return nil
}

// contains checks if a slice contains a string
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// ValidateDependencies checks that all dependencies are present and resolvable
func ValidateDependencies(lockFile *lockfile.LockFile) error {
	artifactMap := make(map[string]*lockfile.Artifact)
	for i := range lockFile.Artifacts {
		artifactMap[lockFile.Artifacts[i].Name] = &lockFile.Artifacts[i]
	}

	// Check each artifact's dependencies
	for _, artifact := range lockFile.Artifacts {
		for _, dep := range artifact.Dependencies {
			if artifactMap[dep.Name] == nil {
				return fmt.Errorf("artifact %s depends on %s, which is not in the lock file", artifact.Name, dep.Name)
			}

			// If version is specified, check it matches
			if dep.Version != "" {
				foundArtifact := artifactMap[dep.Name]
				if foundArtifact.Version != dep.Version {
					return fmt.Errorf("artifact %s requires %s@%s, but lock file has %s", artifact.Name, dep.Name, dep.Version, foundArtifact.Version)
				}
			}
		}
	}

	return nil
}
