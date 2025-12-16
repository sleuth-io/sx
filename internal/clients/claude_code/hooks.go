package claude_code

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sleuth-io/skills/internal/logger"
)

// installHooks installs system hooks for Claude Code (auto-update and usage tracking).
// This is different from installing hook artifacts - these are the skills CLI's own hooks.
func installHooks() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}
	claudeDir := filepath.Join(home, ".claude")

	log := logger.Get()

	// Install usage reporting hook
	if err := installUsageReportingHook(claudeDir); err != nil {
		log.Error("failed to install usage reporting hook", "error", err)
		return fmt.Errorf("failed to install usage reporting hook: %w", err)
	}

	// Install session start hook for auto-update
	if err := installSessionStartHook(claudeDir); err != nil {
		log.Error("failed to install session start hook", "error", err)
		return fmt.Errorf("failed to install session start hook: %w", err)
	}

	return nil
}

// installSessionStartHook installs the SessionStart hook for auto-updating artifacts
func installSessionStartHook(claudeDir string) error {
	settingsPath := filepath.Join(claudeDir, "settings.json")
	log := logger.Get()

	// Read existing settings or create new
	var settings map[string]interface{}
	if data, err := os.ReadFile(settingsPath); err == nil {
		if err := json.Unmarshal(data, &settings); err != nil {
			log.Error("failed to parse settings.json for SessionStart hook", "error", err)
			return fmt.Errorf("failed to parse settings.json: %w", err)
		}
	} else {
		settings = make(map[string]interface{})
	}

	// Get or create hooks section
	hooks, ok := settings["hooks"].(map[string]interface{})
	if !ok {
		hooks = make(map[string]interface{})
		settings["hooks"] = hooks
	}

	// Get or create SessionStart array
	sessionStart, ok := hooks["SessionStart"].([]interface{})
	if !ok {
		sessionStart = []interface{}{}
	}

	hookCommand := "sx install --hook-mode --client=claude-code"

	// First, check if exact hook command already exists
	exactMatch := false
	var oldHookRef map[string]interface{}
	for _, item := range sessionStart {
		if hookMap, ok := item.(map[string]interface{}); ok {
			if hooksArray, ok := hookMap["hooks"].([]interface{}); ok {
				for _, h := range hooksArray {
					if hMap, ok := h.(map[string]interface{}); ok {
						if cmd, ok := hMap["command"].(string); ok {
							if cmd == hookCommand {
								exactMatch = true
								break
							}
							if strings.HasPrefix(cmd, "sx install") || strings.HasPrefix(cmd, "skills install") {
								oldHookRef = hMap // Remember for updating
							}
						}
					}
				}
			}
		}
		if exactMatch {
			break
		}
	}

	// Already have exact match, nothing to do
	if exactMatch {
		return nil
	}

	// Get current working directory for context logging
	cwd, _ := os.Getwd()

	// Update old hook if found, otherwise add new
	if oldHookRef != nil {
		oldHookRef["command"] = hookCommand
		log.Info("hook updated", "hook", "SessionStart", "command", hookCommand, "cwd", cwd)
	} else {
		newHook := map[string]interface{}{
			"hooks": []interface{}{
				map[string]interface{}{
					"type":    "command",
					"command": hookCommand,
				},
			},
		}
		sessionStart = append(sessionStart, newHook)
		hooks["SessionStart"] = sessionStart
		log.Info("hook installed", "hook", "SessionStart", "command", hookCommand, "cwd", cwd)
	}

	// Write back to file
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		log.Error("failed to marshal settings for SessionStart hook", "error", err)
		return fmt.Errorf("failed to marshal settings: %w", err)
	}

	if err := os.WriteFile(settingsPath, data, 0644); err != nil {
		log.Error("failed to write settings.json for SessionStart hook", "error", err, "path", settingsPath)
		return fmt.Errorf("failed to write settings.json: %w", err)
	}

	return nil
}

// uninstallHooks removes system hooks for Claude Code.
// This removes the SessionStart hook (auto-install) and PostToolUse hook (usage tracking).
func uninstallHooks() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}
	claudeDir := filepath.Join(home, ".claude")
	settingsPath := filepath.Join(claudeDir, "settings.json")

	log := logger.Get()

	// Read existing settings
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		if os.IsNotExist(err) {
			// No settings file, nothing to uninstall
			return nil
		}
		return fmt.Errorf("failed to read settings.json: %w", err)
	}

	var settings map[string]interface{}
	if err := json.Unmarshal(data, &settings); err != nil {
		return fmt.Errorf("failed to parse settings.json: %w", err)
	}

	hooks, ok := settings["hooks"].(map[string]interface{})
	if !ok {
		// No hooks section, nothing to uninstall
		return nil
	}

	modified := false

	// Remove our SessionStart hook (check both sx and legacy skills commands)
	if sessionStart, ok := hooks["SessionStart"].([]interface{}); ok {
		filtered := removeSxHooks(sessionStart, "sx install", "skills install")
		if len(filtered) != len(sessionStart) {
			modified = true
			if len(filtered) == 0 {
				delete(hooks, "SessionStart")
			} else {
				hooks["SessionStart"] = filtered
			}
			log.Info("hook removed", "hook", "SessionStart")
		}
	}

	// Remove our PostToolUse hook (check both sx and legacy skills commands)
	if postToolUse, ok := hooks["PostToolUse"].([]interface{}); ok {
		filtered := removeSxHooks(postToolUse, "sx report-usage", "skills report-usage")
		if len(filtered) != len(postToolUse) {
			modified = true
			if len(filtered) == 0 {
				delete(hooks, "PostToolUse")
			} else {
				hooks["PostToolUse"] = filtered
			}
			log.Info("hook removed", "hook", "PostToolUse")
		}
	}

	// Remove empty hooks section
	if len(hooks) == 0 {
		delete(settings, "hooks")
	}

	if !modified {
		return nil
	}

	// Write back to file
	data, err = json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal settings: %w", err)
	}

	if err := os.WriteFile(settingsPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write settings.json: %w", err)
	}

	return nil
}

