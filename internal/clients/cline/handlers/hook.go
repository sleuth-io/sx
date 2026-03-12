package handlers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/handlers/dirasset"
	"github.com/sleuth-io/sx/internal/handlers/hook"
	"github.com/sleuth-io/sx/internal/metadata"
	"github.com/sleuth-io/sx/internal/utils"
)

var hookOps = dirasset.NewOperations("hooks", &asset.TypeHook)

// clineEventMap maps canonical hook events to Cline native event names
var clineEventMap = map[string]string{
	"session-start":         "TaskStart",
	"session-end":           "TaskEnd",
	"pre-tool-use":          "PreToolUse",
	"post-tool-use":         "PostToolUse",
	"post-tool-use-failure": "PostToolUseFailure",
	"user-prompt-submit":    "UserPromptSubmit",
}

// assetMarker identifies hook scripts managed by sx asset system
const assetMarker = "# sx-asset"

// HookHandler handles hook asset installation for Cline
type HookHandler struct {
	metadata *metadata.Metadata
	zipFiles []string // populated during Install to resolve args to absolute paths
}

// NewHookHandler creates a new hook handler
func NewHookHandler(meta *metadata.Metadata) *HookHandler {
	return &HookHandler{metadata: meta}
}

// Install installs a hook asset to Cline by extracting scripts and creating hook files
func (h *HookHandler) Install(ctx context.Context, zipData []byte, targetBase string) error {
	if err := hook.ValidateZipForHook(zipData); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	h.zipFiles = hook.CacheZipFiles(zipData)

	// Extract hook files if there are any to extract
	if hook.HasExtractableFiles(zipData) {
		if err := hookOps.Install(ctx, zipData, targetBase, h.metadata.Asset.Name); err != nil {
			return err
		}
	}

	// Create the hook script in Cline's hooks directory
	if err := h.createHookScript(targetBase); err != nil {
		return fmt.Errorf("failed to create hook script: %w", err)
	}

	return nil
}

// Remove uninstalls a hook asset from Cline
func (h *HookHandler) Remove(ctx context.Context, targetBase string) error {
	// Remove hook script from Cline's hooks directory
	if err := h.removeHookScript(); err != nil {
		return fmt.Errorf("failed to remove hook script: %w", err)
	}

	// Remove extracted files if they exist
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

// VerifyInstalled checks if the hook is properly installed
func (h *HookHandler) VerifyInstalled(targetBase string) (bool, string) {
	// Check if hook script exists in Cline's hooks directory
	hooksDir, err := getClineHooksDir()
	if err != nil {
		return false, "failed to get hooks directory: " + err.Error()
	}

	clineEvent, supported := h.mapEventToCline()
	if !supported {
		return false, fmt.Sprintf("hook event %q not supported for Cline", h.metadata.Hook.Event)
	}

	hookPath := filepath.Join(hooksDir, clineEvent)
	content, err := os.ReadFile(hookPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, "hook script not found"
		}
		return false, "failed to read hook script: " + err.Error()
	}

	// Check if this hook was installed by this asset
	if !strings.Contains(string(content), assetMarker+":"+h.metadata.Asset.Name) {
		return false, "hook exists but not managed by this asset"
	}

	// Also verify extracted files if script-file mode was used
	if h.metadata.Hook != nil && h.metadata.Hook.ScriptFile != "" {
		installDir := filepath.Join(targetBase, "hooks", h.metadata.Asset.Name)
		if utils.IsDirectory(installDir) {
			return hookOps.VerifyInstalled(targetBase, h.metadata.Asset.Name, h.metadata.Asset.Version)
		}
	}

	return true, "installed"
}

// mapEventToCline maps a canonical event name to Cline native event.
// If the hook has a [hook.cline] event override, that is returned instead.
func (h *HookHandler) mapEventToCline() (string, bool) {
	return hook.MapEvent(h.metadata.Hook.Event, clineEventMap, h.metadata.Hook.Cline)
}

// createHookScript creates the hook script in Cline's hooks directory
func (h *HookHandler) createHookScript(targetBase string) error {
	hooksDir, err := getClineHooksDir()
	if err != nil {
		return err
	}

	// Ensure hooks directory exists
	if err := utils.EnsureDir(hooksDir); err != nil {
		return fmt.Errorf("failed to create hooks directory: %w", err)
	}

	clineEvent, supported := h.mapEventToCline()
	if !supported {
		return fmt.Errorf("hook event %q not supported for Cline", h.metadata.Hook.Event)
	}

	hookPath := filepath.Join(hooksDir, clineEvent)

	// Resolve command to execute
	installDir := filepath.Join(targetBase, "hooks", h.metadata.Asset.Name)
	resolved := hook.ResolveCommand(h.metadata.Hook, installDir, h.zipFiles)

	// Generate script content
	script := h.generateScript(resolved.Command)

	// Write the hook script
	if err := os.WriteFile(hookPath, []byte(script), 0755); err != nil {
		return fmt.Errorf("failed to write hook script: %w", err)
	}

	return nil
}

// removeHookScript removes the hook script from Cline's hooks directory
func (h *HookHandler) removeHookScript() error {
	hooksDir, err := getClineHooksDir()
	if err != nil {
		return err
	}

	clineEvent, supported := h.mapEventToCline()
	if !supported {
		// If event is not supported, nothing to remove
		return nil
	}

	hookPath := filepath.Join(hooksDir, clineEvent)

	// Check if hook exists and is managed by this asset
	content, err := os.ReadFile(hookPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // Already removed
		}
		return fmt.Errorf("failed to read hook script: %w", err)
	}

	// Only remove if this asset created the hook
	if !strings.Contains(string(content), assetMarker+":"+h.metadata.Asset.Name) {
		return nil // Not our hook
	}

	if err := os.Remove(hookPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove hook script: %w", err)
	}

	return nil
}

// generateScript creates the hook script content for the given command
func (h *HookHandler) generateScript(command string) string {
	assetHeader := fmt.Sprintf("%s:%s", assetMarker, h.metadata.Asset.Name)

	if runtime.GOOS == "windows" {
		// PowerShell script for Windows
		return fmt.Sprintf(`%s
# Cline hook script - installed by sx
%s
`, assetHeader, command)
	}

	// Shell script for Unix-like systems
	return fmt.Sprintf(`#!/bin/sh
%s
# Cline hook script - installed by sx
%s
`, assetHeader, command)
}

// getClineHooksDir returns the path to Cline's global hooks directory.
// Cline looks for global hooks in ~/Documents/Cline/Hooks/ (not ~/.cline/hooks/).
// See: https://docs.cline.bot/features/hooks
// This is a variable to allow testing to override it.
var getClineHooksDir = func() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(home, "Documents", "Cline", "Hooks"), nil
}
