package handlers

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/sleuth-io/sx/internal/metadata"
)

// MCPRemoteHandler handles MCP remote asset installation for Cursor
// MCP remote assets contain only configuration, no server code
type MCPRemoteHandler struct {
	metadata *metadata.Metadata
}

// NewMCPRemoteHandler creates a new MCP remote handler
func NewMCPRemoteHandler(meta *metadata.Metadata) *MCPRemoteHandler {
	return &MCPRemoteHandler{metadata: meta}
}

// Install installs the MCP remote configuration (no extraction needed)
func (h *MCPRemoteHandler) Install(ctx context.Context, zipData []byte, targetBase string) error {
	mcpConfigPath := filepath.Join(targetBase, "mcp.json")

	// Read existing mcp.json
	config, err := ReadMCPConfig(mcpConfigPath)
	if err != nil {
		return fmt.Errorf("failed to read mcp.json: %w", err)
	}

	// Generate MCP entry from metadata (no path conversion for remote)
	entry := h.generateMCPEntry()

	// Add to config
	if config.MCPServers == nil {
		config.MCPServers = make(map[string]any)
	}
	config.MCPServers[h.metadata.Asset.Name] = entry

	// Write updated mcp.json
	if err := WriteMCPConfig(mcpConfigPath, config); err != nil {
		return fmt.Errorf("failed to write mcp.json: %w", err)
	}

	return nil
}

// Remove uninstalls the MCP remote configuration
func (h *MCPRemoteHandler) Remove(ctx context.Context, targetBase string) error {
	mcpConfigPath := filepath.Join(targetBase, "mcp.json")

	// Read existing mcp.json
	config, err := ReadMCPConfig(mcpConfigPath)
	if err != nil {
		return fmt.Errorf("failed to read mcp.json: %w", err)
	}

	// Remove entry
	delete(config.MCPServers, h.metadata.Asset.Name)

	// Write updated mcp.json
	if err := WriteMCPConfig(mcpConfigPath, config); err != nil {
		return fmt.Errorf("failed to write mcp.json: %w", err)
	}

	return nil
}

func (h *MCPRemoteHandler) generateMCPEntry() map[string]any {
	mcpConfig := h.metadata.MCP

	// For remote MCPs, commands are external (npx, docker, etc.)
	// No path conversion needed
	args := make([]any, len(mcpConfig.Args))
	for i, arg := range mcpConfig.Args {
		args[i] = arg
	}

	entry := map[string]any{
		"command": mcpConfig.Command,
		"args":    args,
	}

	// Add env if present
	if len(mcpConfig.Env) > 0 {
		entry["env"] = mcpConfig.Env
	}

	return entry
}

// VerifyInstalled checks if the MCP remote server is registered in mcp.json
func (h *MCPRemoteHandler) VerifyInstalled(targetBase string) (bool, string) {
	mcpConfigPath := filepath.Join(targetBase, "mcp.json")

	config, err := ReadMCPConfig(mcpConfigPath)
	if err != nil {
		return false, "failed to read mcp.json: " + err.Error()
	}

	if _, exists := config.MCPServers[h.metadata.Asset.Name]; !exists {
		return false, "MCP remote server not registered"
	}

	return true, "installed"
}
