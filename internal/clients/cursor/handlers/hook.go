package handlers

import (
	"context"
	"encoding/json"
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
	zipFiles []string // populated during Install to resolve args to absolute paths
}

// NewHookHandler creates a new hook handler
func NewHookHandler(meta *metadata.Metadata) *HookHandler {
	return &HookHandler{metadata: meta}
}

// Install installs a hook asset to Cursor by extracting scripts and updating hooks.json
func (h *HookHandler) Install(ctx context.Context, zipData []byte, targetBase string) error {
	if err := hook.ValidateZipForHook(zipData); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	h.zipFiles = hook.CacheZipFiles(zipData)

	if hook.HasExtractableFiles(zipData) {
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

	if err := h.updateHooksJSON(targetBase); err != nil {
		return fmt.Errorf("failed to update hooks.json: %w", err)
	}

	return nil
}

// Remove uninstalls a hook asset from Cursor
func (h *HookHandler) Remove(ctx context.Context, targetBase string) error {
	if err := h.removeFromHooksJSON(targetBase); err != nil {
		return fmt.Errorf("failed to remove from hooks.json: %w", err)
	}

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
	return hook.ValidateZipForHook(zipData)
}

// HooksConfig represents Cursor's hooks.json structure
type HooksConfig struct {
	Version int                         `json:"version"`
	Hooks   map[string][]map[string]any `json:"hooks"`
}

// mapEventToCursor maps a canonical event name to Cursor native event.
// If the hook has a [hook.cursor] event override, that is returned instead.
func (h *HookHandler) mapEventToCursor() (string, bool) {
	return hook.MapEvent(h.metadata.Hook.Event, cursorEventMap, h.metadata.Hook.Cursor)
}

func (h *HookHandler) updateHooksJSON(targetBase string) error {
	hooksJSONPath := filepath.Join(targetBase, "hooks.json")

	config, err := ReadHooksJSON(hooksJSONPath)
	if err != nil {
		return err
	}

	cursorEvent, supported := h.mapEventToCursor()
	if !supported {
		return fmt.Errorf("hook event %q not supported for Cursor", h.metadata.Hook.Event)
	}

	entry := h.buildHookEntry(targetBase)

	if config.Hooks[cursorEvent] == nil {
		config.Hooks[cursorEvent] = []map[string]any{}
	}

	filtered := []map[string]any{}
	for _, hk := range config.Hooks[cursorEvent] {
		if assetName, ok := hk["_artifact"].(string); !ok || assetName != h.metadata.Asset.Name {
			filtered = append(filtered, hk)
		}
	}

	filtered = append(filtered, entry)
	config.Hooks[cursorEvent] = filtered

	return WriteHooksJSON(hooksJSONPath, config)
}

func (h *HookHandler) removeFromHooksJSON(targetBase string) error {
	hooksJSONPath := filepath.Join(targetBase, "hooks.json")

	config, err := ReadHooksJSON(hooksJSONPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for eventName, hooks := range config.Hooks {
		filtered := []map[string]any{}
		for _, hk := range hooks {
			if assetName, ok := hk["_artifact"].(string); !ok || assetName != h.metadata.Asset.Name {
				filtered = append(filtered, hk)
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

	installDir := filepath.Join(targetBase, "hooks", h.metadata.Asset.Name)
	resolved := hook.ResolveCommand(h.metadata.Hook, installDir, h.zipFiles)
	entry["command"] = resolved.Command

	if h.metadata.Hook.Timeout > 0 {
		entry["timeout"] = h.metadata.Hook.Timeout
	}

	if h.metadata.Hook.Cursor != nil {
		for k, v := range h.metadata.Hook.Cursor {
			if k == "event" {
				continue
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

// VerifyInstalled checks if the hook is properly installed
func (h *HookHandler) VerifyInstalled(targetBase string) (bool, string) {
	if h.metadata.Hook != nil && h.metadata.Hook.ScriptFile != "" {
		installDir := filepath.Join(targetBase, "hooks", h.metadata.Asset.Name)
		if utils.IsDirectory(installDir) {
			return hookOps.VerifyInstalled(targetBase, h.metadata.Asset.Name, h.metadata.Asset.Version)
		}
	}

	hooksJSONPath := filepath.Join(targetBase, "hooks.json")
	config, err := ReadHooksJSON(hooksJSONPath)
	if err != nil {
		return false, "failed to read hooks.json"
	}

	for _, hooks := range config.Hooks {
		for _, hk := range hooks {
			if assetName, ok := hk["_artifact"].(string); ok && assetName == h.metadata.Asset.Name {
				return true, "installed"
			}
		}
	}

	return false, "hook not registered"
}
