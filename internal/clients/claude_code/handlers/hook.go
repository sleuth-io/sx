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
	"github.com/sleuth-io/sx/internal/handlers/dirasset"
	"github.com/sleuth-io/sx/internal/metadata"
	"github.com/sleuth-io/sx/internal/utils"
)

var hookOps = dirasset.NewOperations("hooks", &asset.TypeHook)

// claudeCodeEventMap maps canonical hook events to Claude Code native event names
var claudeCodeEventMap = map[string]string{
	"session-start":         "SessionStart",
	"session-end":           "SessionEnd",
	"pre-tool-use":          "PreToolUse",
	"post-tool-use":         "PostToolUse",
	"post-tool-use-failure": "PostToolUseFailure",
	"user-prompt-submit":    "UserPromptSubmit",
	"stop":                  "Stop",
	"subagent-start":        "SubagentStart",
	"subagent-stop":         "SubagentStop",
	"pre-compact":           "PreCompact",
}

// HookHandler handles hook asset installation
type HookHandler struct {
	metadata *metadata.Metadata
}

// NewHookHandler creates a new hook handler
func NewHookHandler(meta *metadata.Metadata) *HookHandler {
	return &HookHandler{
		metadata: meta,
	}
}

// DetectType returns true if files indicate this is a hook asset
func (h *HookHandler) DetectType(files []string) bool {
	for _, file := range files {
		if file == "hook.sh" || file == "hook.py" || file == "hook.js" {
			return true
		}
	}
	return false
}

// GetType returns the asset type string
func (h *HookHandler) GetType() string {
	return "hook"
}

// CreateDefaultMetadata creates default metadata for a hook
func (h *HookHandler) CreateDefaultMetadata(name, version string) *metadata.Metadata {
	return &metadata.Metadata{
		MetadataVersion: "1.0",
		Asset: metadata.Asset{
			Name:    name,
			Version: version,
			Type:    asset.TypeHook,
		},
		Hook: &metadata.HookConfig{
			Event:      "pre-tool-use",
			ScriptFile: "hook.sh",
		},
	}
}

// GetPromptFile returns empty for hooks (not applicable)
func (h *HookHandler) GetPromptFile(meta *metadata.Metadata) string {
	return ""
}

// GetScriptFile returns the script file path for hooks
func (h *HookHandler) GetScriptFile(meta *metadata.Metadata) string {
	if meta.Hook != nil {
		return meta.Hook.ScriptFile
	}
	return ""
}

// ValidateMetadata validates hook-specific metadata
func (h *HookHandler) ValidateMetadata(meta *metadata.Metadata) error {
	if meta.Hook == nil {
		return errors.New("hook configuration missing")
	}
	if meta.Hook.Event == "" {
		return errors.New("hook event is required")
	}
	if meta.Hook.ScriptFile == "" && meta.Hook.Command == "" {
		return errors.New("hook script-file or command is required")
	}
	return nil
}

// DetectUsageFromToolCall detects hook usage from tool calls
// Hooks are not detectable from tool usage, so this always returns false
func (h *HookHandler) DetectUsageFromToolCall(toolName string, toolInput map[string]any) (string, bool) {
	return "", false
}

// Install installs the hook asset. For script-file hooks, extracts files and
// registers with absolute path. For command-only hooks, no extraction needed.
func (h *HookHandler) Install(ctx context.Context, zipData []byte, targetBase string) error {
	// Validate zip structure
	if err := h.Validate(zipData); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	if h.metadata.Hook.ScriptFile != "" {
		// Script mode: extract files to hooks directory
		if err := hookOps.Install(ctx, zipData, targetBase, h.metadata.Asset.Name); err != nil {
			return err
		}
	}
	// Command mode: no extraction needed

	// Update settings.json to register the hook
	if err := h.updateSettings(targetBase); err != nil {
		return fmt.Errorf("failed to update settings: %w", err)
	}

	return nil
}

