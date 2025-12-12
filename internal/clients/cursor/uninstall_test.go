package cursor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sleuth-io/skills/internal/clients/cursor/handlers"
)

// TestUninstallHooks tests that UninstallHooks removes skills-related hooks
func TestUninstallHooks(t *testing.T) {
	// Create isolated test environment
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	cursorDir := filepath.Join(homeDir, ".cursor")

	t.Setenv("HOME", homeDir)

	if err := os.MkdirAll(cursorDir, 0755); err != nil {
		t.Fatalf("Failed to create .cursor directory: %v", err)
	}

	// Create hooks.json with skills hook installed
	hooksPath := filepath.Join(cursorDir, "hooks.json")
	hooksConfig := &handlers.HooksConfig{
		Version: 1,
		Hooks: map[string][]map[string]interface{}{
			"beforeSubmitPrompt": {
				{
					"command": "skills install --hook-mode --client=cursor",
				},
			},
		},
	}
	if err := handlers.WriteHooksJSON(hooksPath, hooksConfig); err != nil {
		t.Fatalf("Failed to write hooks.json: %v", err)
	}

	// Verify hook exists before uninstall
	hooksConfig, _ = handlers.ReadHooksJSON(hooksPath)
	if _, exists := hooksConfig.Hooks["beforeSubmitPrompt"]; !exists {
		t.Fatal("beforeSubmitPrompt hook not present before test")
	}

	// Create client and run UninstallHooks
	client := NewClient()
	if err := client.uninstallBeforeSubmitPromptHook(); err != nil {
		t.Fatalf("uninstallBeforeSubmitPromptHook failed: %v", err)
	}

	// Verify hook was removed
	hooksConfig, err := handlers.ReadHooksJSON(hooksPath)
	if err != nil {
		t.Fatalf("Failed to read hooks.json after uninstall: %v", err)
	}

	if hooks, exists := hooksConfig.Hooks["beforeSubmitPrompt"]; exists && len(hooks) > 0 {
		for _, hook := range hooks {
			if cmd, ok := hook["command"].(string); ok && cmd == "skills install --hook-mode --client=cursor" {
				t.Error("Skills hook was not removed")
			}
		}
	}

	t.Log("Skills hook successfully removed")
}

// TestUninstallHooksPreservesOtherHooks verifies custom hooks are not removed
func TestUninstallHooksPreservesOtherHooks(t *testing.T) {
	// Create isolated test environment
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	cursorDir := filepath.Join(homeDir, ".cursor")

	t.Setenv("HOME", homeDir)

	if err := os.MkdirAll(cursorDir, 0755); err != nil {
		t.Fatalf("Failed to create .cursor directory: %v", err)
	}

	// Create hooks.json with both custom and skills hooks
	hooksPath := filepath.Join(cursorDir, "hooks.json")
	hooksConfig := &handlers.HooksConfig{
		Version: 1,
		Hooks: map[string][]map[string]interface{}{
			"beforeSubmitPrompt": {
				// Custom hook (should be preserved)
				{
					"command": "my-custom-prompt-hook",
				},
				// Skills hook (should be removed)
				{
					"command": "skills install --hook-mode --client=cursor",
				},
			},
			"afterFileEdit": {
				// Custom hook in different event (should be preserved)
				{
					"command": "my-file-edit-hook",
				},
			},
		},
	}
	if err := handlers.WriteHooksJSON(hooksPath, hooksConfig); err != nil {
		t.Fatalf("Failed to write hooks.json: %v", err)
	}

	// Create client and run UninstallHooks
	client := NewClient()
	if err := client.uninstallBeforeSubmitPromptHook(); err != nil {
		t.Fatalf("uninstallBeforeSubmitPromptHook failed: %v", err)
	}

	// Verify custom hooks are preserved
	hooksConfig, err := handlers.ReadHooksJSON(hooksPath)
	if err != nil {
		t.Fatalf("Failed to read hooks.json after uninstall: %v", err)
	}

	// Check beforeSubmitPrompt - custom hook should remain
	beforeSubmit, exists := hooksConfig.Hooks["beforeSubmitPrompt"]
	if !exists || len(beforeSubmit) == 0 {
		t.Error("beforeSubmitPrompt section was removed but had custom hook")
	} else {
		foundCustom := false
		foundSkills := false
		for _, hook := range beforeSubmit {
			cmd, _ := hook["command"].(string)
			if cmd == "my-custom-prompt-hook" {
				foundCustom = true
			}
			if cmd == "skills install --hook-mode --client=cursor" {
				foundSkills = true
			}
		}
		if !foundCustom {
			t.Error("Custom beforeSubmitPrompt hook was removed")
		}
		if foundSkills {
			t.Error("Skills beforeSubmitPrompt hook was not removed")
		}
	}

	// Check afterFileEdit - should be untouched
	afterEdit, exists := hooksConfig.Hooks["afterFileEdit"]
	if !exists || len(afterEdit) == 0 {
		t.Error("afterFileEdit section was removed")
	} else {
		foundCustom := false
		for _, hook := range afterEdit {
			if cmd, _ := hook["command"].(string); cmd == "my-file-edit-hook" {
				foundCustom = true
			}
		}
		if !foundCustom {
			t.Error("Custom afterFileEdit hook was removed")
		}
	}

	t.Log("Custom hooks preserved, skills hooks removed")
}

