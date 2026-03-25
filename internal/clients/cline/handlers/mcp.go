package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/handlers/dirasset"
	"github.com/sleuth-io/sx/internal/metadata"
	"github.com/sleuth-io/sx/internal/utils"
)

var mcpOps = dirasset.NewOperations(DirMCPServers, &asset.TypeMCP)

// MCPHandler handles MCP asset installation for Cline
type MCPHandler struct {
	metadata *metadata.Metadata
}

// NewMCPHandler creates a new MCP handler
func NewMCPHandler(meta *metadata.Metadata) *MCPHandler {
	return &MCPHandler{metadata: meta}
}

// Install installs an MCP asset to Cline by updating cline_mcp_settings.json
func (h *MCPHandler) Install(ctx context.Context, zipData []byte, targetBase string) error {
	mcpConfigPath, err := GetMCPConfigPath()
	if err != nil {
		return fmt.Errorf("failed to get MCP config path: %w", err)
	}

	// Ensure parent directory exists
	if err := utils.EnsureDir(filepath.Dir(mcpConfigPath)); err != nil {
		return fmt.Errorf("failed to create MCP config directory: %w", err)
	}

	// Read existing config
	config, err := ReadMCPConfig(mcpConfigPath)
	if err != nil {
		return fmt.Errorf("failed to read MCP config: %w", err)
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

	// Write updated config
	if err := WriteMCPConfig(mcpConfigPath, config); err != nil {
		return fmt.Errorf("failed to write MCP config: %w", err)
	}

	return nil
}

// Remove removes an MCP entry from Cline
func (h *MCPHandler) Remove(ctx context.Context, targetBase string) error {
	mcpConfigPath, err := GetMCPConfigPath()
	if err != nil {
		return fmt.Errorf("failed to get MCP config path: %w", err)
	}

	// Read existing config
	config, err := ReadMCPConfig(mcpConfigPath)
	if err != nil {
		return fmt.Errorf("failed to read MCP config: %w", err)
	}

	// Remove entry
	delete(config.MCPServers, h.metadata.Asset.Name)

	// Write updated config
	if err := WriteMCPConfig(mcpConfigPath, config); err != nil {
		return fmt.Errorf("failed to write MCP config: %w", err)
	}

	// Remove server directory if it exists (packaged mode)
	serverDir := filepath.Join(targetBase, DirMCPServers, h.metadata.Asset.Name)
	os.RemoveAll(serverDir) // Ignore errors if doesn't exist

	return nil
}

// VerifyInstalled checks if the MCP server is properly installed
func (h *MCPHandler) VerifyInstalled(targetBase string) (bool, string) {
	// Check if install directory exists (packaged mode)
	installDir := filepath.Join(targetBase, DirMCPServers, h.metadata.Asset.Name)
	if utils.IsDirectory(installDir) {
		return mcpOps.VerifyInstalled(targetBase, h.metadata.Asset.Name, h.metadata.Asset.Version)
	}

	// Config-only mode: check MCP config for server entry
	mcpConfigPath, err := GetMCPConfigPath()
	if err != nil {
		return false, "failed to get MCP config path: " + err.Error()
	}

	config, err := ReadMCPConfig(mcpConfigPath)
	if err != nil {
		return false, "failed to read MCP config: " + err.Error()
	}

	if _, exists := config.MCPServers[h.metadata.Asset.Name]; !exists {
		return false, "MCP server not registered"
	}

	return true, "installed"
}

func (h *MCPHandler) generatePackagedMCPEntry(serverDir string) map[string]any {
	mcpConfig := h.metadata.MCP

	command, args := utils.ResolveCommandAndArgs(mcpConfig.Command, mcpConfig.Args, serverDir)

	entry := map[string]any{
		"command": command,
		"args":    args,
	}

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

	entry := map[string]any{
		"command": mcpConfig.Command,
		"args":    utils.StringsToAny(mcpConfig.Args),
	}

	if len(mcpConfig.Env) > 0 {
		entry["env"] = mcpConfig.Env
	}

	return entry
}

// MCPConfig represents Cline's cline_mcp_settings.json structure
type MCPConfig struct {
	MCPServers map[string]any `json:"mcpServers"`
}

// GetMCPConfigPath returns the path to cline_mcp_settings.json
// Checks CLI location first (~/.cline/data/settings/), then VS Code extension location
func GetMCPConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	// Check CLI location first: ~/.cline/data/settings/cline_mcp_settings.json
	// Also check CLINE_DIR environment variable
	clineDir := os.Getenv("CLINE_DIR")
	if clineDir == "" {
		clineDir = filepath.Join(home, ".cline")
	}
	cliConfigPath := filepath.Join(clineDir, "data", "settings", "cline_mcp_settings.json")

	// If CLI config exists or CLI directory exists, use CLI path
	if _, err := os.Stat(cliConfigPath); err == nil {
		return cliConfigPath, nil
	}
	// Check if CLI data directory exists (config file may not exist yet)
	cliDataDir := filepath.Join(clineDir, "data")
	if _, err := os.Stat(cliDataDir); err == nil {
		return cliConfigPath, nil
	}

	// Fall back to VS Code extension location
	return getVSCodeMCPConfigPath(home)
}

