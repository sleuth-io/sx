package handlers

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/handlers/dirasset"
	"github.com/sleuth-io/sx/internal/handlers/hook"
	"github.com/sleuth-io/sx/internal/metadata"
	"github.com/sleuth-io/sx/internal/utils"
)

var hookOps = dirasset.NewOperations("hooks", &asset.TypeHook)

// geminiEventMap maps canonical hook events to Gemini native event names
var geminiEventMap = map[string]string{
	"session-start":         "SessionStart",
	"session-end":           "SessionEnd",
	"pre-tool-use":          "PreToolUse",
	"post-tool-use":         "AfterTool",
	"post-tool-use-failure": "AfterTool", // Gemini doesn't distinguish failure
	"user-prompt-submit":    "UserPromptSubmit",
	"stop":                  "Stop",
}

// HookHandler handles hook asset installation for Gemini
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

// Install installs the hook asset to Gemini
// For script-file hooks, extracts files and registers with absolute path.
// For command-only hooks, no extraction needed.
func (h *HookHandler) Install(ctx context.Context, zipData []byte, targetBase string) error {
	if err := hook.ValidateZipForHook(zipData); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	// For global scope, targetBase is already ~/.gemini
	// For repo scope, targetBase is /repo, so we need .gemini/
	geminiDir := targetBase
	if filepath.Base(targetBase) != ConfigDir {
		geminiDir = filepath.Join(targetBase, ConfigDir)
	}

	h.zipFiles = hook.CacheZipFiles(zipData)

	if hook.HasExtractableFiles(zipData) {
		if err := hookOps.Install(ctx, zipData, geminiDir, h.metadata.Asset.Name); err != nil {
			return err
		}
	}

	if err := h.updateSettings(geminiDir); err != nil {
		return fmt.Errorf("failed to update settings: %w", err)
	}

	return nil
}

// Remove uninstalls the hook asset from Gemini
func (h *HookHandler) Remove(ctx context.Context, targetBase string) error {
	geminiDir := targetBase
	if filepath.Base(targetBase) != ConfigDir {
		geminiDir = filepath.Join(targetBase, ConfigDir)
	}

	if err := h.removeFromSettings(geminiDir); err != nil {
		return fmt.Errorf("failed to remove from settings: %w", err)
	}

	installDir := filepath.Join(geminiDir, "hooks", h.metadata.Asset.Name)
	if utils.IsDirectory(installDir) {
		return hookOps.Remove(ctx, geminiDir, h.metadata.Asset.Name)
	}

	return nil
}

// VerifyInstalled checks if the hook is properly installed
func (h *HookHandler) VerifyInstalled(targetBase string) (bool, string) {
	geminiDir := targetBase
	if filepath.Base(targetBase) != ConfigDir {
		geminiDir = filepath.Join(targetBase, ConfigDir)
	}

	if h.metadata.Hook != nil && h.metadata.Hook.ScriptFile != "" {
		installDir := filepath.Join(geminiDir, "hooks", h.metadata.Asset.Name)
		if utils.IsDirectory(installDir) {
			return hookOps.VerifyInstalled(geminiDir, h.metadata.Asset.Name, h.metadata.Asset.Version)
		}
	}

	hookEvent, supported := h.mapEventToGemini()
	if !supported {
		return false, "unsupported hook event"
	}

	found, err := HasHook(geminiDir, hookEvent, h.metadata.Asset.Name)
	if err != nil {
		return false, "failed to check settings.json"
	}

	if found {
		return true, "installed"
	}

	return false, "hook not registered"
}

// Validate checks if the zip structure is valid for a hook asset
func (h *HookHandler) Validate(zipData []byte) error {
	return hook.ValidateZipForHook(zipData)
}

// mapEventToGemini maps a canonical event name to Gemini native event.
// If the hook has a [hook.gemini] event override, that is returned instead.
func (h *HookHandler) mapEventToGemini() (string, bool) {
	return hook.MapEvent(h.metadata.Hook.Event, geminiEventMap, h.metadata.Hook.Gemini)
}

// updateSettings updates settings.json to register the hook
func (h *HookHandler) updateSettings(geminiDir string) error {
	hookEvent, supported := h.mapEventToGemini()
	if !supported {
		return fmt.Errorf("hook event %q not supported for Gemini", h.metadata.Hook.Event)
	}

	installDir := filepath.Join(geminiDir, "hooks", h.metadata.Asset.Name)
	resolved := hook.ResolveCommand(h.metadata.Hook, installDir, h.zipFiles)

	return AddHook(geminiDir, hookEvent, h.metadata.Asset.Name, resolved.Command)
}

// removeFromSettings removes the hook from settings.json
func (h *HookHandler) removeFromSettings(geminiDir string) error {
	hookEvent, supported := h.mapEventToGemini()
	if !supported {
		for _, event := range geminiEventMap {
			_ = RemoveHook(geminiDir, event, h.metadata.Asset.Name)
		}
		return nil
	}

	return RemoveHook(geminiDir, hookEvent, h.metadata.Asset.Name)
}

// GetInstallPath returns the installation path relative to geminiDir
func (h *HookHandler) GetInstallPath() string {
	return filepath.Join(DirHooks, h.metadata.Asset.Name)
}
