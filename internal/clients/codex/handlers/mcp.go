package handlers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/handlers/dirasset"
	"github.com/sleuth-io/sx/internal/metadata"
	"github.com/sleuth-io/sx/internal/utils"
)

var mcpOps = dirasset.NewOperations(DirMCPServers, &asset.TypeMCP)

// MCPHandler handles MCP asset installation for Codex (both packaged and config-only)
type MCPHandler struct {
	metadata *metadata.Metadata
}

// NewMCPHandler creates a new MCP handler
func NewMCPHandler(meta *metadata.Metadata) *MCPHandler {
	return &MCPHandler{metadata: meta}
}

// Install installs an MCP asset to Codex by updating config.toml.
// For packaged assets, extracts files first. For config-only, registers as-is.
func (h *MCPHandler) Install(ctx context.Context, zipData []byte, targetBase string) error {
	hasContent, err := utils.HasContentFiles(zipData)
	if err != nil {
		return fmt.Errorf("failed to inspect zip contents: %w", err)
	}

	var entry MCPServerEntry
	if hasContent {
		// Packaged mode: extract MCP server files
		serverDir := filepath.Join(targetBase, DirMCPServers, h.metadata.Asset.Name)
		if err := utils.ExtractZip(zipData, serverDir); err != nil {
			return fmt.Errorf("failed to extract MCP server: %w", err)
		}
		entry = h.generatePackagedMCPEntry(serverDir)
	} else {
		// Config-only mode: no extraction needed
		entry = h.generateConfigOnlyMCPEntry()
	}

	// Add to config.toml
	configPath := mcpConfigPath(targetBase)
	if err := AddMCPServer(configPath, h.metadata.Asset.Name, entry); err != nil {
		return fmt.Errorf("failed to update config.toml: %w", err)
	}

	return nil
}

// Remove removes an MCP entry from Codex
func (h *MCPHandler) Remove(ctx context.Context, targetBase string) error {
	// Remove from config.toml
	configPath := mcpConfigPath(targetBase)
	if err := RemoveMCPServer(configPath, h.metadata.Asset.Name); err != nil {
		return fmt.Errorf("failed to remove from config.toml: %w", err)
	}

	// Remove server directory if it exists (packaged mode)
	serverDir := filepath.Join(targetBase, DirMCPServers, h.metadata.Asset.Name)
	os.RemoveAll(serverDir) // Ignore errors if doesn't exist

	return nil
}

// VerifyInstalled checks if the MCP server is properly installed.
func (h *MCPHandler) VerifyInstalled(targetBase string) (bool, string) {
	// Check if install directory exists (packaged mode)
	installDir := filepath.Join(targetBase, DirMCPServers, h.metadata.Asset.Name)
	if utils.IsDirectory(installDir) {
		return mcpOps.VerifyInstalled(targetBase, h.metadata.Asset.Name, h.metadata.Asset.Version)
	}

	// Config-only mode: check config.toml for server entry
	configPath := mcpConfigPath(targetBase)
	return VerifyMCPServerInstalled(configPath, h.metadata.Asset.Name)
}

func (h *MCPHandler) generatePackagedMCPEntry(serverDir string) MCPServerEntry {
	mcpConfig := h.metadata.MCP

	return MCPServerEntry{
		Command: utils.ResolveCommand(mcpConfig.Command, serverDir),
		Args:    utils.ResolveArgs(mcpConfig.Args, serverDir),
		Env:     mcpConfig.Env,
	}
}

func (h *MCPHandler) generateConfigOnlyMCPEntry() MCPServerEntry {
	mcpConfig := h.metadata.MCP

	if mcpConfig.IsRemote() {
		return MCPServerEntry{
			URL: mcpConfig.URL,
			Env: mcpConfig.Env,
		}
	}

	return MCPServerEntry{
		Command: mcpConfig.Command,
		Args:    mcpConfig.Args,
		Env:     mcpConfig.Env,
	}
}

// mcpConfigPath returns the path to the Codex config.toml file
func mcpConfigPath(targetBase string) string {
	return filepath.Join(targetBase, "config.toml")
}

