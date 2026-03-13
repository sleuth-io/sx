package cline

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/sleuth-io/sx/internal/logger"
	"github.com/sleuth-io/sx/internal/utils"
)

// Hook event names for Cline
const (
	HookEventTaskStart   = "TaskStart"
	HookEventPostToolUse = "PostToolUse"
)

// sxHookMarker identifies hooks managed by sx
const sxHookMarker = "# sx-managed hook"

// getClineHooksDir returns the path to Cline's global hooks directory.
// Cline looks for global hooks in ~/Documents/Cline/Hooks/ (not ~/.cline/hooks/).
// See: https://docs.cline.bot/features/hooks
func getClineHooksDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(home, "Documents", "Cline", "Hooks"), nil
}

// installSessionHook installs the TaskStart hook for auto-updating assets
func installSessionHook() error {
	log := logger.Get()

	hooksDir, err := getClineHooksDir()
	if err != nil {
		return err
	}

	// Ensure hooks directory exists
	if err := utils.EnsureDir(hooksDir); err != nil {
		return fmt.Errorf("failed to create hooks directory: %w", err)
	}

	hookPath := filepath.Join(hooksDir, HookEventTaskStart)
	hookCommand := "sx install --hook-mode --client=cline"

	// Check if hook already exists and is managed by sx
	if existingContent, err := os.ReadFile(hookPath); err == nil {
		content := string(existingContent)
		if strings.Contains(content, sxHookMarker) {
			// Already installed by sx, check if command matches
			if strings.Contains(content, hookCommand) {
				log.Debug("TaskStart hook already installed", "path", hookPath)
				return nil
			}
			// Update existing sx hook
			log.Info("updating TaskStart hook", "path", hookPath)
		} else {
			// Hook exists but not managed by sx - don't overwrite
			log.Warn("TaskStart hook exists but not managed by sx, skipping", "path", hookPath)
			return nil
		}
	}

	// Write the hook script
	script := generateHookScript(hookCommand)
	if err := os.WriteFile(hookPath, []byte(script), 0755); err != nil {
		return fmt.Errorf("failed to write TaskStart hook: %w", err)
	}

	log.Info("installed TaskStart hook", "path", hookPath)
	return nil
}

// installAnalyticsHook installs the PostToolUse hook for usage tracking
func installAnalyticsHook() error {
	log := logger.Get()

	hooksDir, err := getClineHooksDir()
	if err != nil {
		return err
	}

	// Ensure hooks directory exists
	if err := utils.EnsureDir(hooksDir); err != nil {
		return fmt.Errorf("failed to create hooks directory: %w", err)
	}

	hookPath := filepath.Join(hooksDir, HookEventPostToolUse)
	hookCommand := "sx report-usage --client=cline"

	// Check if hook already exists and is managed by sx
	if existingContent, err := os.ReadFile(hookPath); err == nil {
		content := string(existingContent)
		if strings.Contains(content, sxHookMarker) {
			// Already installed by sx, check if command matches
			if strings.Contains(content, hookCommand) {
				log.Debug("PostToolUse hook already installed", "path", hookPath)
				return nil
			}
			// Update existing sx hook
			log.Info("updating PostToolUse hook", "path", hookPath)
		} else {
			// Hook exists but not managed by sx - don't overwrite
			log.Warn("PostToolUse hook exists but not managed by sx, skipping", "path", hookPath)
			return nil
		}
	}

	// Write the hook script
	script := generateHookScript(hookCommand)
	if err := os.WriteFile(hookPath, []byte(script), 0755); err != nil {
		return fmt.Errorf("failed to write PostToolUse hook: %w", err)
	}

	log.Info("installed PostToolUse hook", "path", hookPath)
	return nil
}

// uninstallSessionHook removes the TaskStart hook if it was installed by sx
func uninstallSessionHook() error {
	log := logger.Get()

	hooksDir, err := getClineHooksDir()
	if err != nil {
		return err
	}

	hookPath := filepath.Join(hooksDir, HookEventTaskStart)

	// Check if hook exists and is managed by sx
	if existingContent, err := os.ReadFile(hookPath); err == nil {
		if !strings.Contains(string(existingContent), sxHookMarker) {
			// Not managed by sx, don't remove
			log.Debug("TaskStart hook not managed by sx, skipping removal", "path", hookPath)
			return nil
		}
	} else if os.IsNotExist(err) {
		// Already removed
		return nil
	}

	if err := os.Remove(hookPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove TaskStart hook: %w", err)
	}

	log.Info("removed TaskStart hook", "path", hookPath)
	return nil
}

// uninstallAnalyticsHook removes the PostToolUse hook if it was installed by sx
func uninstallAnalyticsHook() error {
	log := logger.Get()

	hooksDir, err := getClineHooksDir()
	if err != nil {
		return err
	}

	hookPath := filepath.Join(hooksDir, HookEventPostToolUse)

	// Check if hook exists and is managed by sx
	if existingContent, err := os.ReadFile(hookPath); err == nil {
		if !strings.Contains(string(existingContent), sxHookMarker) {
			// Not managed by sx, don't remove
			log.Debug("PostToolUse hook not managed by sx, skipping removal", "path", hookPath)
			return nil
		}
	} else if os.IsNotExist(err) {
		// Already removed
		return nil
	}

	if err := os.Remove(hookPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove PostToolUse hook: %w", err)
	}

	log.Info("removed PostToolUse hook", "path", hookPath)
	return nil
}

// generateHookScript creates the hook script content for the given command
func generateHookScript(command string) string {
	if runtime.GOOS == "windows" {
		// PowerShell script for Windows
		return fmt.Sprintf(`%s
# Cline hook script - do not edit manually
%s
`, sxHookMarker, command)
	}

	// Shell script for Unix-like systems
	return fmt.Sprintf(`#!/bin/sh
%s
# Cline hook script - do not edit manually
%s
`, sxHookMarker, command)
}
