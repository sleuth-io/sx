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

func TestCursorHookHandler_ScriptFile_Install(t *testing.T) {
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
description = "Lint hook"

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
		t.Error("hook.sh should be extracted")
	}

	// Verify hooks.json was updated
	hooksJSONPath := filepath.Join(targetBase, "hooks.json")
	config := readJSON(t, hooksJSONPath)
	hooks := config["hooks"].(map[string]any)

	// pre-tool-use maps to preToolUse for Cursor
	preToolUse, ok := hooks["preToolUse"].([]any)
	if !ok || len(preToolUse) == 0 {
		t.Fatal("preToolUse should have entries")
	}

	entry := preToolUse[0].(map[string]any)
	if entry["_artifact"] != "lint-hook" {
		t.Errorf("_artifact = %v, want lint-hook", entry["_artifact"])
	}

	command, ok := entry["command"].(string)
	if !ok || !strings.Contains(command, "hook.sh") {
		t.Errorf("command should contain hook.sh, got: %v", entry["command"])
	}
	if !filepath.IsAbs(command) {
		t.Errorf("command should be absolute, got: %s", command)
	}
}

func TestCursorHookHandler_Command_Install(t *testing.T) {
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
			Args:    []string{"lint", "--fix"},
		},
	}

	zipData := createTestZip(t, map[string]string{
		"metadata.toml": `[asset]
name = "cmd-hook"
version = "1.0.0"
type = "hook"
description = "Command hook"

[hook]
event = "post-tool-use"
command = "npx"
args = ["lint", "--fix"]
`,
	})

	handler := NewHookHandler(meta)
	if err := handler.Install(context.Background(), zipData, targetBase); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	// No files should be extracted
	hookDir := filepath.Join(targetBase, "hooks", "cmd-hook")
	if _, err := os.Stat(hookDir); !os.IsNotExist(err) {
		t.Error("Command-only hook should not create directory")
	}

	// Verify hooks.json
	config := readJSON(t, filepath.Join(targetBase, "hooks.json"))
	hooks := config["hooks"].(map[string]any)

	postToolUse, ok := hooks["postToolUse"].([]any)
	if !ok || len(postToolUse) == 0 {
		t.Fatal("postToolUse should have entries")
	}

	entry := postToolUse[0].(map[string]any)
	if entry["command"] != "npx lint --fix" {
		t.Errorf("command = %v, want \"npx lint --fix\"", entry["command"])
	}
}

func TestCursorHookHandler_EventMapping(t *testing.T) {
	tests := []struct {
		canonical string
		native    string
	}{
		{"session-start", "sessionStart"},
		{"session-end", "sessionEnd"},
		{"pre-tool-use", "preToolUse"},
		{"post-tool-use", "postToolUse"},
		{"post-tool-use-failure", "postToolUseFailure"},
		{"user-prompt-submit", "beforeSubmitPrompt"},
		{"stop", "stop"},
		{"subagent-start", "subagentStart"},
		{"subagent-stop", "subagentStop"},
		{"pre-compact", "preCompact"},
	}

	for _, tt := range tests {
		meta := &metadata.Metadata{
			Asset: metadata.Asset{Name: "test", Version: "1.0.0", Type: asset.TypeHook},
			Hook:  &metadata.HookConfig{Event: tt.canonical, Command: "echo"},
		}
		handler := NewHookHandler(meta)
		native, supported := handler.mapEventToCursor()
		if !supported {
			t.Errorf("Event %q should be supported for Cursor", tt.canonical)
		}
		if native != tt.native {
			t.Errorf("mapEventToCursor(%q) = %q, want %q", tt.canonical, native, tt.native)
		}
	}
}

func TestCursorHookHandler_EventMapping_CursorOverride(t *testing.T) {
	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "test", Version: "1.0.0", Type: asset.TypeHook},
		Hook: &metadata.HookConfig{
			Event:   "pre-tool-use",
			Command: "echo",
			Cursor:  map[string]any{"event": "afterFileEdit"},
		},
	}
	handler := NewHookHandler(meta)
	native, supported := handler.mapEventToCursor()
	if !supported {
		t.Error("Should be supported with override")
	}
	if native != "afterFileEdit" {
		t.Errorf("Should use override, got %q", native)
	}
}

func TestCursorHookHandler_Remove(t *testing.T) {
	targetBase := t.TempDir()

	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "my-hook", Version: "1.0.0", Type: asset.TypeHook},
		Hook:  &metadata.HookConfig{Event: "pre-tool-use", Command: "echo"},
	}

	// Pre-populate hooks.json
	hooksConfig := &HooksConfig{
		Version: 1,
		Hooks: map[string][]map[string]any{
			"preToolUse": {
				{"_artifact": "my-hook", "command": "echo"},
				{"_artifact": "other-hook", "command": "other"},
			},
		},
	}
	data, _ := json.MarshalIndent(hooksConfig, "", "  ")
	os.WriteFile(filepath.Join(targetBase, "hooks.json"), data, 0644)

	handler := NewHookHandler(meta)
	if err := handler.Remove(context.Background(), targetBase); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	config := readJSON(t, filepath.Join(targetBase, "hooks.json"))
	hooks := config["hooks"].(map[string]any)
	preToolUse := hooks["preToolUse"].([]any)

	if len(preToolUse) != 1 {
		t.Fatalf("Should have 1 remaining hook, got %d", len(preToolUse))
	}
	remaining := preToolUse[0].(map[string]any)
	if remaining["_artifact"] != "other-hook" {
		t.Errorf("Wrong hook remaining: %v", remaining["_artifact"])
	}
}

func TestCursorHookHandler_VerifyInstalled_CommandMode(t *testing.T) {
	targetBase := t.TempDir()

	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "cmd-hook", Version: "1.0.0", Type: asset.TypeHook},
		Hook:  &metadata.HookConfig{Event: "pre-tool-use", Command: "echo"},
	}
	handler := NewHookHandler(meta)

	// Not installed
	installed, _ := handler.VerifyInstalled(targetBase)
	if installed {
		t.Error("Should not be installed initially")
	}

	// Write hooks.json
	hooksConfig := &HooksConfig{
		Version: 1,
		Hooks: map[string][]map[string]any{
			"preToolUse": {
				{"_artifact": "cmd-hook", "command": "echo"},
			},
		},
	}
	data, _ := json.MarshalIndent(hooksConfig, "", "  ")
	os.WriteFile(filepath.Join(targetBase, "hooks.json"), data, 0644)

	installed, msg := handler.VerifyInstalled(targetBase)
	if !installed {
		t.Errorf("Should be installed, got: %s", msg)
	}
}

func TestCursorHookHandler_BuildEntry_MergesCursorOverrides(t *testing.T) {
	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "test", Version: "1.0.0", Type: asset.TypeHook},
		Hook: &metadata.HookConfig{
			Event:   "pre-tool-use",
			Command: "echo",
			Timeout: 30,
			Cursor:  map[string]any{"loop_limit": 5, "event": "shouldNotAppear"},
		},
	}
	handler := NewHookHandler(meta)
	entry := handler.buildHookEntry(t.TempDir())

	if entry["loop_limit"] != 5 {
		t.Errorf("loop_limit should be merged, got: %v", entry["loop_limit"])
	}
	if entry["timeout"] != 30 {
		t.Errorf("timeout = %v, want 30", entry["timeout"])
	}
	if _, exists := entry["event"]; exists {
		t.Error("event should not be in entry (handled by mapEventToCursor)")
	}
}
