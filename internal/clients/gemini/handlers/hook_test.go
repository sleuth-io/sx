package handlers

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/metadata"
)

func createTestZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, content := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatalf("Failed to create zip entry: %v", err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			t.Fatalf("Failed to write zip content: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Failed to close zip: %v", err)
	}
	return buf.Bytes()
}

func readSettingsJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to read settings.json: %v", err)
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("Failed to parse settings.json: %v", err)
	}
	return result
}

func TestHookHandler_ScriptFile_Install(t *testing.T) {
	targetBase := t.TempDir()
	geminiDir := filepath.Join(targetBase, ".gemini")
	os.MkdirAll(geminiDir, 0755)

	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name:    "lint-hook",
			Version: "1.0.0",
			Type:    asset.TypeHook,
		},
		Hook: &metadata.HookConfig{
			Event:      "pre-tool-use",
			ScriptFile: "hook.sh",
			Timeout:    30,
		},
	}

	zipData := createTestZip(t, map[string]string{
		"metadata.toml": `[asset]
name = "lint-hook"
version = "1.0.0"
type = "hook"
description = "A lint hook"

[hook]
event = "pre-tool-use"
script-file = "hook.sh"
timeout = 30
`,
		"hook.sh": "#!/bin/bash\necho lint",
	})

	handler := NewHookHandler(meta)
	if err := handler.Install(context.Background(), zipData, targetBase); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	// Verify hook.sh was extracted
	hookScript := filepath.Join(geminiDir, "hooks", "lint-hook", "hook.sh")
	if _, err := os.Stat(hookScript); os.IsNotExist(err) {
		t.Error("hook.sh should be extracted to hooks directory")
	}

	// Verify settings.json was updated
	settingsPath := filepath.Join(geminiDir, "settings.json")
	settings := readSettingsJSON(t, settingsPath)
	hooks, ok := settings["hooks"].(map[string]any)
	if !ok {
		t.Fatal("settings.json should have hooks section")
	}

	// pre-tool-use maps to PreToolUse in Gemini
	preToolUse, ok := hooks["PreToolUse"].([]any)
	if !ok || len(preToolUse) == 0 {
		t.Fatal("PreToolUse event should have entries")
	}

	matcher := preToolUse[0].(map[string]any)
	hooksList := matcher["hooks"].([]any)
	hookEntry := hooksList[0].(map[string]any)

	if hookEntry["name"] != "lint-hook" {
		t.Errorf("name = %v, want lint-hook", hookEntry["name"])
	}

	command, ok := hookEntry["command"].(string)
	if !ok || !strings.Contains(command, "hook.sh") {
		t.Errorf("command should contain hook.sh path, got: %v", hookEntry["command"])
	}
	if !filepath.IsAbs(command) {
		t.Errorf("command should be absolute path, got: %s", command)
	}
}

func TestHookHandler_Command_Install(t *testing.T) {
	targetBase := t.TempDir()
	geminiDir := filepath.Join(targetBase, ".gemini")
	os.MkdirAll(geminiDir, 0755)

	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name:    "cmd-hook",
			Version: "1.0.0",
			Type:    asset.TypeHook,
		},
		Hook: &metadata.HookConfig{
			Event:   "post-tool-use",
			Command: "npx",
			Args:    []string{"lint-check", "--fix"},
			Timeout: 10,
		},
	}

	// Command-only zip: no script file
	zipData := createTestZip(t, map[string]string{
		"metadata.toml": `[asset]
name = "cmd-hook"
version = "1.0.0"
type = "hook"
description = "Command hook"

[hook]
event = "post-tool-use"
command = "npx"
args = ["lint-check", "--fix"]
timeout = 10
`,
	})

	handler := NewHookHandler(meta)
	if err := handler.Install(context.Background(), zipData, targetBase); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	// Verify no files were extracted
	hookDir := filepath.Join(geminiDir, "hooks", "cmd-hook")
	if _, err := os.Stat(hookDir); !os.IsNotExist(err) {
		t.Error("Command-only hook should not create install directory")
	}

	// Verify settings.json was updated
	settings := readSettingsJSON(t, filepath.Join(geminiDir, "settings.json"))
	hooks := settings["hooks"].(map[string]any)

	// post-tool-use maps to AfterTool in Gemini
	afterTool, ok := hooks["AfterTool"].([]any)
	if !ok || len(afterTool) == 0 {
		t.Fatal("AfterTool event should have entries")
	}

	matcher := afterTool[0].(map[string]any)
	hooksList := matcher["hooks"].([]any)
	hookEntry := hooksList[0].(map[string]any)

	if hookEntry["command"] != "npx lint-check --fix" {
		t.Errorf("command = %v, want \"npx lint-check --fix\"", hookEntry["command"])
	}
}

func TestHookHandler_EventMapping(t *testing.T) {
	tests := []struct {
		canonical string
		native    string
	}{
		{"session-start", "SessionStart"},
		{"session-end", "SessionEnd"},
		{"pre-tool-use", "PreToolUse"},
		{"post-tool-use", "AfterTool"},
		{"post-tool-use-failure", "AfterTool"}, // Gemini doesn't distinguish
		{"user-prompt-submit", "UserPromptSubmit"},
		{"stop", "Stop"},
	}

	for _, tt := range tests {
		meta := &metadata.Metadata{
			Asset: metadata.Asset{Name: "test", Version: "1.0.0", Type: asset.TypeHook},
			Hook:  &metadata.HookConfig{Event: tt.canonical, Command: "echo test"},
		}
		handler := NewHookHandler(meta)
		native, supported := handler.mapEventToGemini()
		if !supported {
			t.Errorf("Event %q should be supported", tt.canonical)
		}
		if native != tt.native {
			t.Errorf("mapEventToGemini(%q) = %q, want %q", tt.canonical, native, tt.native)
		}
	}
}