// removeSxHooks filters out hooks whose command starts with any of the given prefixes
func removeSxHooks(hooks []interface{}, commandPrefixes ...string) []interface{} {
	var filtered []interface{}
	for _, item := range hooks {
		hookMap, ok := item.(map[string]interface{})
		if !ok {
			filtered = append(filtered, item)
			continue
		}

		hooksArray, ok := hookMap["hooks"].([]interface{})
		if !ok {
			filtered = append(filtered, item)
			continue
		}

		// Check if this hook entry contains our command
		hasSxCommand := false
		for _, h := range hooksArray {
			hMap, ok := h.(map[string]interface{})
			if !ok {
				continue
			}
			cmd, ok := hMap["command"].(string)
			if !ok {
				continue
			}
			for _, prefix := range commandPrefixes {
				if strings.HasPrefix(cmd, prefix) {
					hasSxCommand = true
					break
				}
			}
			if hasSxCommand {
				break
			}
		}

		if !hasSxCommand {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

// installUsageReportingHook installs the PostToolUse hook for usage tracking
func installUsageReportingHook(claudeDir string) error {
	settingsPath := filepath.Join(claudeDir, "settings.json")
	log := logger.Get()

	// Read existing settings or create new
	var settings map[string]interface{}
	if data, err := os.ReadFile(settingsPath); err == nil {
		if err := json.Unmarshal(data, &settings); err != nil {
			log.Error("failed to parse settings.json for PostToolUse hook", "error", err)
			return fmt.Errorf("failed to parse settings.json: %w", err)
		}
	} else {
		settings = make(map[string]interface{})
	}

	// Get or create hooks section
	hooks, ok := settings["hooks"].(map[string]interface{})
	if !ok {
		hooks = make(map[string]interface{})
		settings["hooks"] = hooks
	}

	// Get or create PostToolUse array
	postToolUse, ok := hooks["PostToolUse"].([]interface{})
	if !ok {
		postToolUse = []interface{}{}
	}

	hookCommand := "sx report-usage --client=claude-code"

	// Check if our hook already exists (check for both old and new command formats)
	hookExists := false
	var oldHookRef map[string]interface{}
	for _, item := range postToolUse {
		if hookMap, ok := item.(map[string]interface{}); ok {
			if hooksArray, ok := hookMap["hooks"].([]interface{}); ok {
				for _, h := range hooksArray {
					if hMap, ok := h.(map[string]interface{}); ok {
						if cmd, ok := hMap["command"].(string); ok {
							if cmd == hookCommand {
								hookExists = true
								break
							}
							if cmd == "skills report-usage" || cmd == "sx report-usage" || cmd == "skills report-usage --client=claude-code" {
								oldHookRef = hMap // Remember for updating
							}
						}
					}
				}
			}
		}
		if hookExists {
			break
		}
	}

	// Already have exact match, nothing to do
	if hookExists {
		return nil
	}

	// Update old hook if found, otherwise add new
	if oldHookRef != nil {
		oldHookRef["command"] = hookCommand
		log.Info("hook updated", "hook", "PostToolUse", "command", hookCommand)
	} else {
		newHook := map[string]interface{}{
			"matcher": "Skill|Task|SlashCommand|mcp__.*",
			"hooks": []interface{}{
				map[string]interface{}{
					"type":    "command",
					"command": hookCommand,
				},
			},
		}
		postToolUse = append(postToolUse, newHook)
		hooks["PostToolUse"] = postToolUse
		log.Info("hook installed", "hook", "PostToolUse", "command", hookCommand)
	}

	// Write back to file
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		log.Error("failed to marshal settings for PostToolUse hook", "error", err)
		return fmt.Errorf("failed to marshal settings: %w", err)
	}

	if err := os.WriteFile(settingsPath, data, 0644); err != nil {
		log.Error("failed to write settings.json for PostToolUse hook", "error", err, "path", settingsPath)
		return fmt.Errorf("failed to write settings.json: %w", err)
	}

	return nil
}
