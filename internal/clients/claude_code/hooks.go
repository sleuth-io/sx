package claude_code

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

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

	// Check if our hook already exists
	hookExists := false
	for _, item := range sessionStart {
		if hookMap, ok := item.(map[string]interface{}); ok {
			if hooksArray, ok := hookMap["hooks"].([]interface{}); ok {
				for _, h := range hooksArray {
					if hMap, ok := h.(map[string]interface{}); ok {
						if cmd, ok := hMap["command"].(string); ok && (cmd == "skills install --hook-mode" || cmd == "skills install" || cmd == "skills install --error-on-change") {
							hookExists = true
							break
						}
					}
				}
			}
		}
		if hookExists {
			break
		}
	}

	// Add hook if it doesn't exist
	if !hookExists {
		newHook := map[string]interface{}{
			"hooks": []interface{}{
				map[string]interface{}{
					"type":    "command",
					"command": "skills install --hook-mode",
				},
			},
		}
		sessionStart = append(sessionStart, newHook)
		hooks["SessionStart"] = sessionStart

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

		log.Info("hook installed", "hook", "SessionStart", "command", "skills install --hook-mode")
	}

	return nil
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

	// Check if our hook already exists
	hookExists := false
	for _, item := range postToolUse {
		if hookMap, ok := item.(map[string]interface{}); ok {
			if hooksArray, ok := hookMap["hooks"].([]interface{}); ok {
				for _, h := range hooksArray {
					if hMap, ok := h.(map[string]interface{}); ok {
						if cmd, ok := hMap["command"].(string); ok && cmd == "skills report-usage" {
							hookExists = true
							break
						}
					}
				}
			}
		}
		if hookExists {
			break
		}
	}

	// Add hook if it doesn't exist
	if !hookExists {
		newHook := map[string]interface{}{
			"matcher": "Skill|Task|SlashCommand|mcp__.*",
			"hooks": []interface{}{
				map[string]interface{}{
					"type":    "command",
					"command": "skills report-usage",
				},
			},
		}
		postToolUse = append(postToolUse, newHook)
		hooks["PostToolUse"] = postToolUse

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

		log.Info("hook installed", "hook", "PostToolUse", "command", "skills report-usage")
	}

	return nil
}
