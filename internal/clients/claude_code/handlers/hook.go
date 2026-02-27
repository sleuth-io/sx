package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/handlers/dirasset"
	"github.com/sleuth-io/sx/internal/handlers/hook"
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
	zipFiles []string // populated during Install to resolve args to absolute paths
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

// Install installs the hook asset. Extracts files from the zip when there are
// script files to install, then registers the hook in settings.json.
func (h *HookHandler) Install(ctx context.Context, zipData []byte, targetBase string) error {
	if err := hook.ValidateZipForHook(zipData); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	h.zipFiles = hook.CacheZipFiles(zipData)

	if hook.HasExtractableFiles(zipData) {
		if err := hookOps.Install(ctx, zipData, targetBase, h.metadata.Asset.Name); err != nil {
			return err
		}
	}

	if err := h.updateSettings(targetBase); err != nil {
		return fmt.Errorf("failed to update settings: %w", err)
	}

	return nil
}

// Remove uninstalls the hook asset
func (h *HookHandler) Remove(ctx context.Context, targetBase string) error {
	if err := h.removeFromSettings(targetBase); err != nil {
		return fmt.Errorf("failed to remove from settings: %w", err)
	}

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
	return hook.ValidateZipForHook(zipData)
}

// mapEventToClaudeCode maps a canonical event name to Claude Code native event.
// If the hook has a [hook.claude-code] event override, that is returned instead.
func (h *HookHandler) mapEventToClaudeCode() (string, bool) {
	return hook.MapEvent(h.metadata.Hook.Event, claudeCodeEventMap, h.metadata.Hook.ClaudeCode)
}

// updateSettings updates settings.json to register the hook
func (h *HookHandler) updateSettings(targetBase string) error {
	settingsPath := filepath.Join(targetBase, "settings.json")

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

	hooks, ok := settings["hooks"].(map[string]any)
	if !ok {
		hooks = make(map[string]any)
		settings["hooks"] = hooks
	}

	hookEvent, supported := h.mapEventToClaudeCode()
	if !supported {
		return fmt.Errorf("hook event %q not supported for Claude Code", h.metadata.Hook.Event)
	}

	hookConfig := h.buildHookConfig(targetBase)

	eventHooks, ok := hooks[hookEvent].([]any)
	if !ok {
		eventHooks = []any{}
	}

	var filtered []any
	for _, hk := range eventHooks {
		hookMap, ok := hk.(map[string]any)
		if !ok {
			continue
		}
		assetID, ok := hookMap["_artifact"].(string)
		if !ok || assetID != h.metadata.Asset.Name {
			filtered = append(filtered, hk)
		}
	}

	filtered = append(filtered, hookConfig)
	hooks[hookEvent] = filtered

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
		return nil
	}

	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return fmt.Errorf("failed to read settings.json: %w", err)
	}

	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		return fmt.Errorf("failed to parse settings.json: %w", err)
	}

	hooks, ok := settings["hooks"].(map[string]any)
	if !ok {
		return nil
	}

	for eventName, eventHooksRaw := range hooks {
		eventHooks, ok := eventHooksRaw.([]any)
		if !ok {
			continue
		}

		var filtered []any
		for _, hk := range eventHooks {
			hookMap, ok := hk.(map[string]any)
			if !ok {
				continue
			}
			assetID, ok := hookMap["_artifact"].(string)
			if !ok || assetID != h.metadata.Asset.Name {
				filtered = append(filtered, hk)
			}
		}

		if len(filtered) == 0 {
			delete(hooks, eventName)
		} else {
			hooks[eventName] = filtered
		}
	}

	data, err = json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal settings: %w", err)
	}

	if err := os.WriteFile(settingsPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write settings.json: %w", err)
	}

	return nil
}

// buildHookConfig builds the hook configuration for settings.json.
// Claude Code expects the "matcher group" format:
//
//	{
//	  "matcher": "Edit|Write",          // top-level, optional
//	  "_artifact": "hook-name",         // sx tracking field
//	  "hooks": [                        // REQUIRED array of hook handlers
//	    { "type": "command", "command": "...", "timeout": 30 }
//	  ]
//	}
func (h *HookHandler) buildHookConfig(targetBase string) map[string]any {
	hookHandler := map[string]any{
		"type": "command",
	}

	installDir := filepath.Join(targetBase, h.GetInstallPath())
	resolved := hook.ResolveCommand(h.metadata.Hook, installDir, h.zipFiles)
	hookHandler["command"] = resolved.Command

	if h.metadata.Hook.Timeout > 0 {
		hookHandler["timeout"] = h.metadata.Hook.Timeout
	}

	if h.metadata.Hook.ClaudeCode != nil {
		for k, v := range h.metadata.Hook.ClaudeCode {
			if k == "event" || k == "matcher" {
				continue
			}
			hookHandler[k] = v
		}
	}

	config := map[string]any{
		"_artifact": h.metadata.Asset.Name,
		"hooks":     []any{hookHandler},
	}

	if h.metadata.Hook.Matcher != "" {
		config["matcher"] = h.metadata.Hook.Matcher
	}

	return config
}

// CanDetectInstalledState returns true since hooks can verify installation state
func (h *HookHandler) CanDetectInstalledState() bool {
	return true
}

// VerifyInstalled checks if the hook is properly installed
func (h *HookHandler) VerifyInstalled(targetBase string) (bool, string) {
	if h.metadata.Hook != nil && h.metadata.Hook.ScriptFile != "" {
		installDir := filepath.Join(targetBase, "hooks", h.metadata.Asset.Name)
		if utils.IsDirectory(installDir) {
			return hookOps.VerifyInstalled(targetBase, h.metadata.Asset.Name, h.metadata.Asset.Version)
		}
	}

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

	for _, eventHooksRaw := range hooks {
		eventHooks, ok := eventHooksRaw.([]any)
		if !ok {
			continue
		}
		for _, hk := range eventHooks {
			hookMap, ok := hk.(map[string]any)
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
