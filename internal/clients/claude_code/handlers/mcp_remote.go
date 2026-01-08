package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/metadata"
	"github.com/sleuth-io/sx/internal/utils"
)

// MCPRemoteHandler handles MCP remote asset installation
// MCP remote assets contain only configuration, no server code
type MCPRemoteHandler struct {
	metadata *metadata.Metadata
}

// NewMCPRemoteHandler creates a new MCP remote handler
func NewMCPRemoteHandler(meta *metadata.Metadata) *MCPRemoteHandler {
	return &MCPRemoteHandler{
		metadata: meta,
	}
}

// Install installs the MCP remote configuration (no extraction needed)
func (h *MCPRemoteHandler) Install(ctx context.Context, zipData []byte, targetBase string) error {
	// Validate zip structure
	if err := h.Validate(zipData); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	// For MCP remote, we only need to update .mcp.json
	// No files need to be extracted
	if err := h.updateMCPConfig(targetBase); err != nil {
		return fmt.Errorf("failed to update MCP config: %w", err)
	}

	return nil
}

// Remove uninstalls the MCP remote configuration
func (h *MCPRemoteHandler) Remove(ctx context.Context, targetBase string) error {
	// Remove from .mcp.json
	if err := h.removeFromMCPConfig(targetBase); err != nil {
		return fmt.Errorf("failed to remove from MCP config: %w", err)
	}

	return nil
}

// GetInstallPath returns the installation path relative to targetBase
// For MCP remote, there is no installation directory
func (h *MCPRemoteHandler) GetInstallPath() string {
	return "" // No files installed
}

// Validate checks if the zip structure is valid for an MCP remote asset
func (h *MCPRemoteHandler) Validate(zipData []byte) error {
	// List files in zip
	files, err := utils.ListZipFiles(zipData)
	if err != nil {
		return fmt.Errorf("failed to list zip files: %w", err)
	}

	// Check that metadata.toml exists
	if !containsFile(files, "metadata.toml") {
		return errors.New("metadata.toml not found in zip")
	}

	// Extract and validate metadata
	metadataBytes, err := utils.ReadZipFile(zipData, "metadata.toml")
	if err != nil {
		return fmt.Errorf("failed to read metadata.toml: %w", err)
	}

	meta, err := metadata.Parse(metadataBytes)
	if err != nil {
		return fmt.Errorf("failed to parse metadata: %w", err)
	}

	// Validate metadata
	if err := meta.Validate(); err != nil {
		return fmt.Errorf("metadata validation failed: %w", err)
	}

	// Verify asset type matches
	if meta.Asset.Type != asset.TypeMCPRemote {
		return fmt.Errorf("asset type mismatch: expected mcp-remote, got %s", meta.Asset.Type)
	}

	// Check that MCP config exists
	if meta.MCP == nil {
		return errors.New("[mcp] section missing in metadata")
	}

	return nil
}

// updateMCPConfig updates .mcp.json to register the MCP remote server
func (h *MCPRemoteHandler) updateMCPConfig(targetBase string) error {
	mcpConfigPath := filepath.Join(targetBase, ".mcp.json")

	// Read existing config or create new
	var config map[string]any
	if utils.FileExists(mcpConfigPath) {
		data, err := os.ReadFile(mcpConfigPath)
		if err != nil {
			return fmt.Errorf("failed to read .mcp.json: %w", err)
		}
		if err := json.Unmarshal(data, &config); err != nil {
			return fmt.Errorf("failed to parse .mcp.json: %w", err)
		}
	} else {
		config = map[string]any{
			"mcpServers": make(map[string]any),
		}
	}

	// Ensure mcpServers section exists
	if config["mcpServers"] == nil {
		config["mcpServers"] = make(map[string]any)
	}
	mcpServers := config["mcpServers"].(map[string]any)

	// Build MCP server configuration
	serverConfig := h.buildMCPServerConfig()

	// Add/update MCP server entry
	mcpServers[h.metadata.Asset.Name] = serverConfig

	// Write updated config
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal MCP config: %w", err)
	}

	if err := os.WriteFile(mcpConfigPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write .mcp.json: %w", err)
	}

	return nil
}

// removeFromMCPConfig removes the MCP remote server from .mcp.json
func (h *MCPRemoteHandler) removeFromMCPConfig(targetBase string) error {
	mcpConfigPath := filepath.Join(targetBase, ".mcp.json")

	if !utils.FileExists(mcpConfigPath) {
		return nil // Nothing to remove
	}

	// Read config
	data, err := os.ReadFile(mcpConfigPath)
	if err != nil {
		return fmt.Errorf("failed to read .mcp.json: %w", err)
	}

	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse .mcp.json: %w", err)
	}

	// Check if mcpServers section exists
	if config["mcpServers"] == nil {
		return nil
	}
	mcpServers := config["mcpServers"].(map[string]any)

	// Remove this MCP server
	delete(mcpServers, h.metadata.Asset.Name)

	// Write updated config
	data, err = json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal MCP config: %w", err)
	}

	if err := os.WriteFile(mcpConfigPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write .mcp.json: %w", err)
	}

	return nil
}

// buildMCPServerConfig builds the MCP server configuration for .mcp.json
func (h *MCPRemoteHandler) buildMCPServerConfig() map[string]any {
	mcpConfig := h.metadata.MCP

	// For remote MCPs, commands are external (npx, docker, etc.)
	// No path conversion needed
	args := make([]any, len(mcpConfig.Args))
	for i, arg := range mcpConfig.Args {
		args[i] = arg
	}

	config := map[string]any{
		"command":   mcpConfig.Command,
		"args":      args,
		"_artifact": h.metadata.Asset.Name,
	}

	// Add optional fields
	if len(mcpConfig.Env) > 0 {
		config["env"] = mcpConfig.Env
	}
	if mcpConfig.Timeout > 0 {
		config["timeout"] = mcpConfig.Timeout
	}

	return config
}

// CanDetectInstalledState returns false since mcp-remote doesn't preserve metadata.toml
func (h *MCPRemoteHandler) CanDetectInstalledState() bool {
	return false
}

// VerifyInstalled checks if the MCP remote server is registered in .mcp.json
func (h *MCPRemoteHandler) VerifyInstalled(targetBase string) (bool, string) {
	mcpConfigPath := filepath.Join(targetBase, ".mcp.json")

	if !utils.FileExists(mcpConfigPath) {
		return false, ".mcp.json not found"
	}

	data, err := os.ReadFile(mcpConfigPath)
	if err != nil {
		return false, "failed to read .mcp.json: " + err.Error()
	}

	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		return false, "failed to parse .mcp.json: " + err.Error()
	}

	mcpServers, ok := config["mcpServers"].(map[string]any)
	if !ok {
		return false, "mcpServers section not found"
	}

	if _, exists := mcpServers[h.metadata.Asset.Name]; !exists {
		return false, "MCP remote server not registered"
	}

	return true, "installed"
}

// DetectUsageFromToolCall detects MCP remote server usage from tool calls
// MCP remote uses the same tool naming pattern as regular MCP, so we use the same logic
func (h *MCPRemoteHandler) DetectUsageFromToolCall(toolName string, toolInput map[string]any) (string, bool) {
	// MCP tools follow pattern: mcp__server__tool
	if !strings.HasPrefix(toolName, "mcp__") {
		return "", false
	}
	// Parse: "mcp__github__list_prs" -> "github"
	parts := strings.Split(toolName, "__")
	if len(parts) < 2 {
		return "", false
	}
	serverName := parts[1]
	return serverName, true
}