// Remove uninstalls the hook asset
func (h *HookHandler) Remove(ctx context.Context, targetBase string) error {
	// Remove from settings.json first
	if err := h.removeFromSettings(targetBase); err != nil {
		return fmt.Errorf("failed to remove from settings: %w", err)
	}

	// Remove installation directory if it exists (script mode)
	installDir := filepath.Join(targetBase, "hooks", h.metadata.Asset.Name)
	if utils.IsDirectory(installDir) {
		return hookOps.Remove(ctx, targetBase, h.metadata.Asset.Name)
	}

	return nil
}

// GetInstallPath returns the installation path relative to targetBase
func (h *HookHandler) GetInstallPath() string {
	return filepath.Join("hooks", h.metadata.Asset.Name)
}

// Validate checks if the zip structure is valid for a hook asset
func (h *HookHandler) Validate(zipData []byte) error {
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
	if meta.Asset.Type != asset.TypeHook {
		return fmt.Errorf("asset type mismatch: expected hook, got %s", meta.Asset.Type)
	}

	// Check that hook section exists
	if meta.Hook == nil {
		return errors.New("[hook] section missing in metadata")
	}

	// Only check script file exists when using script-file mode
	if meta.Hook.ScriptFile != "" {
		if !containsFile(files, meta.Hook.ScriptFile) {
			return fmt.Errorf("script file not found in zip: %s", meta.Hook.ScriptFile)
		}
	}

	return nil
}

// mapEventToClaudeCode maps a canonical event name to Claude Code native event.
// If the hook has a [hook.claude-code] event override, that is returned instead.
func (h *HookHandler) mapEventToClaudeCode() (string, bool) {
	// Check for client-specific event override
	if h.metadata.Hook.ClaudeCode != nil {
		if eventOverride, ok := h.metadata.Hook.ClaudeCode["event"].(string); ok && eventOverride != "" {
			return eventOverride, true
		}
	}

	// Map canonical event to Claude Code native
	if nativeEvent, ok := claudeCodeEventMap[h.metadata.Hook.Event]; ok {
		return nativeEvent, true
	}

	return "", false
}

// updateSettings updates settings.json to register the hook
func (h *HookHandler) updateSettings(targetBase string) error {
	settingsPath := filepath.Join(targetBase, "settings.json")

	// Read existing settings or create new
	var settings map[string]any
	if utils.FileExists(settingsPath) {
		data, err := os.ReadFile(settingsPath)
		if err != nil {
			return fmt.Errorf("failed to read settings.json: %w", err)
		}
		if err := json.Unmarshal(data, &settings); err != nil {
			return fmt.Errorf("failed to parse settings.json: %w", err)
		}
	} else {
		settings = make(map[string]any)
	}

	// Ensure hooks section exists
	if settings["hooks"] == nil {
		settings["hooks"] = make(map[string]any)
	}
	hooks := settings["hooks"].(map[string]any)

	// Map canonical event to Claude Code event
	hookEvent, supported := h.mapEventToClaudeCode()
	if !supported {
		return fmt.Errorf("hook event %q not supported for Claude Code", h.metadata.Hook.Event)
	}

	// Build hook configuration
	hookConfig := h.buildHookConfig(targetBase)

	// Add/update hook entry
	if hooks[hookEvent] == nil {
		hooks[hookEvent] = []any{}
	}

	// Get existing hooks for this event
	eventHooks := hooks[hookEvent].([]any)

	// Remove any existing entry for this asset (by checking _artifact field)
	var filtered []any
	for _, hook := range eventHooks {
		hookMap, ok := hook.(map[string]any)
		if !ok {
			continue
		}
		assetID, ok := hookMap["_artifact"].(string)
		if !ok || assetID != h.metadata.Asset.Name {
			filtered = append(filtered, hook)
		}
	}

	// Add new hook entry
	filtered = append(filtered, hookConfig)
	hooks[hookEvent] = filtered

	// Write updated settings
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal settings: %w", err)
	}

	if err := os.WriteFile(settingsPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write settings.json: %w", err)
	}

	return nil
}

