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

func createTestHookZip(t *testing.T, files map[string]string) []byte {
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

func readKiroHookFile(t *testing.T, path string) KiroHookFile {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to read hook file: %v", err)
	}
	var hookFile KiroHookFile
	if err := json.Unmarshal(data, &hookFile); err != nil {
		t.Fatalf("Failed to parse hook file: %v", err)
	}
	return hookFile
}

func TestHookHandler_ScriptFile_Install(t *testing.T) {
	targetBase := t.TempDir()

	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name:        "lint-hook",
			Version:     "1.0.0",
			Type:        asset.TypeHook,
			Description: "A lint hook",
		},
		Hook: &metadata.HookConfig{
			Event:      "pre-tool-use",
			ScriptFile: "hook.sh",
			Timeout:    30,
		},
	}

	zipData := createTestHookZip(t, map[string]string{
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
	hookScript := filepath.Join(targetBase, DirHooks, "lint-hook", "hook.sh")
	if _, err := os.Stat(hookScript); os.IsNotExist(err) {
		t.Error("hook.sh should be extracted to hooks directory")
	}

	// Verify .kiro.hook file was created
	hookFilePath := filepath.Join(targetBase, DirHooks, "lint-hook.kiro.hook")
	hookFile := readKiroHookFile(t, hookFilePath)

	if hookFile.Name != "lint-hook" {
		t.Errorf("name = %q, want %q", hookFile.Name, "lint-hook")
	}
	if hookFile.Description != "A lint hook" {
		t.Errorf("description = %q, want %q", hookFile.Description, "A lint hook")
	}
	if hookFile.When.Type != "preToolUse" {
		t.Errorf("when.type = %q, want %q", hookFile.When.Type, "preToolUse")
	}
	if hookFile.Then.Type != "runCommand" {
		t.Errorf("then.type = %q, want %q", hookFile.Then.Type, "runCommand")
	}
	if !strings.Contains(hookFile.Then.Command, "hook.sh") {
		t.Errorf("command should contain hook.sh path, got: %q", hookFile.Then.Command)
	}
	if !filepath.IsAbs(hookFile.Then.Command) {
		t.Errorf("command should be absolute path, got: %q", hookFile.Then.Command)
	}
}

func TestHookHandler_Command_Install(t *testing.T) {
	targetBase := t.TempDir()

	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name:        "cmd-hook",
			Version:     "1.0.0",
			Type:        asset.TypeHook,
			Description: "Command hook",
		},
		Hook: &metadata.HookConfig{
			Event:   "post-tool-use",
			Command: "npx",
			Args:    []string{"lint-check", "--fix"},
			Timeout: 10,
		},
	}

	zipData := createTestHookZip(t, map[string]string{
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

	// Verify no files were extracted (command-only)
	hookDir := filepath.Join(targetBase, DirHooks, "cmd-hook")
	if _, err := os.Stat(hookDir); !os.IsNotExist(err) {
		t.Error("Command-only hook should not create install directory")
	}

	// Verify .kiro.hook file was created
	hookFilePath := filepath.Join(targetBase, DirHooks, "cmd-hook.kiro.hook")
	hookFile := readKiroHookFile(t, hookFilePath)

	if hookFile.When.Type != "postToolUse" {
		t.Errorf("when.type = %q, want %q", hookFile.When.Type, "postToolUse")
	}
	if hookFile.Then.Command != "npx lint-check --fix" {
		t.Errorf("command = %q, want %q", hookFile.Then.Command, "npx lint-check --fix")
	}
}

func TestHookHandler_EventMapping(t *testing.T) {
	tests := []struct {
		canonical string
		native    string
	}{
		{"session-start", "sessionStart"},
		{"session-end", "sessionEnd"},
		{"pre-tool-use", "preToolUse"},
		{"post-tool-use", "postToolUse"},
		{"user-prompt-submit", "promptSubmit"},
		{"stop", "agentStop"},
	}

	for _, tt := range tests {
		meta := &metadata.Metadata{
			Asset: metadata.Asset{Name: "test", Version: "1.0.0", Type: asset.TypeHook},
			Hook:  &metadata.HookConfig{Event: tt.canonical, Command: "echo test"},
		}
		handler := NewHookHandler(meta)
		native, supported := handler.mapEventToKiro()
		if !supported {
			t.Errorf("Event %q should be supported", tt.canonical)
		}
		if native != tt.native {
			t.Errorf("mapEventToKiro(%q) = %q, want %q", tt.canonical, native, tt.native)
		}
	}
}

func TestHookHandler_EventMapping_UnsupportedEvent(t *testing.T) {
	unsupportedEvents := []string{
		"post-tool-use-failure", // Kiro doesn't have failure-specific event
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
		_, supported := handler.mapEventToKiro()
		if supported {
			t.Errorf("Event %q should not be supported by Kiro", event)
		}
	}
}

func TestHookHandler_EventMapping_KiroOverride(t *testing.T) {
	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "test", Version: "1.0.0", Type: asset.TypeHook},
		Hook: &metadata.HookConfig{
			Event:   "pre-tool-use",
			Command: "echo test",
			Kiro:    map[string]any{"event": "customEvent"},
		},
	}
	handler := NewHookHandler(meta)
	native, supported := handler.mapEventToKiro()
	if !supported {
		t.Error("Should be supported with override")
	}
	if native != "customEvent" {
		t.Errorf("Should use override event, got %q", native)
	}
}

