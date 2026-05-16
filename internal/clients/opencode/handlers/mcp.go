package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/bootstrap"
	"github.com/sleuth-io/sx/internal/handlers/dirasset"
	"github.com/sleuth-io/sx/internal/metadata"
	"github.com/sleuth-io/sx/internal/utils"
)

var mcpOps = dirasset.NewOperations(DirMCPServers, &asset.TypeMCP)

// MCPHandler installs MCP server entries into OpenCode's opencode.json.
// Packaged servers extract files into <targetBase>/mcp-servers/<name>/ and
// register an absolute-path command; config-only servers only modify the
// JSON config.
type MCPHandler struct {
	metadata *metadata.Metadata
}

// NewMCPHandler creates a new MCP handler.
func NewMCPHandler(meta *metadata.Metadata) *MCPHandler {
	return &MCPHandler{metadata: meta}
}

// Install registers the MCP server in opencode.json, extracting files first
// for packaged servers.
func (h *MCPHandler) Install(ctx context.Context, zipData []byte, targetBase string) error {
	hasContent, err := utils.HasContentFiles(zipData)
	if err != nil {
		return fmt.Errorf("failed to inspect zip contents: %w", err)
	}

	var entry map[string]any
	if hasContent {
		serverDir := filepath.Join(targetBase, DirMCPServers, h.metadata.Asset.Name)
		if err := utils.ExtractZip(zipData, serverDir); err != nil {
			return fmt.Errorf("failed to extract MCP server: %w", err)
		}
		entry = h.generatePackagedMCPEntry(serverDir)
	} else {
		entry = h.generateConfigOnlyMCPEntry()
	}

	configPath := filepath.Join(targetBase, ConfigFile)
	return AddMCPServer(configPath, h.metadata.Asset.Name, entry)
}

// Remove removes the MCP entry from opencode.json and cleans up any extracted
// packaged-server files.
func (h *MCPHandler) Remove(ctx context.Context, targetBase string) error {
	configPath := filepath.Join(targetBase, ConfigFile)
	if err := RemoveMCPServer(configPath, h.metadata.Asset.Name); err != nil {
		return err
	}

	serverDir := filepath.Join(targetBase, DirMCPServers, h.metadata.Asset.Name)
	os.RemoveAll(serverDir)

	return nil
}

// VerifyInstalled reports whether the MCP server is registered (and, for
// packaged servers, whether the extracted directory is on disk).
func (h *MCPHandler) VerifyInstalled(targetBase string) (bool, string) {
	installDir := filepath.Join(targetBase, DirMCPServers, h.metadata.Asset.Name)
	if utils.IsDirectory(installDir) {
		return mcpOps.VerifyInstalled(targetBase, h.metadata.Asset.Name, h.metadata.Asset.Version)
	}

	configPath := filepath.Join(targetBase, ConfigFile)
	config, err := ReadOpenCodeConfig(configPath)
	if err != nil {
		return false, "failed to read opencode.json: " + err.Error()
	}
	if _, exists := config.MCP[h.metadata.Asset.Name]; !exists {
		return false, "MCP server not registered"
	}
	return true, "installed"
}

func (h *MCPHandler) generatePackagedMCPEntry(serverDir string) map[string]any {
	mcpConfig := h.metadata.MCP

	command, args := utils.ResolveCommandAndArgs(mcpConfig.Command, mcpConfig.Args, serverDir)

	// OpenCode local servers take a single command array combining the
	// executable and its arguments.
	commandArr := append([]any{command}, args...)

	entry := map[string]any{
		"type":    "local",
		"enabled": true,
		"command": commandArr,
	}

	if len(mcpConfig.Env) > 0 {
		entry["environment"] = mcpConfig.Env
	}

	return entry
}

