package claude_code

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestUninstallHooks tests that UninstallHooks removes skills-related hooks
func TestUninstallHooks(t *testing.T) {
	// Create isolated test environment
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	claudeDir := filepath.Join(homeDir, ".claude")

	t.Setenv("HOME", homeDir)

	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		t.Fatalf("Failed to create .claude directory: %v", err)
	}

	// Create settings.json with skills hooks installed
	settingsPath := filepath.Join(claudeDir, "settings.json")
	settings := map[string]any{
		"hooks": map[string]any{
			"SessionStart": []any{
				map[string]any{
					"hooks": []any{
						map[string]any{
							"type":    "command",
							"command": "skills install --hook-mode --client=claude-code",
						},
					},
				},
			},
			"PostToolUse": []any{
				map[string]any{
					"matcher": "Skill|Task|SlashCommand|mcp__.*",
					"hooks": []any{
						map[string]any{
							"type":    "command",
							"command": "skills report-usage --client=claude-code",
						},
					},
				},
			},
		},
	}
	data, _ := json.MarshalIndent(settings, "", "  ")
	if err := os.WriteFile(settingsPath, data, 0644); err != nil {
		t.Fatalf("Failed to write settings.json: %v", err)
	}

	// Verify hooks exist before uninstall
	data, _ = os.ReadFile(settingsPath)
	_ = json.Unmarshal(data, &settings)
	hooks := settings["hooks"].(map[string]any)
	if _, exists := hooks["SessionStart"]; !exists {
		t.Fatal("SessionStart hook not present before test")
	}
	if _, exists := hooks["PostToolUse"]; !exists {
		t.Fatal("PostToolUse hook not present before test")
	}

	// Run uninstallBootstrap
	if err := uninstallBootstrap(); err != nil {
		t.Fatalf("uninstallBootstrap failed: %v", err)
	}

	// Verify hooks were removed
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("Failed to read settings after uninstall: %v", err)
	}

	var settingsAfter map[string]any
	if err := json.Unmarshal(data, &settingsAfter); err != nil {
		t.Fatalf("Failed to parse settings after uninstall: %v", err)
	}

	// Hooks section should be empty or removed - settings should be {}
	if len(settingsAfter) != 0 {
		t.Errorf("Expected empty settings, got: %v", settingsAfter)
	}

	t.Log("Skills hooks successfully removed")
}

