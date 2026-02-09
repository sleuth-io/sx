package handlers

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sleuth-io/sx/internal/utils"
)

// mcpConfigPath returns the correct Claude Code MCP config file path for the given targetBase.
// Claude Code reads MCP servers from:
//   - User scope (global): ~/.claude.json
//   - Project scope (repo): {repoRoot}/.mcp.json
func mcpConfigPath(targetBase string) string {
	home, _ := os.UserHomeDir()
	globalBase := filepath.Join(home, ".claude")

	if targetBase == globalBase {
		// Global install → user-level config at ~/.claude.json
		return filepath.Join(home, ".claude.json")
	}

	// Repo/path install → project-level config at {repoRoot}/.mcp.json
	// targetBase is {repoRoot}/.claude/ or {repoRoot}/{path}/.claude/
	repoRoot := filepath.Dir(targetBase)
	return filepath.Join(repoRoot, ".mcp.json")
}

// AddMCPServer adds or updates an MCP server entry in the appropriate Claude Code config file.
func AddMCPServer(targetBase, serverName string, serverConfig map[string]any) error {
	configPath := mcpConfigPath(targetBase)

	// Read existing config or create new
	var config map[string]any
	if utils.FileExists(configPath) {
		data, err := os.ReadFile(configPath)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", filepath.Base(configPath), err)
		}
		if err := json.Unmarshal(data, &config); err != nil {
			return fmt.Errorf("failed to parse %s: %w", filepath.Base(configPath), err)
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
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return fmt.Errorf("failed to create directory for %s: %w", filepath.Base(configPath), err)
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", filepath.Base(configPath), err)
	}

	return nil
}

// RemoveMCPServer removes an MCP server entry from the appropriate Claude Code config file.
func RemoveMCPServer(targetBase, serverName string) error {
	configPath := mcpConfigPath(targetBase)

	if !utils.FileExists(configPath) {
		return nil // Nothing to remove
	}

	// Read config
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", filepath.Base(configPath), err)
	}

	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse %s: %w", filepath.Base(configPath), err)
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

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", filepath.Base(configPath), err)
	}

	return nil
}

// VerifyMCPServerInstalled checks if a named MCP server is registered in the appropriate config file.
func VerifyMCPServerInstalled(targetBase, serverName string) (bool, string) {
	configPath := mcpConfigPath(targetBase)

	if !utils.FileExists(configPath) {
		return false, filepath.Base(configPath) + " not found"
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return false, "failed to read " + filepath.Base(configPath) + ": " + err.Error()
	}

	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		return false, "failed to parse " + filepath.Base(configPath) + ": " + err.Error()
	}

	mcpServers, ok := config["mcpServers"].(map[string]any)
	if !ok {
		return false, "mcpServers section not found"
	}

	if _, exists := mcpServers[serverName]; !exists {
		return false, "MCP server not registered"
	}

	return true, "installed"
}
