package clients

import (
	"fmt"
	"sync"

	"github.com/sleuth-io/skills/internal/asset"
)

// Registry holds all registered clients
type Registry struct {
	mu      sync.RWMutex
	clients map[string]Client
}

var globalRegistry = NewRegistry()

// NewRegistry creates a new client registry
func NewRegistry() *Registry {
	return &Registry{
		clients: make(map[string]Client),
	}
}

// Register adds a client to the registry
func (r *Registry) Register(client Client) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clients[client.ID()] = client
}

// Get retrieves a client by ID
func (r *Registry) Get(id string) (Client, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	client, ok := r.clients[id]
	if !ok {
		return nil, fmt.Errorf("unknown client: %s", id)
	}
	return client, nil
}

// DetectInstalled returns all clients detected as installed
func (r *Registry) DetectInstalled() []Client {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var installed []Client
	for _, client := range r.clients {
		if client.IsInstalled() {
			installed = append(installed, client)
		}
	}
	return installed
}

// GetAll returns all registered clients
func (r *Registry) GetAll() []Client {
	r.mu.RLock()
	defer r.mu.RUnlock()

	clients := make([]Client, 0, len(r.clients))
	for _, client := range r.clients {
		clients = append(clients, client)
	}
	return clients
}

// FilterByArtifactType returns clients that support the given artifact type
func (r *Registry) FilterByArtifactType(artifactType asset.Type) []Client {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var supported []Client
	for _, client := range r.clients {
		if client.SupportsArtifactType(artifactType) {
			supported = append(supported, client)
		}
	}
	return supported
}

// Global returns the global registry
func Global() *Registry {
	return globalRegistry
}

// Register registers a client in the global registry
func Register(client Client) {
	globalRegistry.Register(client)
}