// TestUninstallHooksPreservesOtherHooks verifies custom hooks are not removed
func TestUninstallHooksPreservesOtherHooks(t *testing.T) {
	// Create isolated test environment
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	claudeDir := filepath.Join(homeDir, ".claude")

	t.Setenv("HOME", homeDir)

	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		t.Fatalf("Failed to create .claude directory: %v", err)
	}

	// Create settings.json with both custom and skills hooks
	settingsPath := filepath.Join(claudeDir, "settings.json")
	settings := map[string]any{
		"hooks": map[string]any{
			"SessionStart": []any{
				// Custom hook (should be preserved)
				map[string]any{
					"hooks": []any{
						map[string]any{
							"type":    "command",
							"command": "my-custom-startup-script",
						},
					},
				},
				// Skills hook (should be removed)
				map[string]any{
					"hooks": []any{
						map[string]any{
							"type":    "command",
							"command": "skills install --hook-mode --client=claude-code",
						},
					},
				},
			},
			"PostToolUse": []any{
				// Custom hook (should be preserved)
				map[string]any{
					"matcher": "Bash",
					"hooks": []any{
						map[string]any{
							"type":    "command",
							"command": "my-bash-logger",
						},
					},
				},
				// Skills hook (should be removed)
				map[string]any{
					"matcher": "Skill|Task|SlashCommand|mcp__.*",
					"hooks": []any{
						map[string]any{
							"type":    "command",
							"command": "skills report-usage --client=claude-code",
						},
					},
				},
			},
		},
	}
	data, _ := json.MarshalIndent(settings, "", "  ")
	if err := os.WriteFile(settingsPath, data, 0644); err != nil {
		t.Fatalf("Failed to write settings.json: %v", err)
	}

	// Run uninstallBootstrap
	if err := uninstallBootstrap(); err != nil {
		t.Fatalf("uninstallBootstrap failed: %v", err)
	}

	// Verify custom hooks are preserved
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("Failed to read settings after uninstall: %v", err)
	}

	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("Failed to parse settings after uninstall: %v", err)
	}

	hooks, ok := settings["hooks"].(map[string]any)
	if !ok {
		t.Fatal("Hooks section was completely removed")
	}

	// Check SessionStart - custom hook should remain
	sessionStart, ok := hooks["SessionStart"].([]any)
	if !ok || len(sessionStart) == 0 {
		t.Error("SessionStart section was removed but had custom hook")
	} else {
		foundCustom := false
		foundSkills := false
		for _, item := range sessionStart {
			if hookMap, ok := item.(map[string]any); ok {
				if hooksArray, ok := hookMap["hooks"].([]any); ok {
					for _, h := range hooksArray {
						if hMap, ok := h.(map[string]any); ok {
							cmd, _ := hMap["command"].(string)
							if cmd == "my-custom-startup-script" {
								foundCustom = true
							}
							if cmd == "skills install --hook-mode --client=claude-code" {
								foundSkills = true
							}
						}
					}
				}
			}
		}
		if !foundCustom {
			t.Error("Custom SessionStart hook was removed")
		}
		if foundSkills {
			t.Error("Skills SessionStart hook was not removed")
		}
	}

	// Check PostToolUse - custom hook should remain
	postToolUse, ok := hooks["PostToolUse"].([]any)
	if !ok || len(postToolUse) == 0 {
		t.Error("PostToolUse section was removed but had custom hook")
	} else {
		foundCustom := false
		foundSkills := false
		for _, item := range postToolUse {
			if hookMap, ok := item.(map[string]any); ok {
				if hooksArray, ok := hookMap["hooks"].([]any); ok {
					for _, h := range hooksArray {
						if hMap, ok := h.(map[string]any); ok {
							cmd, _ := hMap["command"].(string)
							if cmd == "my-bash-logger" {
								foundCustom = true
							}
							if cmd == "skills report-usage --client=claude-code" {
								foundSkills = true
							}
						}
					}
				}
			}
		}
		if !foundCustom {
			t.Error("Custom PostToolUse hook was removed")
		}
		if foundSkills {
			t.Error("Skills PostToolUse hook was not removed")
		}
	}

	t.Log("Custom hooks preserved, skills hooks removed")
}

// TestUninstallHooksNoSettingsFile tests that uninstall handles missing settings.json gracefully
func TestUninstallHooksNoSettingsFile(t *testing.T) {
	// Create isolated test environment with no settings.json
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	claudeDir := filepath.Join(homeDir, ".claude")

	t.Setenv("HOME", homeDir)

	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		t.Fatalf("Failed to create .claude directory: %v", err)
	}

	// Don't create settings.json - it shouldn't exist

	// Run uninstallBootstrap - should not error
	if err := uninstallBootstrap(); err != nil {
		t.Fatalf("uninstallBootstrap should not fail when settings.json doesn't exist: %v", err)
	}

	t.Log("Handled missing settings.json gracefully")
}

// TestUninstallHooksNoHooksSection tests uninstall when hooks section doesn't exist
func TestUninstallHooksNoHooksSection(t *testing.T) {
	// Create isolated test environment
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	claudeDir := filepath.Join(homeDir, ".claude")

	t.Setenv("HOME", homeDir)

	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		t.Fatalf("Failed to create .claude directory: %v", err)
	}

	// Create settings.json without hooks section
	settingsPath := filepath.Join(claudeDir, "settings.json")
	settings := map[string]any{
		"someSetting": "someValue",
	}
	data, _ := json.MarshalIndent(settings, "", "  ")
	if err := os.WriteFile(settingsPath, data, 0644); err != nil {
		t.Fatalf("Failed to write settings.json: %v", err)
	}

	// Run uninstallBootstrap - should not error
	if err := uninstallBootstrap(); err != nil {
		t.Fatalf("uninstallBootstrap should not fail when hooks section doesn't exist: %v", err)
	}

	// Verify settings file wasn't corrupted
	data, _ = os.ReadFile(settingsPath)
	_ = json.Unmarshal(data, &settings)
	if settings["someSetting"] != "someValue" {
		t.Error("Settings file was corrupted")
	}

	t.Log("Handled missing hooks section gracefully")
}
