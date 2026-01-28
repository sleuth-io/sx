package clients

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/sleuth-io/sx/internal/asset"
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

// FilterByAssetType returns clients that support the given asset type
func (r *Registry) FilterByAssetType(assetType asset.Type) []Client {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var supported []Client
	for _, client := range r.clients {
		if client.SupportsAssetType(assetType) {
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

// Rule detection functions using client capabilities

// DetectClientFromPath asks each client if it owns this path.
// Returns the client and its capabilities, or nil if no client matches.
func (r *Registry) DetectClientFromPath(path string) (Client, *RuleCapabilities) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, client := range r.clients {
		caps := client.RuleCapabilities()
		if caps != nil && caps.MatchesPath != nil && caps.MatchesPath(path) {
			return client, caps
		}
	}
	return nil, nil
}

// DetectClientFromContent asks each client if it recognizes this content.
// Returns the client and its capabilities, or nil if no client matches.
func (r *Registry) DetectClientFromContent(path string, content []byte) (Client, *RuleCapabilities) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, client := range r.clients {
		caps := client.RuleCapabilities()
		if caps != nil && caps.MatchesContent != nil && caps.MatchesContent(path, content) {
			return client, caps
		}
	}
	return nil, nil
}

// IsRuleFile checks if any client recognizes this path as a rule file.
func (r *Registry) IsRuleFile(path string) bool {
	client, _ := r.DetectClientFromPath(path)
	return client != nil
}

// IsRuleContent checks if any client recognizes this content as a rule.
func (r *Registry) IsRuleContent(path string, content []byte) bool {
	client, _ := r.DetectClientFromContent(path, content)
	return client != nil
}

// IsInstructionFile checks if path matches any client's instruction files.
// Instruction files are files like CLAUDE.md or AGENTS.md that can contain
// multiple rule sections.
func (r *Registry) IsInstructionFile(path string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	base := filepath.Base(path)
	for _, client := range r.clients {
		caps := client.RuleCapabilities()
		if caps == nil {
			continue
		}
		for _, instrFile := range caps.InstructionFiles {
			if strings.EqualFold(base, instrFile) {
				return true
			}
		}
	}
	return false
}

// IsImportableFile checks if path can be imported as rules.
// This includes both rule files (e.g., .claude/rules/my-rule.md) and
// instruction files (e.g., CLAUDE.md).
func (r *Registry) IsImportableFile(path string) bool {
	return r.IsRuleFile(path) || r.IsInstructionFile(path)
}

// ParseRuleFile finds the right client and parses the content.
// If no client is detected, returns the raw content as a ParsedRule.
func (r *Registry) ParseRuleFile(path string, content []byte) (*ParsedRule, error) {
	// Try path-based detection first
	if _, caps := r.DetectClientFromPath(path); caps != nil && caps.ParseRuleFile != nil {
		return caps.ParseRuleFile(content)
	}

	// Fall back to content-based detection
	if _, caps := r.DetectClientFromContent(path, content); caps != nil && caps.ParseRuleFile != nil {
		return caps.ParseRuleFile(content)
	}

	// No client recognized it - return raw content
	return &ParsedRule{Content: string(content)}, nil
}

// GetRulesDirectories returns all registered rules directories.
func (r *Registry) GetRulesDirectories() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	dirs := make([]string, 0)
	for _, client := range r.clients {
		caps := client.RuleCapabilities()
		if caps != nil && caps.RulesDirectory != "" {
			dirs = append(dirs, caps.RulesDirectory)
		}
	}
	return dirs
}

// GetInstructionFiles returns all instruction files from all clients.
func (r *Registry) GetInstructionFiles() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	files := make([]string, 0)
	seen := make(map[string]bool)

	for _, client := range r.clients {
		caps := client.RuleCapabilities()
		if caps == nil {
			continue
		}
		for _, f := range caps.InstructionFiles {
			if !seen[f] {
				files = append(files, f)
				seen[f] = true
			}
		}
	}
	return files
}

// Convenience functions that use the global registry

// DetectClientFromPath uses the global registry
func DetectClientFromPath(path string) (Client, *RuleCapabilities) {
	return globalRegistry.DetectClientFromPath(path)
}

// DetectClientFromContent uses the global registry
func DetectClientFromContent(path string, content []byte) (Client, *RuleCapabilities) {
	return globalRegistry.DetectClientFromContent(path, content)
}

// IsRuleFile uses the global registry
func IsRuleFile(path string) bool {
	return globalRegistry.IsRuleFile(path)
}

// IsInstructionFile uses the global registry
func IsInstructionFile(path string) bool {
	return globalRegistry.IsInstructionFile(path)
}

// IsImportableFile uses the global registry
func IsImportableFile(path string) bool {
	return globalRegistry.IsImportableFile(path)
}

// ParseRuleFile uses the global registry
func ParseRuleFile(path string, content []byte) (*ParsedRule, error) {
	return globalRegistry.ParseRuleFile(path, content)
}
