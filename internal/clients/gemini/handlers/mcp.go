package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"

	"github.com/sleuth-io/sx/internal/metadata"
	"github.com/sleuth-io/sx/internal/utils"
)

// MCPHandler handles MCP asset installation for Gemini
type MCPHandler struct {
	metadata *metadata.Metadata
}

// NewMCPHandler creates a new MCP handler
func NewMCPHandler(meta *metadata.Metadata) *MCPHandler {
	return &MCPHandler{metadata: meta}
}

// Install installs an MCP asset to Gemini by updating settings.json.
// For packaged assets, extracts files first. For config-only, registers as-is.
// Also installs to JetBrains IDEs if detected.
func (h *MCPHandler) Install(ctx context.Context, zipData []byte, targetBase string) error {
	settingsPath := filepath.Join(targetBase, SettingsFile)

	// Read existing settings.json
	config, err := ReadSettingsJSON(settingsPath)
	if err != nil {
		return fmt.Errorf("failed to read settings.json: %w", err)
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

	// Add to CLI config (settings.json)
	if config.MCPServers == nil {
		config.MCPServers = make(map[string]any)
	}
	config.MCPServers[h.metadata.Asset.Name] = entry

	// Write updated settings.json
	if err := WriteSettingsJSON(settingsPath, config); err != nil {
		return fmt.Errorf("failed to write settings.json: %w", err)
	}

	// Also install to JetBrains IDEs (best effort, ignore errors)
	h.installToJetBrains(entry)

	return nil
}

// installToJetBrains installs the MCP server to all detected JetBrains IDEs
func (h *MCPHandler) installToJetBrains(entry map[string]any) {
	server := JetBrainsMCPServer{}

	if cmd, ok := entry["command"].(string); ok {
		server.Command = cmd
	}
	if args, ok := entry["args"].([]any); ok {
		for _, arg := range args {
			if s, ok := arg.(string); ok {
				server.Args = append(server.Args, s)
			}
		}
	}
	if url, ok := entry["url"].(string); ok {
		server.URL = url
	}
	if env, ok := entry["env"].(map[string]string); ok {
		server.Env = env
	}

	// Best effort - ignore errors for JetBrains
	_ = AddJetBrainsMCPServer(h.metadata.Asset.Name, server)
}

// Remove removes an MCP entry from Gemini
func (h *MCPHandler) Remove(ctx context.Context, targetBase string) error {
	settingsPath := filepath.Join(targetBase, SettingsFile)

	// Read existing settings.json
	config, err := ReadSettingsJSON(settingsPath)
	if err != nil {
		return fmt.Errorf("failed to read settings.json: %w", err)
	}

	// Remove entry from CLI config
	delete(config.MCPServers, h.metadata.Asset.Name)

	// Write updated settings.json
	if err := WriteSettingsJSON(settingsPath, config); err != nil {
		return fmt.Errorf("failed to write settings.json: %w", err)
	}

	// Remove server directory if it exists (packaged mode)
	serverDir := filepath.Join(targetBase, DirMCPServers, h.metadata.Asset.Name)
	os.RemoveAll(serverDir) // Ignore errors if doesn't exist

	// Also remove from JetBrains IDEs (best effort)
	_ = RemoveJetBrainsMCPServer(h.metadata.Asset.Name)

	return nil
}

func (h *MCPHandler) generatePackagedMCPEntry(serverDir string) map[string]any {
	mcpConfig := h.metadata.MCP

	command, args := utils.ResolveCommandAndArgs(mcpConfig.Command, mcpConfig.Args, serverDir)

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
	entry := map[string]any{
		"command": mcpConfig.Command,
		"args":    utils.StringsToAny(mcpConfig.Args),
	}

	// Add env if present
	if len(mcpConfig.Env) > 0 {
		entry["env"] = mcpConfig.Env
	}

	return entry
}

// VerifyInstalled checks if the MCP server is properly installed.
func (h *MCPHandler) VerifyInstalled(targetBase string) (bool, string) {
	// Check if install directory exists (packaged mode)
	installDir := filepath.Join(targetBase, DirMCPServers, h.metadata.Asset.Name)
	if utils.IsDirectory(installDir) {
		return true, "installed (packaged)"
	}

	// Check CLI config (settings.json)
	settingsPath := filepath.Join(targetBase, SettingsFile)
	config, err := ReadSettingsJSON(settingsPath)
	if err == nil {
		if _, exists := config.MCPServers[h.metadata.Asset.Name]; exists {
			return true, "installed (CLI)"
		}
	}

	// Check JetBrains IDEs
	if HasJetBrainsMCPServer(h.metadata.Asset.Name) {
		return true, "installed (JetBrains)"
	}

	return false, "MCP server not registered"
}

// SettingsJSON represents Gemini's settings.json structure
type SettingsJSON struct {
	MCPServers map[string]any           `json:"mcpServers,omitempty"`
	Hooks      map[string][]HookMatcher `json:"hooks,omitempty"`
	// Preserve other fields
	Other map[string]any `json:"-"`
}

// HookMatcher represents a hook matcher entry in settings.json
type HookMatcher struct {
	Matcher string      `json:"matcher,omitempty"`
	Hooks   []HookEntry `json:"hooks"`
}

// HookEntry represents a single hook configuration
type HookEntry struct {
	Name        string `json:"name,omitempty"`
	Type        string `json:"type"`
	Command     string `json:"command"`
	Timeout     int    `json:"timeout,omitempty"`
	Description string `json:"description,omitempty"`
}

// ReadSettingsJSON reads Gemini's settings.json file
func ReadSettingsJSON(path string) (*SettingsJSON, error) {
	config := &SettingsJSON{
		MCPServers: make(map[string]any),
		Hooks:      make(map[string][]HookMatcher),
		Other:      make(map[string]any),
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return config, nil // Return empty config
		}
		return nil, err
	}

	// First, unmarshal into a generic map to preserve unknown fields
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}

	// Extract mcpServers
	if servers, ok := raw["mcpServers"].(map[string]any); ok {
		config.MCPServers = servers
	}

	// Extract hooks - need to re-marshal and unmarshal to get proper typed structure
	if hooks, ok := raw["hooks"]; ok {
		hooksData, err := json.Marshal(hooks)
		if err == nil {
			var typedHooks map[string][]HookMatcher
			if err := json.Unmarshal(hooksData, &typedHooks); err == nil {
				config.Hooks = typedHooks
			}
		}
	}

	// Preserve other fields
	for k, v := range raw {
		if k != "mcpServers" && k != "hooks" {
			config.Other[k] = v
		}
	}

	return config, nil
}