// TestUninstallHooksNoHooksFile tests that uninstall handles missing hooks.json gracefully
func TestUninstallHooksNoHooksFile(t *testing.T) {
	// Create isolated test environment with no hooks.json
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	cursorDir := filepath.Join(homeDir, ".cursor")

	t.Setenv("HOME", homeDir)

	if err := os.MkdirAll(cursorDir, 0755); err != nil {
		t.Fatalf("Failed to create .cursor directory: %v", err)
	}

	// Don't create hooks.json - it shouldn't exist

	// Create client and run UninstallHooks - should not error
	client := NewClient()
	if err := client.uninstallBeforeSubmitPromptHook(); err != nil {
		t.Fatalf("uninstallBeforeSubmitPromptHook should not fail when hooks.json doesn't exist: %v", err)
	}

	t.Log("Handled missing hooks.json gracefully")
}

// TestUninstallHooksEmptyBeforeSubmitPrompt tests uninstall when beforeSubmitPrompt doesn't exist
func TestUninstallHooksEmptyBeforeSubmitPrompt(t *testing.T) {
	// Create isolated test environment
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	cursorDir := filepath.Join(homeDir, ".cursor")

	t.Setenv("HOME", homeDir)

	if err := os.MkdirAll(cursorDir, 0755); err != nil {
		t.Fatalf("Failed to create .cursor directory: %v", err)
	}

	// Create hooks.json without beforeSubmitPrompt
	hooksPath := filepath.Join(cursorDir, "hooks.json")
	hooksConfig := &handlers.HooksConfig{
		Version: 1,
		Hooks: map[string][]map[string]interface{}{
			"afterFileEdit": {
				{
					"command": "my-other-hook",
				},
			},
		},
	}
	if err := handlers.WriteHooksJSON(hooksPath, hooksConfig); err != nil {
		t.Fatalf("Failed to write hooks.json: %v", err)
	}

	// Create client and run UninstallHooks - should not error
	client := NewClient()
	if err := client.uninstallBeforeSubmitPromptHook(); err != nil {
		t.Fatalf("uninstallBeforeSubmitPromptHook should not fail when beforeSubmitPrompt doesn't exist: %v", err)
	}

	// Verify other hooks weren't affected
	hooksConfig, _ = handlers.ReadHooksJSON(hooksPath)
	if _, exists := hooksConfig.Hooks["afterFileEdit"]; !exists {
		t.Error("Other hooks were removed")
	}

	t.Log("Handled missing beforeSubmitPrompt section gracefully")
}

// TestUninstallHooksOnlySkillsHook tests that the entire beforeSubmitPrompt section
// is removed when it only contains the skills hook
func TestUninstallHooksOnlySkillsHook(t *testing.T) {
	// Create isolated test environment
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	cursorDir := filepath.Join(homeDir, ".cursor")

	t.Setenv("HOME", homeDir)

	if err := os.MkdirAll(cursorDir, 0755); err != nil {
		t.Fatalf("Failed to create .cursor directory: %v", err)
	}

	// Create hooks.json with only skills hook in beforeSubmitPrompt
	hooksPath := filepath.Join(cursorDir, "hooks.json")
	hooksConfig := &handlers.HooksConfig{
		Version: 1,
		Hooks: map[string][]map[string]interface{}{
			"beforeSubmitPrompt": {
				{
					"command": "skills install --hook-mode --client=cursor",
				},
			},
		},
	}
	if err := handlers.WriteHooksJSON(hooksPath, hooksConfig); err != nil {
		t.Fatalf("Failed to write hooks.json: %v", err)
	}

	// Create client and run UninstallHooks
	client := NewClient()
	if err := client.uninstallBeforeSubmitPromptHook(); err != nil {
		t.Fatalf("uninstallBeforeSubmitPromptHook failed: %v", err)
	}

	// Verify beforeSubmitPrompt section was removed entirely
	data, _ := os.ReadFile(hooksPath)
	var config map[string]interface{}
	_ = json.Unmarshal(data, &config)

	if hooks, ok := config["hooks"].(map[string]interface{}); ok {
		if _, exists := hooks["beforeSubmitPrompt"]; exists {
			t.Error("Empty beforeSubmitPrompt section should have been removed")
		}
	}

	t.Log("Empty beforeSubmitPrompt section removed correctly")
}