// getVSCodeMCPConfigPath returns the VS Code extension MCP config path
func getVSCodeMCPConfigPath(home string) (string, error) {
	var vsCodeConfigBase string
	switch runtime.GOOS {
	case "darwin":
		vsCodeConfigBase = filepath.Join(home, "Library", "Application Support", "Code", "User", "globalStorage")
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			appData = filepath.Join(home, "AppData", "Roaming")
		}
		vsCodeConfigBase = filepath.Join(appData, "Code", "User", "globalStorage")
	default: // Linux and others
		vsCodeConfigBase = filepath.Join(home, ".config", "Code", "User", "globalStorage")
	}

	return filepath.Join(vsCodeConfigBase, VSCodeExtensionID, "settings", "cline_mcp_settings.json"), nil
}

// GetMCPConfigDir returns the OS-specific directory containing cline_mcp_settings.json
func GetMCPConfigDir() (string, error) {
	path, err := GetMCPConfigPath()
	if err != nil {
		return "", err
	}
	return filepath.Dir(path), nil
}

// ReadMCPConfig reads Cline's cline_mcp_settings.json file
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

	if err := utils.UnmarshalJSONC(data, config); err != nil {
		return nil, err
	}

	if config.MCPServers == nil {
		config.MCPServers = make(map[string]any)
	}

	return config, nil
}

// WriteMCPConfig writes Cline's cline_mcp_settings.json file
func WriteMCPConfig(path string, config *MCPConfig) error {
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

// AddMCPServer adds or updates an MCP server entry in Cline's config
func AddMCPServer(serverName string, serverConfig map[string]any) error {
	mcpConfigPath, err := GetMCPConfigPath()
	if err != nil {
		return fmt.Errorf("failed to get MCP config path: %w", err)
	}

	// Ensure parent directory exists
	if err := utils.EnsureDir(filepath.Dir(mcpConfigPath)); err != nil {
		return fmt.Errorf("failed to create MCP config directory: %w", err)
	}

	config, err := ReadMCPConfig(mcpConfigPath)
	if err != nil {
		return fmt.Errorf("failed to read MCP config: %w", err)
	}

	if config.MCPServers == nil {
		config.MCPServers = make(map[string]any)
	}
	config.MCPServers[serverName] = serverConfig

	if err := WriteMCPConfig(mcpConfigPath, config); err != nil {
		return fmt.Errorf("failed to write MCP config: %w", err)
	}

	return nil
}

// RemoveMCPServer removes an MCP server entry from Cline's config
func RemoveMCPServer(serverName string) error {
	mcpConfigPath, err := GetMCPConfigPath()
	if err != nil {
		return fmt.Errorf("failed to get MCP config path: %w", err)
	}

	config, err := ReadMCPConfig(mcpConfigPath)
	if err != nil {
		return fmt.Errorf("failed to read MCP config: %w", err)
	}

	delete(config.MCPServers, serverName)

	if err := WriteMCPConfig(mcpConfigPath, config); err != nil {
		return fmt.Errorf("failed to write MCP config: %w", err)
	}

	return nil
}