// WriteSettingsJSON writes Gemini's settings.json file
func WriteSettingsJSON(path string, config *SettingsJSON) error {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// Build output map preserving other fields
	output := make(map[string]any)
	maps.Copy(output, config.Other)

	// Add mcpServers if non-empty
	if len(config.MCPServers) > 0 {
		output["mcpServers"] = config.MCPServers
	}

	// Add hooks if non-empty
	if len(config.Hooks) > 0 {
		output["hooks"] = config.Hooks
	}

	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// AddMCPServer adds or updates an MCP server entry in Gemini's settings.json
func AddMCPServer(targetBase, serverName string, serverConfig map[string]any) error {
	settingsPath := filepath.Join(targetBase, SettingsFile)

	// Read existing config
	config, err := ReadSettingsJSON(settingsPath)
	if err != nil {
		return fmt.Errorf("failed to read settings.json: %w", err)
	}

	// Add/update MCP server entry
	if config.MCPServers == nil {
		config.MCPServers = make(map[string]any)
	}
	config.MCPServers[serverName] = serverConfig

	// Write updated config
	if err := WriteSettingsJSON(settingsPath, config); err != nil {
		return fmt.Errorf("failed to write settings.json: %w", err)
	}

	return nil
}

// RemoveMCPServer removes an MCP server entry from Gemini's settings.json
func RemoveMCPServer(targetBase, serverName string) error {
	settingsPath := filepath.Join(targetBase, SettingsFile)

	// Read existing config
	config, err := ReadSettingsJSON(settingsPath)
	if err != nil {
		return fmt.Errorf("failed to read settings.json: %w", err)
	}

	// Remove the server
	delete(config.MCPServers, serverName)

	// Write updated config
	if err := WriteSettingsJSON(settingsPath, config); err != nil {
		return fmt.Errorf("failed to write settings.json: %w", err)
	}

	return nil
}

// AddHook adds or updates a hook entry in Gemini's settings.json
// hookName is used as a unique identifier to update existing hooks
func AddHook(targetBase, event, hookName, command string) error {
	settingsPath := filepath.Join(targetBase, SettingsFile)

	// Read existing config
	config, err := ReadSettingsJSON(settingsPath)
	if err != nil {
		return fmt.Errorf("failed to read settings.json: %w", err)
	}

	// Initialize hooks map if needed
	if config.Hooks == nil {
		config.Hooks = make(map[string][]HookMatcher)
	}

	// Create the new hook entry
	newEntry := HookEntry{
		Name:    hookName,
		Type:    "command",
		Command: command,
	}

	// Check if there's already a matcher for this event
	matchers := config.Hooks[event]
	if len(matchers) == 0 {
		// Create new matcher with our hook
		config.Hooks[event] = []HookMatcher{
			{
				Hooks: []HookEntry{newEntry},
			},
		}
	} else {
		// Look for existing hook with same name and update, or add new
		found := false
		for i := range matchers {
			for j := range matchers[i].Hooks {
				if matchers[i].Hooks[j].Name == hookName {
					matchers[i].Hooks[j] = newEntry
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			// Add to first matcher
			matchers[0].Hooks = append(matchers[0].Hooks, newEntry)
		}
		config.Hooks[event] = matchers
	}

	// Write updated config
	if err := WriteSettingsJSON(settingsPath, config); err != nil {
		return fmt.Errorf("failed to write settings.json: %w", err)
	}

	return nil
}

// RemoveHook removes a hook entry from Gemini's settings.json
func RemoveHook(targetBase, event, hookName string) error {
	settingsPath := filepath.Join(targetBase, SettingsFile)

	// Read existing config
	config, err := ReadSettingsJSON(settingsPath)
	if err != nil {
		return fmt.Errorf("failed to read settings.json: %w", err)
	}

	if config.Hooks == nil {
		return nil // No hooks to remove
	}

	matchers, exists := config.Hooks[event]
	if !exists {
		return nil // Event not found
	}

	// Remove hook from all matchers
	for i := range matchers {
		filtered := []HookEntry{}
		for _, hook := range matchers[i].Hooks {
			if hook.Name != hookName {
				filtered = append(filtered, hook)
			}
		}
		matchers[i].Hooks = filtered
	}

	// Clean up empty matchers
	nonEmpty := []HookMatcher{}
	for _, m := range matchers {
		if len(m.Hooks) > 0 {
			nonEmpty = append(nonEmpty, m)
		}
	}

	if len(nonEmpty) == 0 {
		delete(config.Hooks, event)
	} else {
		config.Hooks[event] = nonEmpty
	}

	// Write updated config
	if err := WriteSettingsJSON(settingsPath, config); err != nil {
		return fmt.Errorf("failed to write settings.json: %w", err)
	}

	return nil
}

// HasHook checks if a hook with the given name exists for the event
func HasHook(targetBase, event, hookName string) (bool, error) {
	settingsPath := filepath.Join(targetBase, SettingsFile)

	config, err := ReadSettingsJSON(settingsPath)
	if err != nil {
		return false, fmt.Errorf("failed to read settings.json: %w", err)
	}

	if config.Hooks == nil {
		return false, nil
	}

	matchers, exists := config.Hooks[event]
	if !exists {
		return false, nil
	}

	for _, matcher := range matchers {
		for _, hook := range matcher.Hooks {
			if hook.Name == hookName {
				return true, nil
			}
		}
	}

	return false, nil
}
