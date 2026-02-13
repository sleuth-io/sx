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

var mcpOps = dirasset.NewOperations(DirMCPServers, &asset.TypeMCP)

// MCPHandler handles MCP asset installation for Cursor (both packaged and config-only)
type MCPHandler struct {
	metadata *metadata.Metadata
}

// NewMCPHandler creates a new MCP handler
func NewMCPHandler(meta *metadata.Metadata) *MCPHandler {
	return &MCPHandler{metadata: meta}
}

// Install installs an MCP asset to Cursor by updating mcp.json.
// For packaged assets, extracts files first. For config-only, registers as-is.
func (h *MCPHandler) Install(ctx context.Context, zipData []byte, targetBase string) error {
	mcpConfigPath := filepath.Join(targetBase, "mcp.json")

	// Read existing mcp.json
	config, err := ReadMCPConfig(mcpConfigPath)
	if err != nil {
		return fmt.Errorf("failed to read mcp.json: %w", err)
	}

	hasContent, err := utils.HasContentFiles(zipData)
	if err != nil {
		return fmt.Errorf("failed to inspect zip contents: %w", err)
	}

	var entry map[string]any
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

// Remove removes an MCP entry from Cursor
func (h *MCPHandler) Remove(ctx context.Context, targetBase string) error {
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

	// Remove server directory if it exists (packaged mode)
	serverDir := filepath.Join(targetBase, DirMCPServers, h.metadata.Asset.Name)
	os.RemoveAll(serverDir) // Ignore errors if doesn't exist

	return nil
}

func (h *MCPHandler) generatePackagedMCPEntry(serverDir string) map[string]any {
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

func (h *MCPHandler) generateConfigOnlyMCPEntry() map[string]any {
	mcpConfig := h.metadata.MCP

	if mcpConfig.IsRemote() {
		entry := map[string]any{
			"url": mcpConfig.URL,
		}
		if len(mcpConfig.Env) > 0 {
			entry["env"] = mcpConfig.Env
		}
		return entry
	}

	// For config-only MCPs, commands are external (npx, docker, etc.)
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

// MCPConfig represents Cursor's mcp.json structure
type MCPConfig struct {
	MCPServers map[string]any `json:"mcpServers"`
}

// ReadMCPConfig reads Cursor's mcp.json file
func ReadMCPConfig(path string) (*MCPConfig, error) {
	config := &MCPConfig{
		MCPServers: make(map[string]any),
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

// VerifyInstalled checks if the MCP server is properly installed.
// For packaged assets, checks the install directory. For config-only, checks mcp.json.
func (h *MCPHandler) VerifyInstalled(targetBase string) (bool, string) {
	// Check if install directory exists (packaged mode)
	installDir := filepath.Join(targetBase, DirMCPServers, h.metadata.Asset.Name)
	if utils.IsDirectory(installDir) {
		return mcpOps.VerifyInstalled(targetBase, h.metadata.Asset.Name, h.metadata.Asset.Version)
	}

	// Config-only mode: check mcp.json for server entry
	mcpConfigPath := filepath.Join(targetBase, "mcp.json")
	config, err := ReadMCPConfig(mcpConfigPath)
	if err != nil {
		return false, "failed to read mcp.json: " + err.Error()
	}

	if _, exists := config.MCPServers[h.metadata.Asset.Name]; !exists {
		return false, "MCP server not registered"
	}

	return true, "installed"
}
