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

var hookOps = dirasset.NewOperations("hooks", &asset.TypeHook)

// cursorEventMap maps canonical hook events to Cursor native event names
var cursorEventMap = map[string]string{
	"session-start":         "sessionStart",
	"session-end":           "sessionEnd",
	"pre-tool-use":          "preToolUse",
	"post-tool-use":         "postToolUse",
	"post-tool-use-failure": "postToolUseFailure",
	"user-prompt-submit":    "beforeSubmitPrompt",
	"stop":                  "stop",
	"subagent-start":        "subagentStart",
	"subagent-stop":         "subagentStop",
	"pre-compact":           "preCompact",
}

// HookHandler handles hook asset installation for Cursor
type HookHandler struct {
	metadata *metadata.Metadata
}

// NewHookHandler creates a new hook handler
func NewHookHandler(meta *metadata.Metadata) *HookHandler {
	return &HookHandler{metadata: meta}
}

// Install installs a hook asset to Cursor by extracting scripts and updating hooks.json
func (h *HookHandler) Install(ctx context.Context, zipData []byte, targetBase string) error {
	// Validate hook configuration
	if err := h.Validate(zipData); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	if h.metadata.Hook.ScriptFile != "" {
		// Script mode: extract to .cursor/hooks/{name}/
		installPath := filepath.Join(targetBase, "hooks", h.metadata.Asset.Name)
		if err := os.RemoveAll(installPath); err != nil {
			return fmt.Errorf("failed to remove existing hook: %w", err)
		}
		if err := utils.EnsureDir(installPath); err != nil {
			return fmt.Errorf("failed to create hook directory: %w", err)
		}
		if err := utils.ExtractZip(zipData, installPath); err != nil {
			return fmt.Errorf("failed to extract hook: %w", err)
		}
	}
	// Command mode: no extraction needed

	// Update hooks.json
	if err := h.updateHooksJSON(targetBase); err != nil {
		return fmt.Errorf("failed to update hooks.json: %w", err)
	}

	return nil
}

// Remove uninstalls a hook asset from Cursor
func (h *HookHandler) Remove(ctx context.Context, targetBase string) error {
	// Remove from hooks.json
	if err := h.removeFromHooksJSON(targetBase); err != nil {
		return fmt.Errorf("failed to remove from hooks.json: %w", err)
	}

	// Remove directory if it exists (script mode)
	installPath := filepath.Join(targetBase, "hooks", h.metadata.Asset.Name)
	if utils.IsDirectory(installPath) {
		if err := os.RemoveAll(installPath); err != nil {
			return fmt.Errorf("failed to remove hook directory: %w", err)
		}
	}

	return nil
}

// Validate checks if the zip structure is valid for a hook asset
func (h *HookHandler) Validate(zipData []byte) error {
	files, err := utils.ListZipFiles(zipData)
	if err != nil {
		return fmt.Errorf("failed to list zip files: %w", err)
	}

	if !containsFile(files, "metadata.toml") {
		return errors.New("metadata.toml not found in zip")
	}

	if h.metadata.Hook == nil {
		return errors.New("[hook] section missing in metadata")
	}

	// Only check script file when in script mode
	if h.metadata.Hook.ScriptFile != "" {
		if !containsFile(files, h.metadata.Hook.ScriptFile) {
			return fmt.Errorf("script file not found in zip: %s", h.metadata.Hook.ScriptFile)
		}
	}

	return nil
}

// HooksConfig represents Cursor's hooks.json structure
type HooksConfig struct {
	Version int                         `json:"version"`
	Hooks   map[string][]map[string]any `json:"hooks"`
}

// mapEventToCursor maps a canonical event name to Cursor native event.
// If the hook has a [hook.cursor] event override, that is returned instead.
func (h *HookHandler) mapEventToCursor() (string, bool) {
	// Check for client-specific event override
	if h.metadata.Hook.Cursor != nil {
		if eventOverride, ok := h.metadata.Hook.Cursor["event"].(string); ok && eventOverride != "" {
			return eventOverride, true
		}
	}

	// Map canonical event to Cursor native
	if nativeEvent, ok := cursorEventMap[h.metadata.Hook.Event]; ok {
		return nativeEvent, true
	}

	return "", false
}

func (h *HookHandler) updateHooksJSON(targetBase string) error {
	hooksJSONPath := filepath.Join(targetBase, "hooks.json")

	config, err := ReadHooksJSON(hooksJSONPath)
	if err != nil {
		return err
	}

	// Map event to Cursor lifecycle hook
	cursorEvent, supported := h.mapEventToCursor()
	if !supported {
		return fmt.Errorf("hook event %q not supported for Cursor", h.metadata.Hook.Event)
	}

	// Build entry
	entry := h.buildHookEntry(targetBase)

	// Add to hooks array
	if config.Hooks[cursorEvent] == nil {
		config.Hooks[cursorEvent] = []map[string]any{}
	}

	// Remove existing entry for this asset (if any)
	filtered := []map[string]any{}
	for _, hook := range config.Hooks[cursorEvent] {
		if assetName, ok := hook["_artifact"].(string); !ok || assetName != h.metadata.Asset.Name {
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
		filtered := []map[string]any{}
		for _, hook := range hooks {
			if assetName, ok := hook["_artifact"].(string); !ok || assetName != h.metadata.Asset.Name {
				filtered = append(filtered, hook)
			}
		}
		config.Hooks[eventName] = filtered
	}

	return WriteHooksJSON(hooksJSONPath, config)
}

// buildHookEntry builds the hook entry for hooks.json
func (h *HookHandler) buildHookEntry(targetBase string) map[string]any {
	entry := map[string]any{
		"_artifact": h.metadata.Asset.Name,
	}

	if h.metadata.Hook.ScriptFile != "" {
		// Script mode: use absolute path to script
		scriptPath := filepath.Join(targetBase, "hooks", h.metadata.Asset.Name, h.metadata.Hook.ScriptFile)
		entry["command"] = scriptPath
	} else if h.metadata.Hook.Command != "" {
		// Command mode: join command and args
		cmd := h.metadata.Hook.Command
		if len(h.metadata.Hook.Args) > 0 {
			cmd = cmd + " " + strings.Join(h.metadata.Hook.Args, " ")
		}
		entry["command"] = cmd
	}

	// Add timeout if present
	if h.metadata.Hook.Timeout > 0 {
		entry["timeout"] = h.metadata.Hook.Timeout
	}

	// Merge Cursor-specific overrides
	if h.metadata.Hook.Cursor != nil {
		for k, v := range h.metadata.Hook.Cursor {
			if k == "event" {
				continue // event override handled in mapEventToCursor
			}
			entry[k] = v
		}
	}

	return entry
}

// ReadHooksJSON reads and parses the hooks.json file
func ReadHooksJSON(path string) (*HooksConfig, error) {
	config := &HooksConfig{
		Version: 1,
		Hooks:   make(map[string][]map[string]any),
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
		config.Hooks = make(map[string][]map[string]any)
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

func containsFile(files []string, name string) bool {
	return slices.Contains(files, name)
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

	// For command-only hooks, check hooks.json
	hooksJSONPath := filepath.Join(targetBase, "hooks.json")
	config, err := ReadHooksJSON(hooksJSONPath)
	if err != nil {
		return false, "failed to read hooks.json"
	}

	for _, hooks := range config.Hooks {
		for _, hook := range hooks {
			if assetName, ok := hook["_artifact"].(string); ok && assetName == h.metadata.Asset.Name {
				return true, "installed"
			}
		}
	}

	return false, "hook not registered"
}
