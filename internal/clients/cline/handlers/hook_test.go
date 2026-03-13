package handlers

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/metadata"
)

func TestClineHookHandler_ScriptFile_Install(t *testing.T) {
	targetBase := t.TempDir()
	hooksDir := t.TempDir()

	// Override hooks dir for testing
	origGetHooksDir := getClineHooksDir
	getClineHooksDir = func() (string, error) { return hooksDir, nil }
	defer func() { getClineHooksDir = origGetHooksDir }()

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

	zipData := createZipFromFiles(t, map[string]string{
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

	// Verify hook script was created in Cline hooks directory
	// pre-tool-use maps to PreToolUse for Cline
	hookPath := filepath.Join(hooksDir, "PreToolUse")
	content, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("Failed to read hook script: %v", err)
	}

	contentStr := string(content)
	if !strings.Contains(contentStr, assetMarker+":lint-hook") {
		t.Errorf("Hook should contain asset marker, got: %s", contentStr)
	}
	if !strings.Contains(contentStr, "hook.sh") {
		t.Errorf("Hook should reference hook.sh, got: %s", contentStr)
	}
}

func TestClineHookHandler_Command_Install(t *testing.T) {
	targetBase := t.TempDir()
	hooksDir := t.TempDir()

	// Override hooks dir for testing
	origGetHooksDir := getClineHooksDir
	getClineHooksDir = func() (string, error) { return hooksDir, nil }
	defer func() { getClineHooksDir = origGetHooksDir }()

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

	zipData := createZipFromFiles(t, map[string]string{
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

	// No files should be extracted (command-only mode)
	hookDir := filepath.Join(targetBase, "hooks", "cmd-hook")
	if _, err := os.Stat(hookDir); !os.IsNotExist(err) {
		t.Error("Command-only hook should not create directory")
	}

	// Verify hook script was created
	hookPath := filepath.Join(hooksDir, "PostToolUse")
	content, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("Failed to read hook script: %v", err)
	}

	contentStr := string(content)
	if !strings.Contains(contentStr, "npx lint --fix") {
		t.Errorf("Hook should contain command, got: %s", contentStr)
	}
}

func TestClineHookHandler_EventMapping(t *testing.T) {
	tests := []struct {
		canonical string
		native    string
	}{
		{"session-start", "TaskStart"},
		{"session-end", "TaskEnd"},
		{"pre-tool-use", "PreToolUse"},
		{"post-tool-use", "PostToolUse"},
		{"post-tool-use-failure", "PostToolUseFailure"},
		{"user-prompt-submit", "UserPromptSubmit"},
	}

	for _, tt := range tests {
		meta := &metadata.Metadata{
			Asset: metadata.Asset{Name: "test", Version: "1.0.0", Type: asset.TypeHook},
			Hook:  &metadata.HookConfig{Event: tt.canonical, Command: "echo"},
		}
		handler := NewHookHandler(meta)
		native, supported := handler.mapEventToCline()
		if !supported {
			t.Errorf("Event %q should be supported for Cline", tt.canonical)
		}
		if native != tt.native {
			t.Errorf("mapEventToCline(%q) = %q, want %q", tt.canonical, native, tt.native)
		}
	}
}

func TestClineHookHandler_EventMapping_UnsupportedEvent(t *testing.T) {
	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "test", Version: "1.0.0", Type: asset.TypeHook},
		Hook:  &metadata.HookConfig{Event: "unknown-event", Command: "echo"},
	}
	handler := NewHookHandler(meta)
	_, supported := handler.mapEventToCline()
	if supported {
		t.Error("Unknown event should not be supported")
	}
}

func TestClineHookHandler_EventMapping_ClineOverride(t *testing.T) {
	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "test", Version: "1.0.0", Type: asset.TypeHook},
		Hook: &metadata.HookConfig{
			Event:   "pre-tool-use",
			Command: "echo",
			Cline:   map[string]any{"event": "CustomEvent"},
		},
	}
	handler := NewHookHandler(meta)
	native, supported := handler.mapEventToCline()
	if !supported {
		t.Error("Should be supported with override")
	}
	if native != "CustomEvent" {
		t.Errorf("Should use override, got %q", native)
	}
}

func TestClineHookHandler_Remove(t *testing.T) {
	targetBase := t.TempDir()
	hooksDir := t.TempDir()

	// Override hooks dir for testing
	origGetHooksDir := getClineHooksDir
	getClineHooksDir = func() (string, error) { return hooksDir, nil }
	defer func() { getClineHooksDir = origGetHooksDir }()

	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "my-hook", Version: "1.0.0", Type: asset.TypeHook},
		Hook:  &metadata.HookConfig{Event: "pre-tool-use", Command: "echo"},
	}

	// Pre-create hook script
	hookPath := filepath.Join(hooksDir, "PreToolUse")
	script := "#!/bin/sh\n" + assetMarker + ":my-hook\necho test"
	if err := os.WriteFile(hookPath, []byte(script), 0755); err != nil {
		t.Fatalf("Failed to write hook: %v", err)
	}

	handler := NewHookHandler(meta)
	if err := handler.Remove(context.Background(), targetBase); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	// Verify hook was removed
	if _, err := os.Stat(hookPath); !os.IsNotExist(err) {
		t.Error("Hook should have been removed")
	}
}