func TestHookHandler_Remove(t *testing.T) {
	targetBase := t.TempDir()
	hooksDir := filepath.Join(targetBase, DirHooks)
	os.MkdirAll(hooksDir, 0755)

	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "lint-hook", Version: "1.0.0", Type: asset.TypeHook},
		Hook:  &metadata.HookConfig{Event: "pre-tool-use", Command: "echo lint"},
	}

	// Pre-populate hook file and extracted directory
	hookFilePath := filepath.Join(hooksDir, "lint-hook.kiro.hook")
	hookData, _ := json.MarshalIndent(KiroHookFile{
		Name:    "lint-hook",
		Version: "1",
		When:    KiroHookWhen{Type: "preToolUse"},
		Then:    KiroHookThen{Type: "runCommand", Command: "echo lint"},
	}, "", "  ")
	os.WriteFile(hookFilePath, hookData, 0644)

	// Create extracted files directory
	extractedDir := filepath.Join(hooksDir, "lint-hook")
	os.MkdirAll(extractedDir, 0755)
	os.WriteFile(filepath.Join(extractedDir, "hook.sh"), []byte("#!/bin/bash\necho lint"), 0755)

	// Create another hook file BEFORE Remove to verify it's preserved
	otherHookPath := filepath.Join(hooksDir, "other-hook.kiro.hook")
	os.WriteFile(otherHookPath, []byte(`{"name":"other"}`), 0644)

	handler := NewHookHandler(meta)
	if err := handler.Remove(context.Background(), targetBase); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	// Verify .kiro.hook file was removed
	if _, err := os.Stat(hookFilePath); !os.IsNotExist(err) {
		t.Error("Hook file should be removed")
	}

	// Verify extracted directory was removed
	if _, err := os.Stat(extractedDir); !os.IsNotExist(err) {
		t.Error("Extracted directory should be removed")
	}

	// Verify other hook files are preserved (not affected by Remove)
	if _, err := os.Stat(otherHookPath); os.IsNotExist(err) {
		t.Error("Other hook files should be preserved after Remove")
	}
}

