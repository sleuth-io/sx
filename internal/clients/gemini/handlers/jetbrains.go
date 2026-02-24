package handlers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// JetBrainsProduct represents a JetBrains IDE product
type JetBrainsProduct struct {
	Name    string // e.g., "IntelliJIdea", "PyCharm", "GoLand"
	Version string // e.g., "2025.1"
	Path    string // Full path to config directory
}

// FindJetBrainsConfigDirs finds all JetBrains IDE config directories
func FindJetBrainsConfigDirs() ([]JetBrainsProduct, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	var basePath string
	switch runtime.GOOS {
	case "linux":
		basePath = filepath.Join(home, JetBrainsConfigLinux)
	case "darwin":
		basePath = filepath.Join(home, JetBrainsConfigMacOS)
	case "windows":
		basePath = filepath.Join(home, JetBrainsConfigWindows)
	default:
		basePath = filepath.Join(home, JetBrainsConfigLinux)
	}

	entries, err := os.ReadDir(basePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No JetBrains IDEs installed
		}
		return nil, err
	}

	var products []JetBrainsProduct
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		// Parse product name and version (e.g., "IntelliJIdea2025.1")
		product, version := parseProductVersion(name)
		if product != "" {
			products = append(products, JetBrainsProduct{
				Name:    product,
				Version: version,
				Path:    filepath.Join(basePath, name),
			})
		}
	}

	// Sort by version descending (newest first)
	sort.Slice(products, func(i, j int) bool {
		return products[i].Version > products[j].Version
	})

	return products, nil
}

// parseProductVersion splits "IntelliJIdea2025.1" into ("IntelliJIdea", "2025.1")
func parseProductVersion(name string) (string, string) {
	// Find where the version number starts (first digit after letters)
	for i, c := range name {
		if c >= '0' && c <= '9' {
			return name[:i], name[i:]
		}
	}
	return name, ""
}

// JetBrainsMCPConfig represents the mcp.json structure for JetBrains
type JetBrainsMCPConfig struct {
	MCPServers map[string]JetBrainsMCPServer `json:"mcpServers,omitempty"`
}

// JetBrainsMCPServer represents an MCP server entry in mcp.json
type JetBrainsMCPServer struct {
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	URL     string            `json:"url,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// ReadJetBrainsMCPConfig reads the mcp.json file from a JetBrains config dir
func ReadJetBrainsMCPConfig(configDir string) (*JetBrainsMCPConfig, error) {
	path := filepath.Join(configDir, JetBrainsMCPFile)
	config := &JetBrainsMCPConfig{
		MCPServers: make(map[string]JetBrainsMCPServer),
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return config, nil
		}
		return nil, err
	}

	if err := json.Unmarshal(data, config); err != nil {
		return nil, err
	}

	return config, nil
}

// WriteJetBrainsMCPConfig writes the mcp.json file to a JetBrains config dir
func WriteJetBrainsMCPConfig(configDir string, config *JetBrainsMCPConfig) error {
	path := filepath.Join(configDir, JetBrainsMCPFile)

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// AddJetBrainsMCPServer adds an MCP server to all JetBrains IDEs
func AddJetBrainsMCPServer(serverName string, server JetBrainsMCPServer) error {
	products, err := FindJetBrainsConfigDirs()
	if err != nil {
		return err
	}

	// Track which products we've updated (only latest version per product)
	updated := make(map[string]bool)

	for _, product := range products {
		// Skip if we've already updated this product (list is sorted newest first)
		if updated[product.Name] {
			continue
		}

		config, err := ReadJetBrainsMCPConfig(product.Path)
		if err != nil {
			continue // Skip this IDE on error
		}

		config.MCPServers[serverName] = server

		if err := WriteJetBrainsMCPConfig(product.Path, config); err != nil {
			continue // Skip this IDE on error
		}

		updated[product.Name] = true
	}

	return nil
}

// RemoveJetBrainsMCPServer removes an MCP server from all JetBrains IDEs
func RemoveJetBrainsMCPServer(serverName string) error {
	products, err := FindJetBrainsConfigDirs()
	if err != nil {
		return err
	}

	for _, product := range products {
		config, err := ReadJetBrainsMCPConfig(product.Path)
		if err != nil {
			continue
		}

		delete(config.MCPServers, serverName)

		if err := WriteJetBrainsMCPConfig(product.Path, config); err != nil {
			continue
		}
	}

	return nil
}

// HasJetBrainsMCPServer checks if an MCP server exists in any JetBrains IDE
func HasJetBrainsMCPServer(serverName string) bool {
	products, err := FindJetBrainsConfigDirs()
	if err != nil {
		return false
	}

	for _, product := range products {
		config, err := ReadJetBrainsMCPConfig(product.Path)
		if err != nil {
			continue
		}

		if _, exists := config.MCPServers[serverName]; exists {
			return true
		}
	}

	return false
}

// IsJetBrainsInstalled checks if any JetBrains IDE with Gemini plugin might be installed
func IsJetBrainsInstalled() bool {
	products, err := FindJetBrainsConfigDirs()
	if err != nil {
		return false
	}

	// Check if any product has the Gemini plugin or mcp.json
	for _, product := range products {
		// Check for mcp.json (indicates Gemini Code Assist might be configured)
		mcpPath := filepath.Join(product.Path, JetBrainsMCPFile)
		if _, err := os.Stat(mcpPath); err == nil {
			return true
		}

		// Check for Gemini plugin in plugins directory
		pluginsDir := filepath.Join(product.Path, "plugins")
		if entries, err := os.ReadDir(pluginsDir); err == nil {
			for _, entry := range entries {
				if strings.Contains(strings.ToLower(entry.Name()), "gemini") {
					return true
				}
			}
		}
	}

	return false
}
