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

// HookHandler handles hook artifact installation for Cursor
type HookHandler struct {
	metadata *metadata.Metadata
}

// NewHookHandler creates a new hook handler
func NewHookHandler(meta *metadata.Metadata) *HookHandler {
	return &HookHandler{metadata: meta}
}

// Install installs a hook artifact to Cursor by extracting scripts and updating hooks.json
func (h *HookHandler) Install(ctx context.Context, zipData []byte, targetBase string) error {
	// Validate hook configuration
	if err := h.Validate(zipData); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	// Extract to .cursor/hooks/{name}/
	installPath := filepath.Join(targetBase, "hooks", h.metadata.Artifact.Name)
	if err := os.RemoveAll(installPath); err != nil {
		return fmt.Errorf("failed to remove existing hook: %w", err)
	}
	if err := utils.EnsureDir(installPath); err != nil {
		return fmt.Errorf("failed to create hook directory: %w", err)
	}
	if err := utils.ExtractZip(zipData, installPath); err != nil {
		return fmt.Errorf("failed to extract hook: %w", err)
	}

	// Update hooks.json
	if err := h.updateHooksJSON(targetBase); err != nil {
		return fmt.Errorf("failed to update hooks.json: %w", err)
	}

	return nil
}

// Remove uninstalls a hook artifact from Cursor
func (h *HookHandler) Remove(ctx context.Context, targetBase string) error {
	// Remove from hooks.json
	if err := h.removeFromHooksJSON(targetBase); err != nil {
		return fmt.Errorf("failed to remove from hooks.json: %w", err)
	}

	// Remove directory
	installPath := filepath.Join(targetBase, "hooks", h.metadata.Artifact.Name)
	if err := os.RemoveAll(installPath); err != nil {
		return fmt.Errorf("failed to remove hook directory: %w", err)
	}

	return nil
}

// Validate checks if the zip structure is valid for a hook artifact
func (h *HookHandler) Validate(zipData []byte) error {
	files, err := utils.ListZipFiles(zipData)
	if err != nil {
		return fmt.Errorf("failed to list zip files: %w", err)
	}

	if !containsFile(files, "metadata.toml") {
		return fmt.Errorf("metadata.toml not found in zip")
	}

	if h.metadata.Hook == nil {
		return fmt.Errorf("[hook] section missing in metadata")
	}

	if !containsFile(files, h.metadata.Hook.ScriptFile) {
		return fmt.Errorf("script file not found in zip: %s", h.metadata.Hook.ScriptFile)
	}

	return nil
}

// HooksConfig represents Cursor's hooks.json structure
type HooksConfig struct {
	Version int                                 `json:"version"`
	Hooks   map[string][]map[string]interface{} `json:"hooks"`
}

func (h *HookHandler) updateHooksJSON(targetBase string) error {
	hooksJSONPath := filepath.Join(targetBase, "hooks.json")

	config, err := ReadHooksJSON(hooksJSONPath)
	if err != nil {
		return err
	}

	// Map event to Cursor lifecycle hook
	cursorEvent := mapEventToCursorHook(h.metadata.Hook.Event)
	if cursorEvent == "" {
		return fmt.Errorf("unsupported hook event for Cursor: %s (supported: pre-commit, post-commit, pre-push, on-save, on-file-read)", h.metadata.Hook.Event)
	}

	// Build entry with absolute path to script
	scriptPath := filepath.Join(targetBase, "hooks", h.metadata.Artifact.Name, h.metadata.Hook.ScriptFile)
	entry := map[string]interface{}{
		"command":   scriptPath,
		"_artifact": h.metadata.Artifact.Name,
	}

	// Add to hooks array
	if config.Hooks[cursorEvent] == nil {
		config.Hooks[cursorEvent] = []map[string]interface{}{}
	}

	// Remove existing entry for this artifact (if any)
	filtered := []map[string]interface{}{}
	for _, hook := range config.Hooks[cursorEvent] {
		if artifact, ok := hook["_artifact"].(string); !ok || artifact != h.metadata.Artifact.Name {
			filtered = append(filtered, hook)
		}
	}

	// Add new entry
	filtered = append(filtered, entry)
	config.Hooks[cursorEvent] = filtered

	return WriteHooksJSON(hooksJSONPath, config)
}

func (h *HookHandler) removeFromHooksJSON(targetBase string) error {
	hooksJSONPath := filepath.Join(targetBase, "hooks.json")

	config, err := ReadHooksJSON(hooksJSONPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // Nothing to remove
		}
		return err
	}

	// Remove from all hook types
	for eventName, hooks := range config.Hooks {
		filtered := []map[string]interface{}{}
		for _, hook := range hooks {
			if artifact, ok := hook["_artifact"].(string); !ok || artifact != h.metadata.Artifact.Name {
				filtered = append(filtered, hook)
			}
		}
		config.Hooks[eventName] = filtered
	}

	return WriteHooksJSON(hooksJSONPath, config)
}

// ReadHooksJSON reads and parses the hooks.json file
func ReadHooksJSON(path string) (*HooksConfig, error) {
	config := &HooksConfig{
		Version: 1,
		Hooks:   make(map[string][]map[string]interface{}),
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return config, nil
		}
		return nil, err
	}

	if err := json.Unmarshal(data, config); err != nil {
		return nil, err
	}

	if config.Hooks == nil {
		config.Hooks = make(map[string][]map[string]interface{})
	}

	return config, nil
}

// WriteHooksJSON writes the hooks config to the hooks.json file
func WriteHooksJSON(path string, config *HooksConfig) error {
	if err := utils.EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// mapEventToCursorHook maps Skills hook events to Cursor lifecycle hooks
func mapEventToCursorHook(event string) string {
	mapping := map[string]string{
		"pre-commit":   "beforeShellExecution",
		"post-commit":  "afterShellExecution",
		"pre-push":     "beforeShellExecution",
		"on-save":      "afterFileEdit",
		"on-file-read": "beforeReadFile",
		"after-edit":   "afterFileEdit",
	}

	if cursorEvent, ok := mapping[event]; ok {
		return cursorEvent
	}

	return "" // Unsupported
}

func containsFile(files []string, name string) bool {
	for _, f := range files {
		if f == name {
			return true
		}
	}
	return false
}

// VerifyInstalled checks if the hook is properly installed
func (h *HookHandler) VerifyInstalled(targetBase string) (bool, string) {
	return hookOps.VerifyInstalled(targetBase, h.metadata.Artifact.Name, h.metadata.Artifact.Version)
}
