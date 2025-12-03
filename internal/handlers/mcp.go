package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sleuth-io/skills/internal/metadata"
	"github.com/sleuth-io/skills/internal/utils"
)

// MCPHandler handles MCP server artifact installation
type MCPHandler struct {
	metadata *metadata.Metadata
}

// NewMCPHandler creates a new MCP handler
func NewMCPHandler(meta *metadata.Metadata) *MCPHandler {
	return &MCPHandler{
		metadata: meta,
	}
}

// DetectType returns true if files indicate this is an MCP artifact
func (h *MCPHandler) DetectType(files []string) bool {
	for _, file := range files {
		if file == "package.json" {
			return true
		}
	}
	return false
}

// GetType returns the artifact type string
func (h *MCPHandler) GetType() string {
	return "mcp"
}

// CreateDefaultMetadata creates default metadata for an MCP
func (h *MCPHandler) CreateDefaultMetadata(name, version string) *metadata.Metadata {
	return &metadata.Metadata{
		MetadataVersion: "1.0",
		Artifact: metadata.Artifact{
			Name:    name,
			Version: version,
			Type:    "mcp",
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

// Install extracts and installs the MCP server artifact
func (h *MCPHandler) Install(ctx context.Context, zipData []byte, targetBase string) error {
	// Validate zip structure
	if err := h.Validate(zipData); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	// Determine installation path
	installPath := filepath.Join(targetBase, h.GetInstallPath())

	// Remove existing installation if present
	if utils.IsDirectory(installPath) {
		if err := os.RemoveAll(installPath); err != nil {
			return fmt.Errorf("failed to remove existing installation: %w", err)
		}
	}

	// Create installation directory
	if err := utils.EnsureDir(installPath); err != nil {
		return fmt.Errorf("failed to create installation directory: %w", err)
	}

	// Extract zip to installation directory
	if err := utils.ExtractZip(zipData, installPath); err != nil {
		return fmt.Errorf("failed to extract zip: %w", err)
	}

	// Update .mcp.json to register the MCP server
	if err := h.updateMCPConfig(targetBase, installPath); err != nil {
		return fmt.Errorf("failed to update MCP config: %w", err)
	}

	return nil
}

// Remove uninstalls the MCP server artifact
func (h *MCPHandler) Remove(ctx context.Context, targetBase string) error {
	installPath := filepath.Join(targetBase, h.GetInstallPath())

	// Remove from .mcp.json first
	if err := h.removeFromMCPConfig(targetBase); err != nil {
		return fmt.Errorf("failed to remove from MCP config: %w", err)
	}

	// Remove installation directory
	if utils.IsDirectory(installPath) {
		if err := os.RemoveAll(installPath); err != nil {
			return fmt.Errorf("failed to remove MCP server: %w", err)
		}
	}

	return nil
}

// GetInstallPath returns the installation path relative to targetBase
func (h *MCPHandler) GetInstallPath() string {
	return filepath.Join("mcp-servers", h.metadata.Artifact.Name)
}

// Validate checks if the zip structure is valid for an MCP artifact
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

	// Verify artifact type matches
	if meta.Artifact.Type != "mcp" {
		return fmt.Errorf("artifact type mismatch: expected mcp, got %s", meta.Artifact.Type)
	}

	// Check that MCP config exists
	if meta.MCP == nil {
		return fmt.Errorf("[mcp] section missing in metadata")
	}

	return nil
}

// updateMCPConfig updates .mcp.json to register the MCP server
func (h *MCPHandler) updateMCPConfig(targetBase, installPath string) error {
	mcpConfigPath := filepath.Join(targetBase, ".mcp.json")

	// Read existing config or create new
	var config map[string]interface{}
	if utils.FileExists(mcpConfigPath) {
		data, err := os.ReadFile(mcpConfigPath)
		if err != nil {
			return fmt.Errorf("failed to read .mcp.json: %w", err)
		}
		if err := json.Unmarshal(data, &config); err != nil {
			return fmt.Errorf("failed to parse .mcp.json: %w", err)
		}
	} else {
		config = map[string]interface{}{
			"mcpServers": make(map[string]interface{}),
		}
	}

	// Ensure mcpServers section exists
	if config["mcpServers"] == nil {
		config["mcpServers"] = make(map[string]interface{})
	}
	mcpServers := config["mcpServers"].(map[string]interface{})

	// Build MCP server configuration
	serverConfig := h.buildMCPServerConfig(installPath)

	// Add/update MCP server entry
	mcpServers[h.metadata.Artifact.Name] = serverConfig

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

// removeFromMCPConfig removes the MCP server from .mcp.json
func (h *MCPHandler) removeFromMCPConfig(targetBase string) error {
	mcpConfigPath := filepath.Join(targetBase, ".mcp.json")

	if !utils.FileExists(mcpConfigPath) {
		return nil // Nothing to remove
	}

	// Read config
	data, err := os.ReadFile(mcpConfigPath)
	if err != nil {
		return fmt.Errorf("failed to read .mcp.json: %w", err)
	}

	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse .mcp.json: %w", err)
	}

	// Check if mcpServers section exists
	if config["mcpServers"] == nil {
		return nil
	}
	mcpServers := config["mcpServers"].(map[string]interface{})

	// Remove this MCP server
	delete(mcpServers, h.metadata.Artifact.Name)

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
		"command":   command,
		"args":      args,
		"_artifact": h.metadata.Artifact.Name,
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
