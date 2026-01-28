package clients

import (
	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/assets/detectors"
)

// DetectAssetType asks clients to determine the asset type for a path.
// If no client claims it, falls back to generic path/content detection.
func (r *Registry) DetectAssetType(path string, content []byte) *asset.Type {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// First, ask each client
	for _, client := range r.clients {
		caps := client.RuleCapabilities()
		if caps != nil && caps.DetectAssetType != nil {
			if t := caps.DetectAssetType(path, content); t != nil {
				return t
			}
		}
	}

	// Generic fallback for files not claimed by any client
	return detectors.DetectAssetTypeFromPath(path, content)
}

// DetectAssetType uses the global registry
func DetectAssetType(path string, content []byte) *asset.Type {
	return globalRegistry.DetectAssetType(path, content)
}
