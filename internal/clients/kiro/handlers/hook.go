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

// kiroEventMap maps canonical hook events to Kiro native event names
// Note: post-tool-use-failure is intentionally NOT mapped - Kiro doesn't have
// a failure-specific event, and mapping it to postToolUse would change semantics
// (hook would fire on all tool uses, not just failures)
var kiroEventMap = map[string]string{
	"session-start":      "sessionStart",
	"session-end":        "sessionEnd",
	"pre-tool-use":       "preToolUse",
	"post-tool-use":      "postToolUse",
	"user-prompt-submit": "promptSubmit",
	"stop":               "agentStop",
}

// KiroHookFile represents the JSON structure of a .kiro.hook file
type KiroHookFile struct {
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Version     string       `json:"version"`
	When        KiroHookWhen `json:"when"`
	Then        KiroHookThen `json:"then"`
}

// KiroHookWhen represents the trigger condition for a hook
type KiroHookWhen struct {
	Type      string   `json:"type"`
	ToolTypes []string `json:"toolTypes,omitempty"`
}

// KiroHookThen represents the action to take when a hook fires
type KiroHookThen struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

// HookHandler handles hook asset installation for Kiro
type HookHandler struct {
	metadata *metadata.Metadata
	zipFiles []string
}

// NewHookHandler creates a new hook handler
func NewHookHandler(meta *metadata.Metadata) *HookHandler {
	return &HookHandler{metadata: meta}
}

// Install installs a hook asset to Kiro by extracting scripts and writing a .kiro.hook file
func (h *HookHandler) Install(ctx context.Context, zipData []byte, targetBase string) error {
	if err := hook.ValidateZipForHook(zipData); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	h.zipFiles = hook.CacheZipFiles(zipData)

	// Extract hook files if there are any to extract (e.g., script files)
	if hook.HasExtractableFiles(zipData) {
		if err := hookOps.Install(ctx, zipData, targetBase, h.metadata.Asset.Name); err != nil {
			return err
		}
	}

	// Write the .kiro.hook file
	if err := h.writeHookFile(targetBase); err != nil {
		return fmt.Errorf("failed to write hook file: %w", err)
	}

	return nil
}

// Remove uninstalls a hook asset from Kiro
func (h *HookHandler) Remove(ctx context.Context, targetBase string) error {
	// Remove the .kiro.hook file
	hookFilePath := filepath.Join(targetBase, DirHooks, h.metadata.Asset.Name+".kiro.hook")
	if err := os.Remove(hookFilePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove hook file: %w", err)
	}

	// Remove extracted files if they exist
	installPath := filepath.Join(targetBase, DirHooks, h.metadata.Asset.Name)
	if utils.IsDirectory(installPath) {
		if err := os.RemoveAll(installPath); err != nil {
			return fmt.Errorf("failed to remove hook directory: %w", err)
		}
	}

	return nil
}

// VerifyInstalled checks if the hook is properly installed
func (h *HookHandler) VerifyInstalled(targetBase string) (bool, string) {
	hookFilePath := filepath.Join(targetBase, DirHooks, h.metadata.Asset.Name+".kiro.hook")

	data, err := os.ReadFile(hookFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, "hook file not found"
		}
		return false, "failed to read hook file: " + err.Error()
	}

	// Verify it's a valid hook file with our asset name
	var hookFile KiroHookFile
	if err := json.Unmarshal(data, &hookFile); err != nil {
		return false, "failed to parse hook file: " + err.Error()
	}

	// If script-file mode was used, verify the extracted files exist
	if h.metadata.Hook != nil && h.metadata.Hook.ScriptFile != "" {
		installDir := filepath.Join(targetBase, DirHooks, h.metadata.Asset.Name)
		if !utils.IsDirectory(installDir) {
			return false, "script directory not found"
		}
		return hookOps.VerifyInstalled(targetBase, h.metadata.Asset.Name, h.metadata.Asset.Version)
	}

	return true, "installed"
}

// mapEventToKiro maps a canonical event name to Kiro native event.
// If the hook has a [hook.kiro] event override, that is returned instead.
func (h *HookHandler) mapEventToKiro() (string, bool) {
	return hook.MapEvent(h.metadata.Hook.Event, kiroEventMap, h.metadata.Hook.Kiro)
}

// writeHookFile creates the .kiro.hook JSON file
func (h *HookHandler) writeHookFile(targetBase string) error {
	hooksDir := filepath.Join(targetBase, DirHooks)
	if err := utils.EnsureDir(hooksDir); err != nil {
		return fmt.Errorf("failed to create hooks directory: %w", err)
	}

	kiroEvent, supported := h.mapEventToKiro()
	if !supported {
		return fmt.Errorf("hook event %q not supported for Kiro", h.metadata.Hook.Event)
	}

	// Resolve the command to execute
	installDir := filepath.Join(targetBase, DirHooks, h.metadata.Asset.Name)
	resolved := hook.ResolveCommand(h.metadata.Hook, installDir, h.zipFiles)

	// Extract tool-types from [hook.kiro] if present
	var toolTypes []string
	if h.metadata.Hook.Kiro != nil {
		if tt, ok := h.metadata.Hook.Kiro["tool-types"].([]any); ok {
			for _, t := range tt {
				if s, ok := t.(string); ok {
					toolTypes = append(toolTypes, s)
				}
			}
		}
	}

	hookFile := KiroHookFile{
		Name:        h.metadata.Asset.Name,
		Description: h.metadata.Asset.Description,
		Version:     "1",
		When:        KiroHookWhen{Type: kiroEvent, ToolTypes: toolTypes},
		Then: KiroHookThen{
			Type:    "runCommand",
			Command: resolved.Command,
		},
	}

	hookFilePath := filepath.Join(hooksDir, h.metadata.Asset.Name+".kiro.hook")

	data, err := json.MarshalIndent(hookFile, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal hook file: %w", err)
	}
	data = append(data, '\n')

	return os.WriteFile(hookFilePath, data, 0644)
}
