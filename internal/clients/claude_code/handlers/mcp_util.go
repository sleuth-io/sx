package handlers

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sleuth-io/sx/internal/utils"
)

// AddMCPServer adds or updates an MCP server entry in .claude.json
// For Claude Code, MCP servers are configured in ~/.claude.json (global) or .claude/.mcp.json (project)
func AddMCPServer(targetBase, serverName string, serverConfig map[string]any) error {
	// Claude Code stores MCP servers globally in .claude.json
	mcpConfigPath := filepath.Join(targetBase, ".claude.json")

	// Read existing config or create new
	var config map[string]any
	if utils.FileExists(mcpConfigPath) {
		data, err := os.ReadFile(mcpConfigPath)
		if err != nil {
			return fmt.Errorf("failed to read .claude.json: %w", err)
		}
		if err := json.Unmarshal(data, &config); err != nil {
			return fmt.Errorf("failed to parse .claude.json: %w", err)
		}
	} else {
		config = make(map[string]any)
	}

	// Ensure mcpServers section exists
	if config["mcpServers"] == nil {
		config["mcpServers"] = make(map[string]any)
	}
	mcpServers := config["mcpServers"].(map[string]any)

	// Add/update MCP server entry
	mcpServers[serverName] = serverConfig

	// Write updated config
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal MCP config: %w", err)
	}

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(mcpConfigPath), 0755); err != nil {
		return fmt.Errorf("failed to create directory for .claude.json: %w", err)
	}

	if err := os.WriteFile(mcpConfigPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write .claude.json: %w", err)
	}

	return nil
}

// RemoveMCPServer removes an MCP server entry from .claude.json
func RemoveMCPServer(targetBase, serverName string) error {
	mcpConfigPath := filepath.Join(targetBase, ".claude.json")

	if !utils.FileExists(mcpConfigPath) {
		return nil // Nothing to remove
	}

	// Read config
	data, err := os.ReadFile(mcpConfigPath)
	if err != nil {
		return fmt.Errorf("failed to read .claude.json: %w", err)
	}

	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse .claude.json: %w", err)
	}

	// Check if mcpServers section exists
	if config["mcpServers"] == nil {
		return nil
	}
	mcpServers := config["mcpServers"].(map[string]any)

	// Remove this MCP server
	delete(mcpServers, serverName)

	// Write updated config
	data, err = json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal MCP config: %w", err)
	}

	if err := os.WriteFile(mcpConfigPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write .claude.json: %w", err)
	}

	return nil
}