func TestHookHandler_EventMapping_UnsupportedEvent(t *testing.T) {
	// Events not supported by Gemini
	unsupportedEvents := []string{
		"subagent-start",
		"subagent-stop",
		"pre-compact",
		"unknown-event",
	}

	for _, event := range unsupportedEvents {
		meta := &metadata.Metadata{
			Asset: metadata.Asset{Name: "test", Version: "1.0.0", Type: asset.TypeHook},
			Hook:  &metadata.HookConfig{Event: event, Command: "echo test"},
		}
		handler := NewHookHandler(meta)
		_, supported := handler.mapEventToGemini()
		if supported {
			t.Errorf("Event %q should not be supported by Gemini", event)
		}
	}
}

func TestHookHandler_EventMapping_GeminiOverride(t *testing.T) {
	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "test", Version: "1.0.0", Type: asset.TypeHook},
		Hook: &metadata.HookConfig{
			Event:   "pre-tool-use",
			Command: "echo test",
			Gemini:  map[string]any{"event": "CustomEvent"},
		},
	}
	handler := NewHookHandler(meta)
	native, supported := handler.mapEventToGemini()
	if !supported {
		t.Error("Should be supported with override")
	}
	if native != "CustomEvent" {
		t.Errorf("Should use override event, got %q", native)
	}
}

func TestHookHandler_Remove(t *testing.T) {
	targetBase := t.TempDir()
	geminiDir := filepath.Join(targetBase, ".gemini")
	os.MkdirAll(geminiDir, 0755)

	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "lint-hook", Version: "1.0.0", Type: asset.TypeHook},
		Hook:  &metadata.HookConfig{Event: "pre-tool-use", Command: "echo lint"},
	}

	// Pre-populate settings.json with the hook
	settings := map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []any{
				map[string]any{
					"hooks": []any{
						map[string]any{"name": "lint-hook", "type": "command", "command": "echo lint"},
						map[string]any{"name": "other-hook", "type": "command", "command": "echo other"},
					},
				},
			},
		},
	}
	data, _ := json.MarshalIndent(settings, "", "  ")
	os.WriteFile(filepath.Join(geminiDir, "settings.json"), data, 0644)

	handler := NewHookHandler(meta)
	if err := handler.Remove(context.Background(), targetBase); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	updated := readSettingsJSON(t, filepath.Join(geminiDir, "settings.json"))
	hooks := updated["hooks"].(map[string]any)
	preToolUse := hooks["PreToolUse"].([]any)
	matcher := preToolUse[0].(map[string]any)
	hooksList := matcher["hooks"].([]any)

	if len(hooksList) != 1 {
		t.Fatalf("Should have 1 remaining hook, got %d", len(hooksList))
	}

	remaining := hooksList[0].(map[string]any)
	if remaining["name"] != "other-hook" {
		t.Errorf("Wrong hook remaining: %v", remaining["name"])
	}
}

func TestHookHandler_VerifyInstalled_CommandMode(t *testing.T) {
	targetBase := t.TempDir()
	geminiDir := filepath.Join(targetBase, ".gemini")
	os.MkdirAll(geminiDir, 0755)

	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "cmd-hook", Version: "1.0.0", Type: asset.TypeHook},
		Hook:  &metadata.HookConfig{Event: "pre-tool-use", Command: "echo test"},
	}
	handler := NewHookHandler(meta)

	// Before install
	installed, _ := handler.VerifyInstalled(targetBase)
	if installed {
		t.Error("Should not be installed before setup")
	}

	// Write settings.json with hook
	settings := map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []any{
				map[string]any{
					"hooks": []any{
						map[string]any{"name": "cmd-hook", "type": "command", "command": "echo test"},
					},
				},
			},
		},
	}
	data, _ := json.MarshalIndent(settings, "", "  ")
	os.WriteFile(filepath.Join(geminiDir, "settings.json"), data, 0644)

	installed, msg := handler.VerifyInstalled(targetBase)
	if !installed {
		t.Errorf("Should be installed, got msg: %s", msg)
	}
}

func TestHookHandler_GlobalScope(t *testing.T) {
	// Test that for global scope (targetBase ends with .gemini),
	// hooks go directly to targetBase, not targetBase/.gemini
	targetBase := t.TempDir()
	geminiDir := filepath.Join(targetBase, ".gemini")
	os.MkdirAll(geminiDir, 0755)

	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name:    "global-hook",
			Version: "1.0.0",
			Type:    asset.TypeHook,
		},
		Hook: &metadata.HookConfig{
			Event:      "session-start",
			ScriptFile: "hook.sh",
		},
	}

	zipData := createTestZip(t, map[string]string{
		"metadata.toml": `[asset]
name = "global-hook"
version = "1.0.0"
type = "hook"

[hook]
event = "session-start"
script-file = "hook.sh"
`,
		"hook.sh": "#!/bin/bash\necho session",
	})

	handler := NewHookHandler(meta)
	// Install to ~/.gemini directly (global scope)
	if err := handler.Install(context.Background(), zipData, geminiDir); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	// Verify hook.sh was extracted to ~/.gemini/hooks/, not ~/.gemini/.gemini/hooks/
	correctPath := filepath.Join(geminiDir, "hooks", "global-hook", "hook.sh")
	wrongPath := filepath.Join(geminiDir, ".gemini", "hooks", "global-hook", "hook.sh")

	if _, err := os.Stat(correctPath); os.IsNotExist(err) {
		t.Errorf("hook.sh should be at %s", correctPath)
	}

	if _, err := os.Stat(wrongPath); !os.IsNotExist(err) {
		t.Errorf("hook.sh should NOT be at %s (double .gemini bug)", wrongPath)
	}
}
