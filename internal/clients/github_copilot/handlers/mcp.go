package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/handlers/dirasset"
	"github.com/sleuth-io/sx/internal/metadata"
	"github.com/sleuth-io/sx/internal/utils"
)

var mcpOps = dirasset.NewOperations("mcp-servers", &asset.TypeMCP)

// MCPHandler handles MCP asset installation for GitHub Copilot (VS Code)
type MCPHandler struct {
	metadata *metadata.Metadata
}

// NewMCPHandler creates a new MCP handler
func NewMCPHandler(meta *metadata.Metadata) *MCPHandler {
	return &MCPHandler{metadata: meta}
}

// Install installs an MCP asset to VS Code by updating .vscode/mcp.json
func (h *MCPHandler) Install(ctx context.Context, zipData []byte, targetBase string) error {
	// For MCP, targetBase should be .vscode/ (not .github/)
	mcpConfigPath := filepath.Join(targetBase, "mcp.json")

	// Read existing mcp.json
	config, err := readMCPConfig(mcpConfigPath)
	if err != nil {
		return fmt.Errorf("failed to read mcp.json: %w", err)
	}

	// Extract MCP server files to .vscode/mcp-servers/{name}/
	serverDir := filepath.Join(targetBase, "mcp-servers", h.metadata.Asset.Name)
	if err := utils.ExtractZip(zipData, serverDir); err != nil {
		return fmt.Errorf("failed to extract MCP server: %w", err)
	}

	// Generate MCP entry from metadata (with paths relative to extraction)
	entry := h.generateMCPEntry(serverDir)

	// Add to config
	if config.Servers == nil {
		config.Servers = make(map[string]any)
	}
	config.Servers[h.metadata.Asset.Name] = entry

	// Write updated mcp.json
	if err := writeMCPConfig(mcpConfigPath, config); err != nil {
		return fmt.Errorf("failed to write mcp.json: %w", err)
	}

	return nil
}

// Remove removes an MCP entry from VS Code
func (h *MCPHandler) Remove(ctx context.Context, targetBase string) error {
	mcpConfigPath := filepath.Join(targetBase, "mcp.json")

	// Read existing mcp.json
	config, err := readMCPConfig(mcpConfigPath)
	if err != nil {
		return fmt.Errorf("failed to read mcp.json: %w", err)
	}

	// Remove entry
	delete(config.Servers, h.metadata.Asset.Name)

	// Write updated mcp.json
	if err := writeMCPConfig(mcpConfigPath, config); err != nil {
		return fmt.Errorf("failed to write mcp.json: %w", err)
	}

	// Remove server directory (if exists)
	serverDir := filepath.Join(targetBase, "mcp-servers", h.metadata.Asset.Name)
	os.RemoveAll(serverDir) // Ignore errors if doesn't exist

	return nil
}

func (h *MCPHandler) generateMCPEntry(serverDir string) map[string]any {
	mcpConfig := h.metadata.MCP

	// Convert relative command paths to absolute (relative to server directory)
	command := mcpConfig.Command
	if !filepath.IsAbs(command) {
		command = filepath.Join(serverDir, command)
	}

	// Convert relative args paths to absolute
	args := make([]any, len(mcpConfig.Args))
	for i, arg := range mcpConfig.Args {
		// If arg looks like a relative path (contains / or \), make it absolute
		if !filepath.IsAbs(arg) && (filepath.Base(arg) != arg) {
			args[i] = filepath.Join(serverDir, arg)
		} else {
			args[i] = arg
		}
	}

	entry := map[string]any{
		"command": command,
		"args":    args,
	}

	// Add env if present
	if len(mcpConfig.Env) > 0 {
		entry["env"] = mcpConfig.Env
	}

	return entry
}

// mcpConfig represents VS Code's .vscode/mcp.json structure
type mcpConfig struct {
	Servers map[string]any `json:"servers"`
}

// readMCPConfig reads VS Code's mcp.json file
func readMCPConfig(path string) (*mcpConfig, error) {
	config := &mcpConfig{
		Servers: make(map[string]any),
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

// writeMCPConfig writes VS Code's mcp.json file
func writeMCPConfig(path string, config *mcpConfig) error {
	// Ensure directory exists
	if err := utils.EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// VerifyInstalled checks if the MCP server is properly installed
func (h *MCPHandler) VerifyInstalled(targetBase string) (bool, string) {
	return mcpOps.VerifyInstalled(targetBase, h.metadata.Asset.Name, h.metadata.Asset.Version)
}
