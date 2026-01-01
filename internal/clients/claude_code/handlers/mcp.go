package handlers

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/handlers/dirasset"
	"github.com/sleuth-io/sx/internal/metadata"
	"github.com/sleuth-io/sx/internal/utils"
)

var mcpOps = dirasset.NewOperations("mcp-servers", &asset.TypeMCP)

// MCPHandler handles MCP server asset installation
type MCPHandler struct {
	metadata *metadata.Metadata
}

// NewMCPHandler creates a new MCP handler
func NewMCPHandler(meta *metadata.Metadata) *MCPHandler {
	return &MCPHandler{
		metadata: meta,
	}
}

// DetectType returns true if files indicate this is an MCP asset
func (h *MCPHandler) DetectType(files []string) bool {
	for _, file := range files {
		if file == "package.json" {
			return true
		}
	}
	return false
}

// GetType returns the asset type string
func (h *MCPHandler) GetType() string {
	return "mcp"
}

// CreateDefaultMetadata creates default metadata for an MCP
func (h *MCPHandler) CreateDefaultMetadata(name, version string) *metadata.Metadata {
	return &metadata.Metadata{
		MetadataVersion: "1.0",
		Asset: metadata.Asset{
			Name:    name,
			Version: version,
			Type:    asset.TypeMCP,
		},
		MCP: &metadata.MCPConfig{},
	}
}

// GetPromptFile returns empty for MCP servers (not applicable)
func (h *MCPHandler) GetPromptFile(meta *metadata.Metadata) string {
	return ""
}

// GetScriptFile returns empty for MCP servers (not applicable)
func (h *MCPHandler) GetScriptFile(meta *metadata.Metadata) string {
	return ""
}

// ValidateMetadata validates MCP-specific metadata
func (h *MCPHandler) ValidateMetadata(meta *metadata.Metadata) error {
	if meta.MCP == nil {
		return fmt.Errorf("mcp configuration missing")
	}
	return nil
}

// DetectUsageFromToolCall detects MCP server usage from tool calls
func (h *MCPHandler) DetectUsageFromToolCall(toolName string, toolInput map[string]interface{}) (string, bool) {
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

// Install extracts and installs the MCP server asset
func (h *MCPHandler) Install(ctx context.Context, zipData []byte, targetBase string) error {
	// Validate zip structure
	if err := h.Validate(zipData); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	// Extract to mcp-servers directory
	if err := mcpOps.Install(ctx, zipData, targetBase, h.metadata.Asset.Name); err != nil {
		return err
	}

	// Update settings.json to register the MCP server
	installPath := filepath.Join(targetBase, h.GetInstallPath())
	if err := h.updateMCPConfig(targetBase, installPath); err != nil {
		return fmt.Errorf("failed to update MCP config: %w", err)
	}

	return nil
}

// Remove uninstalls the MCP server asset
func (h *MCPHandler) Remove(ctx context.Context, targetBase string) error {
	// Remove from settings.json first
	if err := h.removeFromMCPConfig(targetBase); err != nil {
		return fmt.Errorf("failed to remove from MCP config: %w", err)
	}

	// Remove installation directory
	return mcpOps.Remove(ctx, targetBase, h.metadata.Asset.Name)
}

// GetInstallPath returns the installation path relative to targetBase
func (h *MCPHandler) GetInstallPath() string {
	return filepath.Join("mcp-servers", h.metadata.Asset.Name)
}

// Validate checks if the zip structure is valid for an MCP asset
func (h *MCPHandler) Validate(zipData []byte) error {
	// List files in zip
	files, err := utils.ListZipFiles(zipData)
	if err != nil {
		return fmt.Errorf("failed to list zip files: %w", err)
	}

	// Check that metadata.toml exists
	if !containsFile(files, "metadata.toml") {
		return fmt.Errorf("metadata.toml not found in zip")
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

	// Validate metadata with file list
	if err := meta.ValidateWithFiles(files); err != nil {
		return fmt.Errorf("metadata validation failed: %w", err)
	}

	// Verify asset type matches
	if meta.Asset.Type != asset.TypeMCP {
		return fmt.Errorf("asset type mismatch: expected mcp, got %s", meta.Asset.Type)
	}

	// Check that MCP config exists
	if meta.MCP == nil {
		return fmt.Errorf("[mcp] section missing in metadata")
	}

	return nil
}

// updateMCPConfig updates settings.json to register the MCP server
func (h *MCPHandler) updateMCPConfig(targetBase, installPath string) error {
	// Build MCP server configuration
	serverConfig := h.buildMCPServerConfig(installPath)

	// Use shared utility to add/update the server
	return AddMCPServer(targetBase, h.metadata.Asset.Name, serverConfig)
}

// removeFromMCPConfig removes the MCP server from settings.json
func (h *MCPHandler) removeFromMCPConfig(targetBase string) error {
	// Use shared utility to remove the server
	return RemoveMCPServer(targetBase, h.metadata.Asset.Name)
}

// buildMCPServerConfig builds the MCP server configuration for settings.json
func (h *MCPHandler) buildMCPServerConfig(installPath string) map[string]interface{} {
	mcpConfig := h.metadata.MCP

	// Convert relative command paths to absolute (relative to install path)
	command := mcpConfig.Command
	if !filepath.IsAbs(command) {
		command = filepath.Join(installPath, command)
	}

	// Convert relative args paths to absolute
	args := make([]interface{}, len(mcpConfig.Args))
	for i, arg := range mcpConfig.Args {
		// If arg looks like a relative path (contains / or \), make it absolute
		if !filepath.IsAbs(arg) && (filepath.Base(arg) != arg) {
			args[i] = filepath.Join(installPath, arg)
		} else {
			args[i] = arg
		}
	}

	config := map[string]interface{}{
		"type":      "stdio",
		"command":   command,
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

// CanDetectInstalledState returns true since MCP servers preserve metadata.toml
func (h *MCPHandler) CanDetectInstalledState() bool {
	return true
}

// VerifyInstalled checks if the MCP server is properly installed
func (h *MCPHandler) VerifyInstalled(targetBase string) (bool, string) {
	return mcpOps.VerifyInstalled(targetBase, h.metadata.Asset.Name, h.metadata.Asset.Version)
}