// removeFromSettings removes the hook from settings.json
func (h *HookHandler) removeFromSettings(targetBase string) error {
	settingsPath := filepath.Join(targetBase, "settings.json")

	if !utils.FileExists(settingsPath) {
		return nil // Nothing to remove
	}

	// Read settings
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return fmt.Errorf("failed to read settings.json: %w", err)
	}

	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		return fmt.Errorf("failed to parse settings.json: %w", err)
	}

	// Check if hooks section exists
	if settings["hooks"] == nil {
		return nil
	}
	hooks := settings["hooks"].(map[string]any)

	// Remove from all event types (in case event mapping changed)
	for eventName, eventHooksRaw := range hooks {
		eventHooks, ok := eventHooksRaw.([]any)
		if !ok {
			continue
		}

		var filtered []any
		for _, hook := range eventHooks {
			hookMap, ok := hook.(map[string]any)
			if !ok {
				continue
			}
			assetID, ok := hookMap["_artifact"].(string)
			if !ok || assetID != h.metadata.Asset.Name {
				filtered = append(filtered, hook)
			}
		}

		hooks[eventName] = filtered
	}

	// Write updated settings
	data, err = json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal settings: %w", err)
	}

	if err := os.WriteFile(settingsPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write settings.json: %w", err)
	}

	return nil
}

// buildHookConfig builds the hook configuration for settings.json
func (h *HookHandler) buildHookConfig(targetBase string) map[string]any {
	config := map[string]any{
		"_artifact": h.metadata.Asset.Name,
	}

	if h.metadata.Hook.ScriptFile != "" {
		// Script mode: use absolute path to script file
		scriptPath := filepath.Join(targetBase, h.GetInstallPath(), h.metadata.Hook.ScriptFile)
		config["command"] = scriptPath
	} else if h.metadata.Hook.Command != "" {
		// Command mode: join command and args
		cmd := h.metadata.Hook.Command
		if len(h.metadata.Hook.Args) > 0 {
			cmd = cmd + " " + strings.Join(h.metadata.Hook.Args, " ")
		}
		config["command"] = cmd
	}

	// Add matcher if present
	if h.metadata.Hook.Matcher != "" {
		config["matcher"] = h.metadata.Hook.Matcher
	}

	// Add timeout if present
	if h.metadata.Hook.Timeout > 0 {
		config["timeout"] = h.metadata.Hook.Timeout
	}

	// Merge Claude Code-specific overrides
	if h.metadata.Hook.ClaudeCode != nil {
		for k, v := range h.metadata.Hook.ClaudeCode {
			if k == "event" {
				continue // event override is handled in mapEventToClaudeCode
			}
			config[k] = v
		}
	}

	return config
}

// CanDetectInstalledState returns true since hooks can verify installation state
func (h *HookHandler) CanDetectInstalledState() bool {
	return true
}

// VerifyInstalled checks if the hook is properly installed
func (h *HookHandler) VerifyInstalled(targetBase string) (bool, string) {
	// For script-file hooks, check install directory
	if h.metadata.Hook != nil && h.metadata.Hook.ScriptFile != "" {
		installDir := filepath.Join(targetBase, "hooks", h.metadata.Asset.Name)
		if utils.IsDirectory(installDir) {
			return hookOps.VerifyInstalled(targetBase, h.metadata.Asset.Name, h.metadata.Asset.Version)
		}
	}

	// For command-only hooks, check settings.json
	settingsPath := filepath.Join(targetBase, "settings.json")
	if !utils.FileExists(settingsPath) {
		return false, "settings.json not found"
	}

	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return false, "failed to read settings.json"
	}

	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		return false, "failed to parse settings.json"
	}

	hooks, ok := settings["hooks"].(map[string]any)
	if !ok {
		return false, "hooks section not found"
	}

	// Check all event types for this asset
	for _, eventHooksRaw := range hooks {
		eventHooks, ok := eventHooksRaw.([]any)
		if !ok {
			continue
		}
		for _, hook := range eventHooks {
			hookMap, ok := hook.(map[string]any)
			if !ok {
				continue
			}
			if assetID, ok := hookMap["_artifact"].(string); ok && assetID == h.metadata.Asset.Name {
				return true, "installed"
			}
		}
	}

	return false, "hook not registered"
}
