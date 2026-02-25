package handlers

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/handlers/dirasset"
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
	// Validate zip structure
	if err := h.Validate(zipData); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	// For global scope, targetBase is already ~/.gemini
	// For repo scope, targetBase is /repo, so we need .gemini/
	geminiDir := targetBase
	if filepath.Base(targetBase) != ConfigDir {
		geminiDir = filepath.Join(targetBase, ConfigDir)
	}

	if h.metadata.Hook.ScriptFile != "" {
		// Script mode: extract files to hooks directory
		if err := hookOps.Install(ctx, zipData, geminiDir, h.metadata.Asset.Name); err != nil {
			return err
		}
	}
	// Command mode: no extraction needed

	// Update settings.json to register the hook
	if err := h.updateSettings(geminiDir); err != nil {
		return fmt.Errorf("failed to update settings: %w", err)
	}

	return nil
}

// Remove uninstalls the hook asset from Gemini
func (h *HookHandler) Remove(ctx context.Context, targetBase string) error {
	// For global scope, targetBase is already ~/.gemini
	// For repo scope, targetBase is /repo, so we need .gemini/
	geminiDir := targetBase
	if filepath.Base(targetBase) != ConfigDir {
		geminiDir = filepath.Join(targetBase, ConfigDir)
	}

	// Remove from settings.json first
	if err := h.removeFromSettings(geminiDir); err != nil {
		return fmt.Errorf("failed to remove from settings: %w", err)
	}

	// Remove installation directory if it exists (script mode)
	installDir := filepath.Join(geminiDir, "hooks", h.metadata.Asset.Name)
	if utils.IsDirectory(installDir) {
		return hookOps.Remove(ctx, geminiDir, h.metadata.Asset.Name)
	}

	return nil
}

// VerifyInstalled checks if the hook is properly installed
func (h *HookHandler) VerifyInstalled(targetBase string) (bool, string) {
	// For global scope, targetBase is already ~/.gemini
	// For repo scope, targetBase is /repo, so we need .gemini/
	geminiDir := targetBase
	if filepath.Base(targetBase) != ConfigDir {
		geminiDir = filepath.Join(targetBase, ConfigDir)
	}

	// For script-file hooks, check install directory
	if h.metadata.Hook != nil && h.metadata.Hook.ScriptFile != "" {
		installDir := filepath.Join(geminiDir, "hooks", h.metadata.Asset.Name)
		if utils.IsDirectory(installDir) {
			return hookOps.VerifyInstalled(geminiDir, h.metadata.Asset.Name, h.metadata.Asset.Version)
		}
	}

	// For command-only hooks, check settings.json
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

// mapEventToGemini maps a canonical event name to Gemini native event.
// If the hook has a [hook.gemini] event override, that is returned instead.
func (h *HookHandler) mapEventToGemini() (string, bool) {
	// Check for client-specific event override
	if h.metadata.Hook.Gemini != nil {
		if eventOverride, ok := h.metadata.Hook.Gemini["event"].(string); ok && eventOverride != "" {
			return eventOverride, true
		}
	}

	// Map canonical event to Gemini native
	if nativeEvent, ok := geminiEventMap[h.metadata.Hook.Event]; ok {
		return nativeEvent, true
	}

	return "", false
}

// updateSettings updates settings.json to register the hook
func (h *HookHandler) updateSettings(geminiDir string) error {
	// Map canonical event to Gemini event
	hookEvent, supported := h.mapEventToGemini()
	if !supported {
		return fmt.Errorf("hook event %q not supported for Gemini", h.metadata.Hook.Event)
	}

	// Build the command
	command := h.buildCommand(geminiDir)

	return AddHook(geminiDir, hookEvent, h.metadata.Asset.Name, command)
}

// removeFromSettings removes the hook from settings.json
func (h *HookHandler) removeFromSettings(geminiDir string) error {
	// Map canonical event to Gemini event
	hookEvent, supported := h.mapEventToGemini()
	if !supported {
		// Try to remove from all event types in case event mapping changed
		for _, event := range geminiEventMap {
			_ = RemoveHook(geminiDir, event, h.metadata.Asset.Name)
		}
		return nil
	}

	return RemoveHook(geminiDir, hookEvent, h.metadata.Asset.Name)
}

// buildCommand builds the command string for the hook
func (h *HookHandler) buildCommand(geminiDir string) string {
	if h.metadata.Hook.ScriptFile != "" {
		// Script mode: use absolute path to script file
		scriptPath := filepath.Join(geminiDir, "hooks", h.metadata.Asset.Name, h.metadata.Hook.ScriptFile)
		return scriptPath
	}

	// Command mode: join command and args
	cmd := h.metadata.Hook.Command
	if len(h.metadata.Hook.Args) > 0 {
		cmd = cmd + " " + strings.Join(h.metadata.Hook.Args, " ")
	}
	return cmd
}

// GetInstallPath returns the installation path relative to geminiDir
func (h *HookHandler) GetInstallPath() string {
	return filepath.Join(DirHooks, h.metadata.Asset.Name)
}

// containsFile checks if a filename exists in the file list
func containsFile(files []string, filename string) bool {
	for _, f := range files {
		if f == filename || filepath.Base(f) == filename {
			return true
		}
	}
	return false
}