func TestClineHookHandler_Remove_NotOurHook(t *testing.T) {
	targetBase := t.TempDir()
	hooksDir := t.TempDir()

	// Override hooks dir for testing
	origGetHooksDir := getClineHooksDir
	getClineHooksDir = func() (string, error) { return hooksDir, nil }
	defer func() { getClineHooksDir = origGetHooksDir }()

	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "my-hook", Version: "1.0.0", Type: asset.TypeHook},
		Hook:  &metadata.HookConfig{Event: "pre-tool-use", Command: "echo"},
	}

	// Pre-create hook script owned by different asset
	hookPath := filepath.Join(hooksDir, "PreToolUse")
	script := "#!/bin/sh\n" + assetMarker + ":other-hook\necho test"
	if err := os.WriteFile(hookPath, []byte(script), 0755); err != nil {
		t.Fatalf("Failed to write hook: %v", err)
	}

	handler := NewHookHandler(meta)
	if err := handler.Remove(context.Background(), targetBase); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	// Verify hook was NOT removed (not our hook)
	if _, err := os.Stat(hookPath); os.IsNotExist(err) {
		t.Error("Hook should NOT have been removed (belongs to other asset)")
	}
}

func TestClineHookHandler_VerifyInstalled(t *testing.T) {
	targetBase := t.TempDir()
	hooksDir := t.TempDir()

	// Override hooks dir for testing
	origGetHooksDir := getClineHooksDir
	getClineHooksDir = func() (string, error) { return hooksDir, nil }
	defer func() { getClineHooksDir = origGetHooksDir }()

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

	// Write hook script
	hookPath := filepath.Join(hooksDir, "PreToolUse")
	script := "#!/bin/sh\n" + assetMarker + ":cmd-hook\necho test"
	if err := os.WriteFile(hookPath, []byte(script), 0755); err != nil {
		t.Fatalf("Failed to write hook: %v", err)
	}

	installed, msg := handler.VerifyInstalled(targetBase)
	if !installed {
		t.Errorf("Should be installed, got: %s", msg)
	}
}

func TestClineHookHandler_VerifyInstalled_WrongAsset(t *testing.T) {
	targetBase := t.TempDir()
	hooksDir := t.TempDir()

	// Override hooks dir for testing
	origGetHooksDir := getClineHooksDir
	getClineHooksDir = func() (string, error) { return hooksDir, nil }
	defer func() { getClineHooksDir = origGetHooksDir }()

	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "my-hook", Version: "1.0.0", Type: asset.TypeHook},
		Hook:  &metadata.HookConfig{Event: "pre-tool-use", Command: "echo"},
	}
	handler := NewHookHandler(meta)

	// Write hook script owned by different asset
	hookPath := filepath.Join(hooksDir, "PreToolUse")
	script := "#!/bin/sh\n" + assetMarker + ":other-hook\necho test"
	if err := os.WriteFile(hookPath, []byte(script), 0755); err != nil {
		t.Fatalf("Failed to write hook: %v", err)
	}

	installed, msg := handler.VerifyInstalled(targetBase)
	if installed {
		t.Error("Should not be installed (different asset)")
	}
	if !strings.Contains(msg, "not managed by this asset") {
		t.Errorf("Expected message about wrong asset, got: %s", msg)
	}
}

func TestClineHookHandler_GenerateScript(t *testing.T) {
	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "test-hook", Version: "1.0.0", Type: asset.TypeHook},
		Hook:  &metadata.HookConfig{Event: "pre-tool-use", Command: "echo"},
	}
	handler := NewHookHandler(meta)

	script := handler.generateScript("echo hello")

	// Should contain asset marker
	if !strings.Contains(script, assetMarker+":test-hook") {
		t.Errorf("Script should contain asset marker, got: %s", script)
	}

	// Should contain command
	if !strings.Contains(script, "echo hello") {
		t.Errorf("Script should contain command, got: %s", script)
	}

	// Should have shebang on Unix
	if runtime.GOOS != "windows" {
		if !strings.HasPrefix(script, "#!/bin/sh\n") {
			t.Errorf("Script should start with shebang, got: %s", script)
		}
	}
}

func TestClineHookHandler_Validate(t *testing.T) {
	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "test", Version: "1.0.0", Type: asset.TypeHook},
		Hook:  &metadata.HookConfig{Event: "pre-tool-use", ScriptFile: "hook.sh"},
	}
	handler := NewHookHandler(meta)

	// Valid zip with script file
	validZip := createZipFromFiles(t, map[string]string{
		"metadata.toml": `[asset]
name = "test"
version = "1.0.0"
type = "hook"

[hook]
event = "pre-tool-use"
script-file = "hook.sh"
`,
		"hook.sh": "#!/bin/bash\necho test",
	})

	if err := handler.Validate(validZip); err != nil {
		t.Errorf("Valid zip should pass validation: %v", err)
	}

	// Invalid zip missing script file
	invalidZip := createZipFromFiles(t, map[string]string{
		"metadata.toml": `[asset]
name = "test"
version = "1.0.0"
type = "hook"

[hook]
event = "pre-tool-use"
script-file = "missing.sh"
`,
	})

	if err := handler.Validate(invalidZip); err == nil {
		t.Error("Invalid zip should fail validation")
	}
}
