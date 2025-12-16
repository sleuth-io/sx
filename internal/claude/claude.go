package claude

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sleuth-io/skills/internal/logger"
)

// Output is an interface for printing messages during hook installation
type Output interface {
	Println(string)
	PrintfErr(string, ...interface{})
}

// GetClaudeDir returns the Claude Code directory (~/.claude)
func GetClaudeDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(home, ".claude"), nil
}

// InstallHooks installs Claude Code hooks (usage tracking and auto-update)
func InstallHooks(out Output) error {
	claudeDir, err := GetClaudeDir()
	if err != nil {
		return fmt.Errorf("failed to get Claude directory: %w", err)
	}

	// Install usage reporting hook
	if err := installUsageReportingHook(claudeDir, out); err != nil {
		return fmt.Errorf("failed to install usage reporting hook: %w", err)
	}

	// Install session start hook for auto-update
	if err := installSessionStartHook(claudeDir, out); err != nil {
		return fmt.Errorf("failed to install session start hook: %w", err)
	}

	return nil
}

// installSessionStartHook installs the SessionStart hook for auto-updating artifacts
func installSessionStartHook(claudeDir string, out Output) error {
	settingsPath := filepath.Join(claudeDir, "settings.json")

	// Read existing settings or create new
	var settings map[string]interface{}
	if data, err := os.ReadFile(settingsPath); err == nil {
		if err := json.Unmarshal(data, &settings); err != nil {
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
						if cmd, ok := hMap["command"].(string); ok && (cmd == "sx install --hook-mode" || cmd == "sx install" || cmd == "sx install --error-on-change" || cmd == "skills install --hook-mode" || cmd == "skills install" || cmd == "skills install --error-on-change") {
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
					"command": "sx install --hook-mode",
				},
			},
		}
		sessionStart = append(sessionStart, newHook)
		hooks["SessionStart"] = sessionStart

		// Write back to file
		data, err := json.MarshalIndent(settings, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal settings: %w", err)
		}

		if err := os.WriteFile(settingsPath, data, 0644); err != nil {
			return fmt.Errorf("failed to write settings.json: %w", err)
		}

		log := logger.Get()
		log.Info("hook installed", "hook", "SessionStart", "command", "sx install --hook-mode")
		out.Println("\n✓ Installed auto-update hook to ~/.claude/settings.json")
	}

	return nil
}

// installUsageReportingHook installs the PostToolUse hook for usage tracking
func installUsageReportingHook(claudeDir string, out Output) error {
	settingsPath := filepath.Join(claudeDir, "settings.json")

	// Read existing settings or create new
	var settings map[string]interface{}
	if data, err := os.ReadFile(settingsPath); err == nil {
		if err := json.Unmarshal(data, &settings); err != nil {
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
							if cmd == hookCommand || cmd == "skills report-usage --client=claude-code" {
								hookExists = true
								break
							}
							if cmd == "skills report-usage" || cmd == "sx report-usage" {
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

	log := logger.Get()

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
		return fmt.Errorf("failed to marshal settings: %w", err)
	}

	if err := os.WriteFile(settingsPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write settings.json: %w", err)
	}

	out.Println("\n✓ Installed usage reporting hook to ~/.claude/settings.json")

	return nil
}
