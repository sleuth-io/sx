package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sleuth-io/skills/internal/asset"
	"github.com/sleuth-io/skills/internal/handlers/dirasset"
	"github.com/sleuth-io/skills/internal/metadata"
	"github.com/sleuth-io/skills/internal/utils"
)

var hookOps = dirasset.NewOperations("hooks", &asset.TypeHook)

// HookHandler handles hook artifact installation
type HookHandler struct {
	metadata *metadata.Metadata
}

// NewHookHandler creates a new hook handler
func NewHookHandler(meta *metadata.Metadata) *HookHandler {
	return &HookHandler{
		metadata: meta,
	}
}

// DetectType returns true if files indicate this is a hook artifact
func (h *HookHandler) DetectType(files []string) bool {
	for _, file := range files {
		if file == "hook.sh" || file == "hook.py" || file == "hook.js" {
			return true
		}
	}
	return false
}

// GetType returns the artifact type string
func (h *HookHandler) GetType() string {
	return "hook"
}

// CreateDefaultMetadata creates default metadata for a hook
func (h *HookHandler) CreateDefaultMetadata(name, version string) *metadata.Metadata {
	return &metadata.Metadata{
		MetadataVersion: "1.0",
		Artifact: metadata.Artifact{
			Name:    name,
			Version: version,
			Type:    asset.TypeHook,
		},
		Hook: &metadata.HookConfig{
			Event:      "pre-commit",
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
		return fmt.Errorf("hook configuration missing")
	}
	if meta.Hook.Event == "" {
		return fmt.Errorf("hook event is required")
	}
	if meta.Hook.ScriptFile == "" {
		return fmt.Errorf("hook script-file is required")
	}
	return nil
}

// DetectUsageFromToolCall detects hook usage from tool calls
// Hooks are not detectable from tool usage, so this always returns false
func (h *HookHandler) DetectUsageFromToolCall(toolName string, toolInput map[string]interface{}) (string, bool) {
	return "", false
}

// Install extracts and installs the hook artifact
func (h *HookHandler) Install(ctx context.Context, zipData []byte, targetBase string) error {
	// Validate zip structure
	if err := h.Validate(zipData); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	// Extract to hooks directory
	if err := hookOps.Install(ctx, zipData, targetBase, h.metadata.Artifact.Name); err != nil {
		return err
	}

	// Update settings.json to register the hook
	if err := h.updateSettings(targetBase); err != nil {
		return fmt.Errorf("failed to update settings: %w", err)
	}

	return nil
}

// Remove uninstalls the hook artifact
func (h *HookHandler) Remove(ctx context.Context, targetBase string) error {
	// Remove from settings.json first
	if err := h.removeFromSettings(targetBase); err != nil {
		return fmt.Errorf("failed to remove from settings: %w", err)
	}

	// Remove installation directory
	return hookOps.Remove(ctx, targetBase, h.metadata.Artifact.Name)
}

// GetInstallPath returns the installation path relative to targetBase
func (h *HookHandler) GetInstallPath() string {
	return filepath.Join("hooks", h.metadata.Artifact.Name)
}

// Validate checks if the zip structure is valid for a hook artifact
func (h *HookHandler) Validate(zipData []byte) error {
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
	if meta.Artifact.Type != asset.TypeHook {
		return fmt.Errorf("artifact type mismatch: expected hook, got %s", meta.Artifact.Type)
	}

	// Check that script file exists
	if meta.Hook == nil {
		return fmt.Errorf("[hook] section missing in metadata")
	}

	if !containsFile(files, meta.Hook.ScriptFile) {
		return fmt.Errorf("script file not found in zip: %s", meta.Hook.ScriptFile)
	}

	return nil
}

// updateSettings updates settings.json to register the hook
func (h *HookHandler) updateSettings(targetBase string) error {
	settingsPath := filepath.Join(targetBase, "settings.json")

	// Read existing settings or create new
	var settings map[string]interface{}
	if utils.FileExists(settingsPath) {
		data, err := os.ReadFile(settingsPath)
		if err != nil {
			return fmt.Errorf("failed to read settings.json: %w", err)
		}
		if err := json.Unmarshal(data, &settings); err != nil {
			return fmt.Errorf("failed to parse settings.json: %w", err)
		}
	} else {
		settings = make(map[string]interface{})
	}

	// Ensure hooks section exists
	if settings["hooks"] == nil {
		settings["hooks"] = make(map[string]interface{})
	}
	hooks := settings["hooks"].(map[string]interface{})

	// Build hook configuration
	hookConfig := h.buildHookConfig()

	// Add/update hook entry
	hookEvent := h.metadata.Hook.Event
	if hooks[hookEvent] == nil {
		hooks[hookEvent] = []interface{}{}
	}

	// Get existing hooks for this event
	eventHooks := hooks[hookEvent].([]interface{})

	// Remove any existing entry for this artifact (by checking _artifact field)
	var filtered []interface{}
	for _, hook := range eventHooks {
		hookMap, ok := hook.(map[string]interface{})
		if !ok {
			continue
		}
		artifactID, ok := hookMap["_artifact"].(string)
		if !ok || artifactID != h.metadata.Artifact.Name {
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

	var settings map[string]interface{}
	if err := json.Unmarshal(data, &settings); err != nil {
		return fmt.Errorf("failed to parse settings.json: %w", err)
	}

	// Check if hooks section exists
	if settings["hooks"] == nil {
		return nil
	}
	hooks := settings["hooks"].(map[string]interface{})

	// Remove from the specific event
	hookEvent := h.metadata.Hook.Event
	if hooks[hookEvent] == nil {
		return nil
	}

	eventHooks := hooks[hookEvent].([]interface{})

	// Filter out this artifact's hook
	var filtered []interface{}
	for _, hook := range eventHooks {
		hookMap, ok := hook.(map[string]interface{})
		if !ok {
			continue
		}
		artifactID, ok := hookMap["_artifact"].(string)
		if !ok || artifactID != h.metadata.Artifact.Name {
			filtered = append(filtered, hook)
		}
	}

	hooks[hookEvent] = filtered

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
func (h *HookHandler) buildHookConfig() map[string]interface{} {
	// Get absolute path to script file
	scriptPath := filepath.Join(h.GetInstallPath(), h.metadata.Hook.ScriptFile)

	config := map[string]interface{}{
		"script":    scriptPath,
		"_artifact": h.metadata.Artifact.Name,
	}

	// Add optional fields
	if h.metadata.Hook.Async {
		config["async"] = true
	}
	if !h.metadata.Hook.FailOnError {
		config["failOnError"] = false
	}
	if h.metadata.Hook.Timeout > 0 {
		config["timeout"] = h.metadata.Hook.Timeout
	}

	return config
}

// CanDetectInstalledState returns true since hooks preserve metadata.toml
func (h *HookHandler) CanDetectInstalledState() bool {
	return true
}

// VerifyInstalled checks if the hook is properly installed
func (h *HookHandler) VerifyInstalled(targetBase string) (bool, string) {
	return hookOps.VerifyInstalled(targetBase, h.metadata.Artifact.Name, h.metadata.Artifact.Version)
}
