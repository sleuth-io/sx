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

	hasContent, err := utils.HasContentFiles(zipData)
	if err != nil {
		return fmt.Errorf("failed to inspect zip contents: %w", err)
	}

	var entry map[string]any
	if hasContent {
		// Packaged mode: extract MCP server files to .vscode/mcp-servers/{name}/
		serverDir := filepath.Join(targetBase, DirMCPServers, h.metadata.Asset.Name)
		if err := utils.ExtractZip(zipData, serverDir); err != nil {
			return fmt.Errorf("failed to extract MCP server: %w", err)
		}
		entry = h.generateMCPEntry(serverDir)
	} else {
		// Config-only mode: no extraction, register commands as-is
		entry = h.generateConfigOnlyMCPEntry()
	}

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
	serverDir := filepath.Join(targetBase, DirMCPServers, h.metadata.Asset.Name)
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

func (h *MCPHandler) generateConfigOnlyMCPEntry() map[string]any {
	mcpConfig := h.metadata.MCP

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

// AddMCPServer adds an MCP server entry to .vscode/mcp.json
// This is used by bootstrap to add servers like the sx query MCP.
func AddMCPServer(vscodeDir, name string, serverConfig map[string]any) error {
	mcpConfigPath := filepath.Join(vscodeDir, "mcp.json")

	// Read existing mcp.json
	config, err := readMCPConfig(mcpConfigPath)
	if err != nil {
		return fmt.Errorf("failed to read mcp.json: %w", err)
	}

	// Only add if missing (don't overwrite existing entry)
	if config.Servers == nil {
		config.Servers = make(map[string]any)
	}
	if _, exists := config.Servers[name]; exists {
		// Already configured, don't overwrite
		return nil
	}

	config.Servers[name] = serverConfig

	// Write updated mcp.json
	if err := writeMCPConfig(mcpConfigPath, config); err != nil {
		return fmt.Errorf("failed to write mcp.json: %w", err)
	}

	return nil
}

// RemoveMCPServer removes an MCP server entry from .vscode/mcp.json
func RemoveMCPServer(vscodeDir, name string) error {
	mcpConfigPath := filepath.Join(vscodeDir, "mcp.json")

	// Read existing mcp.json
	config, err := readMCPConfig(mcpConfigPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // Already removed
		}
		return fmt.Errorf("failed to read mcp.json: %w", err)
	}

	// Remove entry
	delete(config.Servers, name)

	// Write updated mcp.json
	if err := writeMCPConfig(mcpConfigPath, config); err != nil {
		return fmt.Errorf("failed to write mcp.json: %w", err)
	}

	return nil
}

// copilotCLIMCPConfig represents Copilot CLI's ~/.copilot/mcp-config.json structure
type copilotCLIMCPConfig struct {
	MCPServers map[string]any `json:"mcpServers"`
}

// AddCopilotCLIMCPServer adds an MCP server entry to ~/.copilot/mcp-config.json
// This is the Copilot CLI-specific MCP config location.
func AddCopilotCLIMCPServer(copilotDir, name string, serverConfig map[string]any) error {
	mcpConfigPath := filepath.Join(copilotDir, "mcp-config.json")

	// Read existing mcp-config.json
	config, err := readCopilotCLIMCPConfig(mcpConfigPath)
	if err != nil {
		return fmt.Errorf("failed to read mcp-config.json: %w", err)
	}

	// Only add if missing (don't overwrite existing entry)
	if config.MCPServers == nil {
		config.MCPServers = make(map[string]any)
	}
	if _, exists := config.MCPServers[name]; exists {
		// Already configured, don't overwrite
		return nil
	}

	config.MCPServers[name] = serverConfig

	// Write updated mcp-config.json
	if err := writeCopilotCLIMCPConfig(mcpConfigPath, config); err != nil {
		return fmt.Errorf("failed to write mcp-config.json: %w", err)
	}

	return nil
}

// RemoveCopilotCLIMCPServer removes an MCP server entry from ~/.copilot/mcp-config.json
func RemoveCopilotCLIMCPServer(copilotDir, name string) error {
	mcpConfigPath := filepath.Join(copilotDir, "mcp-config.json")

	// Read existing mcp-config.json
	config, err := readCopilotCLIMCPConfig(mcpConfigPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // Already removed
		}
		return fmt.Errorf("failed to read mcp-config.json: %w", err)
	}

	// Remove entry
	delete(config.MCPServers, name)

	// Write updated mcp-config.json
	if err := writeCopilotCLIMCPConfig(mcpConfigPath, config); err != nil {
		return fmt.Errorf("failed to write mcp-config.json: %w", err)
	}

	return nil
}

// readCopilotCLIMCPConfig reads Copilot CLI's mcp-config.json file
func readCopilotCLIMCPConfig(path string) (*copilotCLIMCPConfig, error) {
	config := &copilotCLIMCPConfig{
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

// writeCopilotCLIMCPConfig writes Copilot CLI's mcp-config.json file
func writeCopilotCLIMCPConfig(path string, config *copilotCLIMCPConfig) error {
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
