package clients

import (
	"context"
	"sync"
)

// Orchestrator coordinates installation across multiple clients
type Orchestrator struct {
	registry *Registry
}

// NewOrchestrator creates a new installation orchestrator
func NewOrchestrator(registry *Registry) *Orchestrator {
	return &Orchestrator{registry: registry}
}

// InstallToAll installs assets to all detected clients concurrently
func (o *Orchestrator) InstallToAll(ctx context.Context,
	assets []*AssetBundle,
	scope *InstallScope,
	options InstallOptions) map[string]InstallResponse {
	clients := o.registry.DetectInstalled()
	return o.InstallToClients(ctx, assets, scope, options, clients)
}

// InstallToClients installs assets to specific clients concurrently
func (o *Orchestrator) InstallToClients(ctx context.Context,
	assets []*AssetBundle,
	scope *InstallScope,
	options InstallOptions,
	targetClients []Client) map[string]InstallResponse {
	// Install to clients concurrently
	results := make(map[string]InstallResponse)
	resultsMu := sync.Mutex{}
	wg := sync.WaitGroup{}

	for _, client := range targetClients {
		wg.Add(1)
		go func(client Client) {
			defer wg.Done()

			// Filter assets by client compatibility and scope support
			compatibleAssets := o.filterAssets(assets, client, scope)

			if len(compatibleAssets) == 0 {
				resultsMu.Lock()
				results[client.ID()] = InstallResponse{
					Results: []AssetResult{
						{
							Status:  StatusSkipped,
							Message: "No compatible assets",
						},
					},
				}
				resultsMu.Unlock()
				return
			}

			// Let the client handle installation however it wants
			req := InstallRequest{
				Assets:  compatibleAssets,
				Scope:   scope,
				Options: options,
			}

			resp, err := client.InstallAssets(ctx, req)
			if err != nil {
				// Client returned error - ensure all results marked as failed
				for i := range resp.Results {
					if resp.Results[i].Status != StatusFailed {
						resp.Results[i].Status = StatusFailed
						if resp.Results[i].Error == nil {
							resp.Results[i].Error = err
						}
					}
				}
			}

			resultsMu.Lock()
			results[client.ID()] = resp
			resultsMu.Unlock()
		}(client)
	}

	wg.Wait()
	return results
}

// filterAssets returns assets compatible with client and scope
func (o *Orchestrator) filterAssets(assets []*AssetBundle,
	client Client,
	scope *InstallScope) []*AssetBundle {
	compatible := make([]*AssetBundle, 0)

	for _, bundle := range assets {
		// Check if client supports this asset type
		if !client.SupportsAssetType(bundle.Asset.Type) {
			continue
		}

		// If asset is scoped to repo/path and this is a global scope,
		// skip it (client doesn't support repo scope)
		if !bundle.Asset.IsGlobal() && scope.Type == ScopeGlobal {
			continue
		}

		compatible = append(compatible, bundle)
	}

	return compatible
}

// HasAnyErrors checks if any client installation failed
func HasAnyErrors(results map[string]InstallResponse) bool {
	for _, resp := range results {
		for _, result := range resp.Results {
			if result.Status == StatusFailed {
				return true
			}
		}
	}
	return false
}