// MCPServerEntry represents a single MCP server entry in config.toml
// Codex uses [mcp_servers.<name>] table format, not [[mcp]] array
type MCPServerEntry struct {
	Command string            `toml:"command,omitempty"`
	Args    []string          `toml:"args,omitempty"`
	URL     string            `toml:"url,omitempty"`
	Env     map[string]string `toml:"env,omitempty"`
}

// CodexConfig represents the relevant parts of Codex's config.toml
type CodexConfig struct {
	MCPServers map[string]MCPServerEntry `toml:"mcp_servers"`
	// Other fields are preserved as raw data
	Other map[string]any `toml:"-"`
}

// ReadCodexConfig reads the Codex config.toml file
func ReadCodexConfig(path string) (*CodexConfig, map[string]any, error) {
	config := &CodexConfig{
		MCPServers: make(map[string]MCPServerEntry),
	}

	if !utils.FileExists(path) {
		return config, make(map[string]any), nil
	}

	// Read raw data to preserve other fields
	var raw map[string]any
	if _, err := toml.DecodeFile(path, &raw); err != nil {
		return nil, nil, fmt.Errorf("failed to parse config.toml: %w", err)
	}

	// Decode MCP servers from [mcp_servers.<name>] tables
	if mcpServers, ok := raw["mcp_servers"].(map[string]any); ok {
		for name, serverData := range mcpServers {
			server := MCPServerEntry{}
			if serverMap, ok := serverData.(map[string]any); ok {
				if command, ok := serverMap["command"].(string); ok {
					server.Command = command
				}
				if args, ok := serverMap["args"].([]any); ok {
					for _, arg := range args {
						if s, ok := arg.(string); ok {
							server.Args = append(server.Args, s)
						}
					}
				}
				if url, ok := serverMap["url"].(string); ok {
					server.URL = url
				}
				if env, ok := serverMap["env"].(map[string]any); ok {
					server.Env = make(map[string]string)
					for k, v := range env {
						if s, ok := v.(string); ok {
							server.Env[k] = s
						}
					}
				}
			}
			config.MCPServers[name] = server
		}
	}

	return config, raw, nil
}

// WriteCodexConfig writes the Codex config.toml file, preserving other fields
func WriteCodexConfig(path string, config *CodexConfig, raw map[string]any) error {
	// Update MCP servers in raw data using [mcp_servers.<name>] format
	if len(config.MCPServers) > 0 {
		mcpServers := make(map[string]any)
		for name, server := range config.MCPServers {
			entry := make(map[string]any)
			if server.Command != "" {
				entry["command"] = server.Command
			}
			if len(server.Args) > 0 {
				entry["args"] = server.Args
			}
			if server.URL != "" {
				entry["url"] = server.URL
			}
			if len(server.Env) > 0 {
				entry["env"] = server.Env
			}
			mcpServers[name] = entry
		}
		raw["mcp_servers"] = mcpServers
	} else {
		delete(raw, "mcp_servers")
	}

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Write config
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create config.toml: %w", err)
	}
	defer f.Close()

	encoder := toml.NewEncoder(f)
	if err := encoder.Encode(raw); err != nil {
		return fmt.Errorf("failed to write config.toml: %w", err)
	}

	return nil
}

// AddMCPServer adds or updates an MCP server entry in config.toml
func AddMCPServer(configPath, serverName string, entry MCPServerEntry) error {
	config, raw, err := ReadCodexConfig(configPath)
	if err != nil {
		return err
	}

	// Add or update the server entry
	config.MCPServers[serverName] = entry

	return WriteCodexConfig(configPath, config, raw)
}

// RemoveMCPServer removes an MCP server entry from config.toml
func RemoveMCPServer(configPath, serverName string) error {
	config, raw, err := ReadCodexConfig(configPath)
	if err != nil {
		return err
	}

	// Remove the server
	delete(config.MCPServers, serverName)

	return WriteCodexConfig(configPath, config, raw)
}

// VerifyMCPServerInstalled checks if a named MCP server is registered in config.toml
func VerifyMCPServerInstalled(configPath, serverName string) (bool, string) {
	config, _, err := ReadCodexConfig(configPath)
	if err != nil {
		return false, "failed to read config.toml: " + err.Error()
	}

	if _, exists := config.MCPServers[serverName]; exists {
		return true, "installed"
	}

	return false, "MCP server not registered"
}
