package handlers

import (
	"fmt"
	"path/filepath"
)

// AddMCPServer adds or updates an MCP server entry in Cursor's mcp.json
func AddMCPServer(targetBase, serverName string, serverConfig map[string]interface{}) error {
	mcpConfigPath := filepath.Join(targetBase, "mcp.json")

	// Read existing config
	config, err := ReadMCPConfig(mcpConfigPath)
	if err != nil {
		return fmt.Errorf("failed to read mcp.json: %w", err)
	}

	// Add/update MCP server entry
	if config.MCPServers == nil {
		config.MCPServers = make(map[string]interface{})
	}
	config.MCPServers[serverName] = serverConfig

	// Write updated config
	if err := WriteMCPConfig(mcpConfigPath, config); err != nil {
		return fmt.Errorf("failed to write mcp.json: %w", err)
	}

	return nil
}

// RemoveMCPServer removes an MCP server entry from Cursor's mcp.json
func RemoveMCPServer(targetBase, serverName string) error {
	mcpConfigPath := filepath.Join(targetBase, "mcp.json")

	// Read existing config
	config, err := ReadMCPConfig(mcpConfigPath)
	if err != nil {
		return fmt.Errorf("failed to read mcp.json: %w", err)
	}

	// Remove the server
	delete(config.MCPServers, serverName)

	// Write updated config
	if err := WriteMCPConfig(mcpConfigPath, config); err != nil {
		return fmt.Errorf("failed to write mcp.json: %w", err)
	}

	return nil
}
