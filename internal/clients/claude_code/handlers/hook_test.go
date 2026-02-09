package handlers

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/metadata"
)

func TestHookHandler_ScriptFile_Install(t *testing.T) {
	targetBase := t.TempDir()

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
	hookScript := filepath.Join(targetBase, "hooks", "lint-hook", "hook.sh")
	if _, err := os.Stat(hookScript); os.IsNotExist(err) {
		t.Error("hook.sh should be extracted to hooks directory")
	}

	// Verify settings.json was updated
	settingsPath := filepath.Join(targetBase, "settings.json")
	settings := readJSON(t, settingsPath)
	hooks, ok := settings["hooks"].(map[string]any)
	if !ok {
		t.Fatal("settings.json should have hooks section")
	}

	// pre-tool-use maps to PreToolUse
	preToolUse, ok := hooks["PreToolUse"].([]any)
	if !ok || len(preToolUse) == 0 {
		t.Fatal("PreToolUse event should have entries")
	}

	hookEntry := preToolUse[0].(map[string]any)
	if hookEntry["_artifact"] != "lint-hook" {
		t.Errorf("_artifact = %v, want lint-hook", hookEntry["_artifact"])
	}

	command, ok := hookEntry["command"].(string)
	if !ok || !strings.Contains(command, "hook.sh") {
		t.Errorf("command should contain hook.sh path, got: %v", hookEntry["command"])
	}
	if !filepath.IsAbs(command) {
		t.Errorf("command should be absolute path, got: %s", command)
	}
	if hookEntry["timeout"] != float64(30) {
		t.Errorf("timeout = %v, want 30", hookEntry["timeout"])
	}
}

func TestHookHandler_Command_Install(t *testing.T) {
	targetBase := t.TempDir()

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
	hookDir := filepath.Join(targetBase, "hooks", "cmd-hook")
	if _, err := os.Stat(hookDir); !os.IsNotExist(err) {
		t.Error("Command-only hook should not create install directory")
	}

	// Verify settings.json was updated
	settings := readJSON(t, filepath.Join(targetBase, "settings.json"))
	hooks := settings["hooks"].(map[string]any)

	// post-tool-use maps to PostToolUse
	postToolUse, ok := hooks["PostToolUse"].([]any)
	if !ok || len(postToolUse) == 0 {
		t.Fatal("PostToolUse event should have entries")
	}

	hookEntry := postToolUse[0].(map[string]any)
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
		{"post-tool-use", "PostToolUse"},
		{"post-tool-use-failure", "PostToolUseFailure"},
		{"user-prompt-submit", "UserPromptSubmit"},
		{"stop", "Stop"},
		{"subagent-start", "SubagentStart"},
		{"subagent-stop", "SubagentStop"},
		{"pre-compact", "PreCompact"},
	}

	for _, tt := range tests {
		meta := &metadata.Metadata{
			Asset: metadata.Asset{Name: "test", Version: "1.0.0", Type: asset.TypeHook},
			Hook:  &metadata.HookConfig{Event: tt.canonical, Command: "echo test"},
		}
		handler := NewHookHandler(meta)
		native, supported := handler.mapEventToClaudeCode()
		if !supported {
			t.Errorf("Event %q should be supported", tt.canonical)
		}
		if native != tt.native {
			t.Errorf("mapEventToClaudeCode(%q) = %q, want %q", tt.canonical, native, tt.native)
		}
	}
}

func TestHookHandler_EventMapping_UnsupportedEvent(t *testing.T) {
	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "test", Version: "1.0.0", Type: asset.TypeHook},
		Hook:  &metadata.HookConfig{Event: "unknown-event", Command: "echo test"},
	}
	handler := NewHookHandler(meta)
	_, supported := handler.mapEventToClaudeCode()
	if supported {
		t.Error("Unknown event should not be supported")
	}
}

func TestHookHandler_EventMapping_ClaudeCodeOverride(t *testing.T) {
	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "test", Version: "1.0.0", Type: asset.TypeHook},
		Hook: &metadata.HookConfig{
			Event:      "pre-tool-use",
			Command:    "echo test",
			ClaudeCode: map[string]any{"event": "CustomEvent"},
		},
	}
	handler := NewHookHandler(meta)
	native, supported := handler.mapEventToClaudeCode()
	if !supported {
		t.Error("Should be supported with override")
	}
	if native != "CustomEvent" {
		t.Errorf("Should use override event, got %q", native)
	}
}

func TestHookHandler_Remove(t *testing.T) {
	targetBase := t.TempDir()

	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "lint-hook", Version: "1.0.0", Type: asset.TypeHook},
		Hook:  &metadata.HookConfig{Event: "pre-tool-use", Command: "echo lint"},
	}

	// Pre-populate settings.json with the hook
	settings := map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []any{
				map[string]any{"_artifact": "lint-hook", "command": "echo lint"},
				map[string]any{"_artifact": "other-hook", "command": "echo other"},
			},
		},
	}
	data, _ := json.MarshalIndent(settings, "", "  ")
	os.WriteFile(filepath.Join(targetBase, "settings.json"), data, 0644)

	handler := NewHookHandler(meta)
	if err := handler.Remove(context.Background(), targetBase); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	updated := readJSON(t, filepath.Join(targetBase, "settings.json"))
	hooks := updated["hooks"].(map[string]any)
	preToolUse := hooks["PreToolUse"].([]any)

	if len(preToolUse) != 1 {
		t.Fatalf("Should have 1 remaining hook, got %d", len(preToolUse))
	}

	remaining := preToolUse[0].(map[string]any)
	if remaining["_artifact"] != "other-hook" {
		t.Errorf("Wrong hook remaining: %v", remaining["_artifact"])
	}
}

func TestHookHandler_VerifyInstalled_CommandMode(t *testing.T) {
	targetBase := t.TempDir()

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
				map[string]any{"_artifact": "cmd-hook", "command": "echo test"},
			},
		},
	}
	data, _ := json.MarshalIndent(settings, "", "  ")
	os.WriteFile(filepath.Join(targetBase, "settings.json"), data, 0644)

	installed, msg := handler.VerifyInstalled(targetBase)
	if !installed {
		t.Errorf("Should be installed, got msg: %s", msg)
	}
}

func TestHookHandler_BuildConfig_WithMatcher(t *testing.T) {
	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "test", Version: "1.0.0", Type: asset.TypeHook},
		Hook: &metadata.HookConfig{
			Event:   "pre-tool-use",
			Command: "echo lint",
			Matcher: "Edit|Write",
			Timeout: 30,
		},
	}
	handler := NewHookHandler(meta)
	config := handler.buildHookConfig(t.TempDir())

	if config["matcher"] != "Edit|Write" {
		t.Errorf("matcher = %v, want \"Edit|Write\"", config["matcher"])
	}
	if config["timeout"] != 30 {
		t.Errorf("timeout = %v, want 30", config["timeout"])
	}
}

func TestHookHandler_BuildConfig_MergesClaudeCodeOverrides(t *testing.T) {
	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "test", Version: "1.0.0", Type: asset.TypeHook},
		Hook: &metadata.HookConfig{
			Event:      "pre-tool-use",
			Command:    "echo lint",
			ClaudeCode: map[string]any{"async": true, "event": "ShouldNotAppear"},
		},
	}
	handler := NewHookHandler(meta)
	config := handler.buildHookConfig(t.TempDir())

	if config["async"] != true {
		t.Error("async should be merged from ClaudeCode overrides")
	}
	// event should NOT be in the config (it's handled by mapEventToClaudeCode)
	if _, exists := config["event"]; exists {
		t.Error("event should not be merged into hook config")
	}
}