func TestHookHandler_ToolTypes(t *testing.T) {
	targetBase := t.TempDir()

	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name:        "tool-hook",
			Version:     "1.0.0",
			Type:        asset.TypeHook,
			Description: "Hook with tool types",
		},
		Hook: &metadata.HookConfig{
			Event:   "post-tool-use",
			Command: "echo test",
			Kiro: map[string]any{
				"tool-types": []any{"readFile", "writeFile"},
			},
		},
	}

	zipData := createTestHookZip(t, map[string]string{
		"metadata.toml": `[asset]
name = "tool-hook"
version = "1.0.0"
type = "hook"

[hook]
event = "post-tool-use"
command = "echo test"

[hook.kiro]
tool-types = ["readFile", "writeFile"]
`,
	})

	handler := NewHookHandler(meta)
	if err := handler.Install(context.Background(), zipData, targetBase); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	// Verify .kiro.hook file was created with toolTypes
	hookFilePath := filepath.Join(targetBase, DirHooks, "tool-hook.kiro.hook")
	hookFile := readKiroHookFile(t, hookFilePath)

	if hookFile.When.Type != "postToolUse" {
		t.Errorf("when.type = %q, want %q", hookFile.When.Type, "postToolUse")
	}
	if len(hookFile.When.ToolTypes) != 2 {
		t.Errorf("toolTypes length = %d, want 2", len(hookFile.When.ToolTypes))
	}
	if hookFile.When.ToolTypes[0] != "readFile" || hookFile.When.ToolTypes[1] != "writeFile" {
		t.Errorf("toolTypes = %v, want [readFile, writeFile]", hookFile.When.ToolTypes)
	}
}

func TestHookHandler_ToolTypes_Wildcard(t *testing.T) {
	targetBase := t.TempDir()

	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name:    "wildcard-hook",
			Version: "1.0.0",
			Type:    asset.TypeHook,
		},
		Hook: &metadata.HookConfig{
			Event:   "post-tool-use",
			Command: "echo test",
			Kiro: map[string]any{
				"tool-types": []any{"*"},
			},
		},
	}

	zipData := createTestHookZip(t, map[string]string{
		"metadata.toml": `[asset]
name = "wildcard-hook"
version = "1.0.0"
type = "hook"

[hook]
event = "post-tool-use"
command = "echo test"

[hook.kiro]
tool-types = ["*"]
`,
	})

	handler := NewHookHandler(meta)
	if err := handler.Install(context.Background(), zipData, targetBase); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	hookFilePath := filepath.Join(targetBase, DirHooks, "wildcard-hook.kiro.hook")
	hookFile := readKiroHookFile(t, hookFilePath)

	if len(hookFile.When.ToolTypes) != 1 || hookFile.When.ToolTypes[0] != "*" {
		t.Errorf("toolTypes = %v, want [*]", hookFile.When.ToolTypes)
	}
}

func TestHookHandler_ToolTypes_NotSpecified(t *testing.T) {
	targetBase := t.TempDir()

	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name:    "no-tooltype-hook",
			Version: "1.0.0",
			Type:    asset.TypeHook,
		},
		Hook: &metadata.HookConfig{
			Event:   "post-tool-use",
			Command: "echo test",
			// No Kiro section
		},
	}

	zipData := createTestHookZip(t, map[string]string{
		"metadata.toml": `[asset]
name = "no-tooltype-hook"
version = "1.0.0"
type = "hook"

[hook]
event = "post-tool-use"
command = "echo test"
`,
	})

	handler := NewHookHandler(meta)
	if err := handler.Install(context.Background(), zipData, targetBase); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	hookFilePath := filepath.Join(targetBase, DirHooks, "no-tooltype-hook.kiro.hook")
	hookFile := readKiroHookFile(t, hookFilePath)

	// When no tool-types specified, toolTypes should be nil/empty (omitted from JSON)
	if len(hookFile.When.ToolTypes) != 0 {
		t.Errorf("toolTypes should be empty when not specified, got %v", hookFile.When.ToolTypes)
	}
}

func TestHookHandler_VerifyInstalled_CommandMode(t *testing.T) {
	targetBase := t.TempDir()
	hooksDir := filepath.Join(targetBase, DirHooks)
	os.MkdirAll(hooksDir, 0755)

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

	// Write hook file
	hookFilePath := filepath.Join(hooksDir, "cmd-hook.kiro.hook")
	hookData, _ := json.MarshalIndent(KiroHookFile{
		Name:    "cmd-hook",
		Version: "1",
		When:    KiroHookWhen{Type: "preToolUse"},
		Then:    KiroHookThen{Type: "runCommand", Command: "echo test"},
	}, "", "  ")
	os.WriteFile(hookFilePath, hookData, 0644)

	installed, msg := handler.VerifyInstalled(targetBase)
	if !installed {
		t.Errorf("Should be installed, got msg: %s", msg)
	}
}