func (h *MCPHandler) generateConfigOnlyMCPEntry() map[string]any {
	mcpConfig := h.metadata.MCP

	if mcpConfig.IsRemote() {
		entry := map[string]any{
			"type":    "remote",
			"enabled": true,
			"url":     mcpConfig.URL,
		}
		if len(mcpConfig.Env) > 0 {
			entry["environment"] = mcpConfig.Env
		}
		return entry
	}

	// Build single command array: [command, ...args]
	commandArr := []any{mcpConfig.Command}
	for _, a := range mcpConfig.Args {
		commandArr = append(commandArr, a)
	}

	entry := map[string]any{
		"type":    "local",
		"enabled": true,
		"command": commandArr,
	}

	if len(mcpConfig.Env) > 0 {
		entry["environment"] = mcpConfig.Env
	}

	return entry
}

// OpenCodeConfig is a minimal view of opencode.json that we care about.
// Unknown top-level fields are preserved in Other so writes don't drop them.
// WriteOpenCodeConfig also injects a `$schema` reference if the config
// doesn't already have one, so newly-materialized files are valid against
// the OpenCode JSON schema out of the box.
type OpenCodeConfig struct {
	MCP   map[string]any
	Other map[string]any
}

// ReadOpenCodeConfig reads opencode.json (or returns an empty config if the
// file doesn't exist yet). Supports JSONC.
func ReadOpenCodeConfig(path string) (*OpenCodeConfig, error) {
	config := &OpenCodeConfig{
		MCP:   make(map[string]any),
		Other: make(map[string]any),
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return config, nil
		}
		return nil, err
	}

	var raw map[string]any
	if err := utils.UnmarshalJSONC(data, &raw); err != nil {
		return nil, err
	}

	if servers, ok := raw["mcp"].(map[string]any); ok {
		config.MCP = servers
	}

	for k, v := range raw {
		if k != "mcp" {
			config.Other[k] = v
		}
	}

	return config, nil
}

// WriteOpenCodeConfig writes opencode.json, preserving unknown fields and
// adding the $schema reference if it isn't already present.
func WriteOpenCodeConfig(path string, config *OpenCodeConfig) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	output := make(map[string]any)
	maps.Copy(output, config.Other)

	if _, hasSchema := output["$schema"]; !hasSchema {
		output["$schema"] = "https://opencode.ai/config.json"
	}

	if len(config.MCP) > 0 {
		output["mcp"] = config.MCP
	}

	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// AddMCPServer adds or updates an entry in opencode.json under the mcp key.
func AddMCPServer(configPath, name string, entry map[string]any) error {
	config, err := ReadOpenCodeConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to read opencode.json: %w", err)
	}

	if config.MCP == nil {
		config.MCP = make(map[string]any)
	}
	config.MCP[name] = entry

	if err := WriteOpenCodeConfig(configPath, config); err != nil {
		return fmt.Errorf("failed to write opencode.json: %w", err)
	}
	return nil
}

// MCPServerEntryFromBootstrap renders a bootstrap.MCPServerConfig into the
// JSON shape OpenCode expects under the `mcp.<name>` key. Bootstrap MCPs are
// always local processes (bootstrap doesn't model remote URLs).
func MCPServerEntryFromBootstrap(cfg *bootstrap.MCPServerConfig) map[string]any {
	commandArr := make([]any, 0, 1+len(cfg.Args))
	commandArr = append(commandArr, cfg.Command)
	for _, a := range cfg.Args {
		commandArr = append(commandArr, a)
	}
	entry := map[string]any{
		"type":    "local",
		"enabled": true,
		"command": commandArr,
	}
	if len(cfg.Env) > 0 {
		entry["environment"] = cfg.Env
	}
	return entry
}

// RemoveMCPServer removes the named entry from opencode.json. If the config
// file doesn't exist, this is a no-op so we don't materialize a config when
// there's nothing to remove.
func RemoveMCPServer(configPath, name string) error {
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return nil
	}

	config, err := ReadOpenCodeConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to read opencode.json: %w", err)
	}

	delete(config.MCP, name)

	if err := WriteOpenCodeConfig(configPath, config); err != nil {
		return fmt.Errorf("failed to write opencode.json: %w", err)
	}
	return nil
}
