package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sleuth-io/skills/internal/asset"
	"github.com/sleuth-io/skills/internal/handlers/dirasset"
	"github.com/sleuth-io/skills/internal/metadata"
	"github.com/sleuth-io/skills/internal/utils"
)

var mcpOps = dirasset.NewOperations("mcp-servers", &asset.TypeMCP)

// MCPHandler handles MCP artifact installation for Cursor
type MCPHandler struct {
	metadata *metadata.Metadata
}

// NewMCPHandler creates a new MCP handler
func NewMCPHandler(meta *metadata.Metadata) *MCPHandler {
	return &MCPHandler{metadata: meta}
}

// Install installs an MCP artifact to Cursor by updating mcp.json
func (h *MCPHandler) Install(ctx context.Context, zipData []byte, targetBase string) error {
	mcpConfigPath := filepath.Join(targetBase, "mcp.json")

	// Read existing mcp.json
	config, err := ReadMCPConfig(mcpConfigPath)
	if err != nil {
		return fmt.Errorf("failed to read mcp.json: %w", err)
	}

	// Extract MCP server files to .cursor/mcp-servers/{name}/
	serverDir := filepath.Join(targetBase, "mcp-servers", h.metadata.Artifact.Name)
	if err := utils.ExtractZip(zipData, serverDir); err != nil {
		return fmt.Errorf("failed to extract MCP server: %w", err)
	}

	// Generate MCP entry from metadata (with paths relative to extraction)
	entry := h.generateMCPEntry(serverDir)

	// Add to config
	if config.MCPServers == nil {
		config.MCPServers = make(map[string]interface{})
	}
	config.MCPServers[h.metadata.Artifact.Name] = entry

	// Write updated mcp.json
	if err := WriteMCPConfig(mcpConfigPath, config); err != nil {
		return fmt.Errorf("failed to write mcp.json: %w", err)
	}

	return nil
}

// Remove removes an MCP entry from Cursor
func (h *MCPHandler) Remove(ctx context.Context, targetBase string) error {
	mcpConfigPath := filepath.Join(targetBase, "mcp.json")

	// Read existing mcp.json
	config, err := ReadMCPConfig(mcpConfigPath)
	if err != nil {
		return fmt.Errorf("failed to read mcp.json: %w", err)
	}

	// Remove entry
	delete(config.MCPServers, h.metadata.Artifact.Name)

	// Write updated mcp.json
	if err := WriteMCPConfig(mcpConfigPath, config); err != nil {
		return fmt.Errorf("failed to write mcp.json: %w", err)
	}

	// Remove server directory (if exists)
	serverDir := filepath.Join(targetBase, "mcp-servers", h.metadata.Artifact.Name)
	os.RemoveAll(serverDir) // Ignore errors if doesn't exist

	return nil
}

func (h *MCPHandler) generateMCPEntry(serverDir string) map[string]interface{} {
	mcpConfig := h.metadata.MCP

	// Convert relative command paths to absolute (relative to server directory)
	command := mcpConfig.Command
	if !filepath.IsAbs(command) {
		command = filepath.Join(serverDir, command)
	}

	// Convert relative args paths to absolute
	args := make([]interface{}, len(mcpConfig.Args))
	for i, arg := range mcpConfig.Args {
		// If arg looks like a relative path (contains / or \), make it absolute
		if !filepath.IsAbs(arg) && (filepath.Base(arg) != arg) {
			args[i] = filepath.Join(serverDir, arg)
		} else {
			args[i] = arg
		}
	}

	entry := map[string]interface{}{
		"command": command,
		"args":    args,
	}

	// Add env if present
	if len(mcpConfig.Env) > 0 {
		entry["env"] = mcpConfig.Env
	}

	return entry
}

// MCPConfig represents Cursor's mcp.json structure
type MCPConfig struct {
	MCPServers map[string]interface{} `json:"mcpServers"`
}

// ReadMCPConfig reads Cursor's mcp.json file
func ReadMCPConfig(path string) (*MCPConfig, error) {
	config := &MCPConfig{
		MCPServers: make(map[string]interface{}),
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return config, nil // Return empty config
		}
		return nil, err
	}

	if err := json.Unmarshal(data, config); err != nil {
		return nil, err
	}

	return config, nil
}

// WriteMCPConfig writes Cursor's mcp.json file
func WriteMCPConfig(path string, config *MCPConfig) error {
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// VerifyInstalled checks if the MCP server is properly installed
func (h *MCPHandler) VerifyInstalled(targetBase string) (bool, string) {
	return mcpOps.VerifyInstalled(targetBase, h.metadata.Artifact.Name, h.metadata.Artifact.Version)
}
