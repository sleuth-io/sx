package assets

import (
	"errors"
	"fmt"
	"slices"

	"github.com/sleuth-io/sx/internal/lockfile"
)

// DependencyResolver resolves asset dependencies
type DependencyResolver struct {
	assets map[string]*lockfile.Asset
}

// NewDependencyResolver creates a new dependency resolver
func NewDependencyResolver(lockFile *lockfile.LockFile) *DependencyResolver {
	assetMap := make(map[string]*lockfile.Asset)
	for i := range lockFile.Assets {
		assetMap[lockFile.Assets[i].Name] = &lockFile.Assets[i]
	}

	return &DependencyResolver{
		assets: assetMap,
	}
}

// Resolve resolves dependencies and returns assets in topological order
func (r *DependencyResolver) Resolve(assets []*lockfile.Asset) ([]*lockfile.Asset, error) {
	// Build dependency graph
	graph := make(map[string][]string)
	inDegree := make(map[string]int)
	assetSet := make(map[string]*lockfile.Asset)

	// Initialize graph
	for _, asset := range assets {
		graph[asset.Name] = []string{}
		inDegree[asset.Name] = 0
		assetSet[asset.Name] = asset
	}

	// Build edges (if A depends on B, then B -> A)
	for _, asset := range assets {
		for _, dep := range asset.Dependencies {
			// Check if dependency is in the asset set
			if assetSet[dep.Name] == nil {
				// Dependency not in the set, try to find it in the full assets map
				if r.assets[dep.Name] == nil {
					return nil, fmt.Errorf("dependency not found: %s (required by %s)", dep.Name, asset.Name)
				}
				// Add the dependency to the set
				depAsset := r.assets[dep.Name]
				assetSet[dep.Name] = depAsset
				graph[dep.Name] = []string{}
				inDegree[dep.Name] = 0

				// Recursively add dependencies of the dependency
				if err := r.addDependenciesRecursive(depAsset, graph, inDegree, assetSet); err != nil {
					return nil, err
				}
			}

			// Add edge: dependency -> asset
			graph[dep.Name] = append(graph[dep.Name], asset.Name)
			inDegree[asset.Name]++
		}
	}

	// Topological sort using Kahn's algorithm
	var result []*lockfile.Asset
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
		if assetSet[current] != nil {
			result = append(result, assetSet[current])
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
	if len(result) != len(assetSet) {
		return nil, errors.New("circular dependency detected")
	}

	return result, nil
}

// addDependenciesRecursive recursively adds dependencies to the graph
func (r *DependencyResolver) addDependenciesRecursive(asset *lockfile.Asset, graph map[string][]string, inDegree map[string]int, assetSet map[string]*lockfile.Asset) error {
	for _, dep := range asset.Dependencies {
		if assetSet[dep.Name] == nil {
			// Dependency not yet added
			if r.assets[dep.Name] == nil {
				return fmt.Errorf("dependency not found: %s (required by %s)", dep.Name, asset.Name)
			}

			depAsset := r.assets[dep.Name]
			assetSet[dep.Name] = depAsset
			graph[dep.Name] = []string{}
			inDegree[dep.Name] = 0

			// Recursively add its dependencies
			if err := r.addDependenciesRecursive(depAsset, graph, inDegree, assetSet); err != nil {
				return err
			}
		}

		// Add edge if not already present
		if !contains(graph[dep.Name], asset.Name) {
			graph[dep.Name] = append(graph[dep.Name], asset.Name)
			inDegree[asset.Name]++
		}
	}

	return nil
}

// contains checks if a slice contains a string
func contains(slice []string, item string) bool {
	return slices.Contains(slice, item)
}

// ValidateDependencies checks that all dependencies are present and resolvable
func ValidateDependencies(lockFile *lockfile.LockFile) error {
	assetMap := make(map[string]*lockfile.Asset)
	for i := range lockFile.Assets {
		assetMap[lockFile.Assets[i].Name] = &lockFile.Assets[i]
	}

	// Check each asset's dependencies
	for _, asset := range lockFile.Assets {
		for _, dep := range asset.Dependencies {
			if assetMap[dep.Name] == nil {
				return fmt.Errorf("asset %s depends on %s, which is not in the lock file", asset.Name, dep.Name)
			}

			// If version is specified, check it matches
			if dep.Version != "" {
				foundAsset := assetMap[dep.Name]
				if foundAsset.Version != dep.Version {
					return fmt.Errorf("asset %s requires %s@%s, but lock file has %s", asset.Name, dep.Name, dep.Version, foundAsset.Version)
				}
			}
		}
	}

	return nil
}
