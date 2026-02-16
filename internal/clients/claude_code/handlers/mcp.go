package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/handlers/dirasset"
	"github.com/sleuth-io/sx/internal/metadata"
	"github.com/sleuth-io/sx/internal/utils"
)

var mcpOps = dirasset.NewOperations(DirMCPServers, &asset.TypeMCP)

// MCPHandler handles MCP server asset installation (both packaged and config-only)
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
	return slices.Contains(files, "package.json")
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
		return errors.New("mcp configuration missing")
	}
	return nil
}

// DetectUsageFromToolCall detects MCP server usage from tool calls
func (h *MCPHandler) DetectUsageFromToolCall(toolName string, toolInput map[string]any) (string, bool) {
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

// Install installs the MCP asset. For packaged assets (zip has content files),
// extracts to mcp-servers/ and registers with absolute paths. For config-only
// assets (zip has only metadata.toml), registers commands as-is.
func (h *MCPHandler) Install(ctx context.Context, zipData []byte, targetBase string) error {
	// Validate zip structure
	if err := h.Validate(zipData); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	hasContent, err := utils.HasContentFiles(zipData)
	if err != nil {
		return fmt.Errorf("failed to inspect zip contents: %w", err)
	}

	if hasContent {
		// Packaged mode: extract files and register with absolute paths
		if err := mcpOps.Install(ctx, zipData, targetBase, h.metadata.Asset.Name); err != nil {
			return err
		}
		installPath := filepath.Join(targetBase, h.GetInstallPath())
		serverConfig := h.buildPackagedMCPServerConfig(installPath)
		if err := AddMCPServer(targetBase, h.metadata.Asset.Name, serverConfig); err != nil {
			return fmt.Errorf("failed to update MCP config: %w", err)
		}
	} else {
		// Config-only mode: no extraction, register commands as-is
		serverConfig := h.buildConfigOnlyMCPServerConfig()
		if err := AddMCPServer(targetBase, h.metadata.Asset.Name, serverConfig); err != nil {
			return fmt.Errorf("failed to update MCP config: %w", err)
		}
	}

	return nil
}

// Remove uninstalls the MCP server asset
func (h *MCPHandler) Remove(ctx context.Context, targetBase string) error {
	// Remove from .claude.json
	if err := RemoveMCPServer(targetBase, h.metadata.Asset.Name); err != nil {
		return fmt.Errorf("failed to remove from MCP config: %w", err)
	}

	// Also remove from legacy .mcp.json if present (transition from old mcp-remote)
	h.removeFromLegacyMCPConfig(targetBase)

	// Remove installation directory if it exists (packaged mode)
	installDir := filepath.Join(targetBase, DirMCPServers, h.metadata.Asset.Name)
	if utils.IsDirectory(installDir) {
		return mcpOps.Remove(ctx, targetBase, h.metadata.Asset.Name)
	}

	return nil
}

// GetInstallPath returns the installation path relative to targetBase.
// For config-only assets, checks if the directory exists on disk.
func (h *MCPHandler) GetInstallPath() string {
	return filepath.Join(DirMCPServers, h.metadata.Asset.Name)
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
		return errors.New("[mcp] section missing in metadata")
	}

	return nil
}

// buildPackagedMCPServerConfig builds config for packaged MCP servers (with extracted files)
func (h *MCPHandler) buildPackagedMCPServerConfig(installPath string) map[string]any {
	mcpConfig := h.metadata.MCP

	// Convert relative command paths to absolute (relative to install path).
	// Bare command names like "node" or "python" are left as-is (resolved via PATH).
	command := mcpConfig.Command
	if !filepath.IsAbs(command) && filepath.Base(command) != command {
		command = filepath.Join(installPath, command)
	}

	// Convert relative args to absolute paths (relative to install path).
	// For packaged MCPs, args are file references within the package.
	// Only skip args that are clearly not paths (flags starting with -).
	args := make([]any, len(mcpConfig.Args))
	for i, arg := range mcpConfig.Args {
		if filepath.IsAbs(arg) || strings.HasPrefix(arg, "-") {
			args[i] = arg
		} else {
			args[i] = filepath.Join(installPath, arg)
		}
	}

	config := map[string]any{
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

// buildConfigOnlyMCPServerConfig builds config for config-only MCP assets (no extraction)
func (h *MCPHandler) buildConfigOnlyMCPServerConfig() map[string]any {
	mcpConfig := h.metadata.MCP

	if mcpConfig.IsRemote() {
		config := map[string]any{
			"type":      mcpConfig.Transport,
			"url":       mcpConfig.URL,
			"_artifact": h.metadata.Asset.Name,
		}
		if len(mcpConfig.Env) > 0 {
			config["env"] = mcpConfig.Env
		}
		if mcpConfig.Timeout > 0 {
			config["timeout"] = mcpConfig.Timeout
		}
		return config
	}

	// For config-only MCPs, commands are external (npx, docker, etc.)
	// No path conversion needed
	args := make([]any, len(mcpConfig.Args))
	for i, arg := range mcpConfig.Args {
		args[i] = arg
	}

	config := map[string]any{
		"type":      "stdio",
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

// removeFromLegacyMCPConfig removes from .mcp.json (legacy mcp-remote location)
func (h *MCPHandler) removeFromLegacyMCPConfig(targetBase string) {
	mcpConfigPath := filepath.Join(targetBase, ".mcp.json")

	if !utils.FileExists(mcpConfigPath) {
		return
	}

	data, err := os.ReadFile(mcpConfigPath)
	if err != nil {
		return
	}

	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		return
	}

	mcpServers, ok := config["mcpServers"].(map[string]any)
	if !ok {
		return
	}

	if _, exists := mcpServers[h.metadata.Asset.Name]; !exists {
		return
	}

	delete(mcpServers, h.metadata.Asset.Name)

	data, err = json.MarshalIndent(config, "", "  ")
	if err != nil {
		return
	}

	os.WriteFile(mcpConfigPath, data, 0644)
}

// CanDetectInstalledState returns true since we can verify installation state
func (h *MCPHandler) CanDetectInstalledState() bool {
	return true
}

// VerifyInstalled checks if the MCP server is properly installed.
// For packaged assets, checks the install directory. For config-only, checks the config file.
func (h *MCPHandler) VerifyInstalled(targetBase string) (bool, string) {
	// Check if install directory exists (packaged mode)
	installDir := filepath.Join(targetBase, DirMCPServers, h.metadata.Asset.Name)
	if utils.IsDirectory(installDir) {
		return mcpOps.VerifyInstalled(targetBase, h.metadata.Asset.Name, h.metadata.Asset.Version)
	}

	// Config-only mode: check the appropriate config file
	return VerifyMCPServerInstalled(targetBase, h.metadata.Asset.Name)
}
