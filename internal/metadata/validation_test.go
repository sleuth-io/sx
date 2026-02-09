package metadata

import (
	"strings"
	"testing"

	"github.com/sleuth-io/sx/internal/asset"
)

func TestHookConfig_Validate_CanonicalEvents(t *testing.T) {
	validEvents := []string{
		"session-start", "session-end",
		"pre-tool-use", "post-tool-use", "post-tool-use-failure",
		"user-prompt-submit", "stop",
		"subagent-start", "subagent-stop",
		"pre-compact",
	}
	for _, event := range validEvents {
		h := &HookConfig{Event: event, ScriptFile: "hook.sh"}
		if err := h.Validate(); err != nil {
			t.Errorf("HookConfig.Validate() with event %q returned error: %v", event, err)
		}
	}
}

func TestHookConfig_Validate_InvalidEvent(t *testing.T) {
	invalidEvents := []string{
		"pre-commit", "post-commit", "pre-push", "post-push",
		"pre-merge", "post-merge", "invalid", "",
	}
	for _, event := range invalidEvents {
		h := &HookConfig{Event: event, ScriptFile: "hook.sh"}
		err := h.Validate()
		if event == "" {
			if err == nil {
				t.Errorf("HookConfig.Validate() with empty event should fail")
			}
		} else {
			if err == nil {
				t.Errorf("HookConfig.Validate() with event %q should fail", event)
			} else if !strings.Contains(err.Error(), "invalid hook event") {
				t.Errorf("Expected 'invalid hook event' error for %q, got: %v", event, err)
			}
		}
	}
}

func TestHookConfig_Validate_ScriptFile(t *testing.T) {
	h := &HookConfig{Event: "pre-tool-use", ScriptFile: "hook.sh"}
	if err := h.Validate(); err != nil {
		t.Errorf("ScriptFile mode should be valid: %v", err)
	}
}

func TestHookConfig_Validate_Command(t *testing.T) {
	h := &HookConfig{Event: "pre-tool-use", Command: "npx lint-check"}
	if err := h.Validate(); err != nil {
		t.Errorf("Command mode should be valid: %v", err)
	}
}

func TestHookConfig_Validate_NeitherScriptFileNorCommand(t *testing.T) {
	h := &HookConfig{Event: "pre-tool-use"}
	err := h.Validate()
	if err == nil {
		t.Fatal("Should fail when neither script-file nor command is set")
	}
	if !strings.Contains(err.Error(), "either script-file or command is required") {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestHookConfig_Validate_BothScriptFileAndCommand(t *testing.T) {
	h := &HookConfig{Event: "pre-tool-use", ScriptFile: "hook.sh", Command: "npx lint"}
	err := h.Validate()
	if err == nil {
		t.Fatal("Should fail when both script-file and command are set")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestHookConfig_Validate_NegativeTimeout(t *testing.T) {
	h := &HookConfig{Event: "pre-tool-use", ScriptFile: "hook.sh", Timeout: -1}
	err := h.Validate()
	if err == nil {
		t.Fatal("Should fail with negative timeout")
	}
	if !strings.Contains(err.Error(), "timeout must be non-negative") {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestHookConfig_Validate_ZeroTimeout(t *testing.T) {
	h := &HookConfig{Event: "pre-tool-use", ScriptFile: "hook.sh", Timeout: 0}
	if err := h.Validate(); err != nil {
		t.Errorf("Zero timeout should be valid: %v", err)
	}
}

func TestMetadata_Validate_HookRequiresSection(t *testing.T) {
	m := &Metadata{
		Asset: Asset{Name: "test", Version: "1.0.0", Type: asset.TypeHook},
	}
	err := m.Validate()
	if err == nil {
		t.Fatal("Should fail without [hook] section")
	}
	if !strings.Contains(err.Error(), "[hook] section is required") {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestMetadata_Validate_MCPRequiresSection(t *testing.T) {
	m := &Metadata{
		Asset: Asset{Name: "test", Version: "1.0.0", Type: asset.TypeMCP},
	}
	err := m.Validate()
	if err == nil {
		t.Fatal("Should fail without [mcp] section")
	}
	if !strings.Contains(err.Error(), "[mcp] section is required") {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestMetadata_Validate_MCPRemoteTypeResolves(t *testing.T) {
	// Parsing "mcp-remote" should resolve to TypeMCP and validate with MCP section
	m := &Metadata{
		Asset: Asset{Name: "test", Version: "1.0.0", Type: asset.FromString("mcp-remote")},
		MCP:   &MCPConfig{Transport: "stdio", Command: "npx", Args: []string{"server"}},
	}
	if err := m.Validate(); err != nil {
		t.Errorf("mcp-remote type with MCP config should validate: %v", err)
	}
}

func TestAsset_Validate_TypeMCPRemote_Invalid_In_ErrorMsg(t *testing.T) {
	m := &Metadata{
		Asset: Asset{Name: "test", Version: "1.0.0", Type: asset.Type{Key: "bogus"}},
	}
	err := m.Validate()
	if err == nil {
		t.Fatal("Should fail with invalid type")
	}
	// Error message should NOT mention mcp-remote as a valid type
	if strings.Contains(err.Error(), "mcp-remote") {
		t.Errorf("Error message should not mention mcp-remote: %v", err)
	}
}

func TestMCPConfig_Validate_SSE_Valid(t *testing.T) {
	m := &MCPConfig{Transport: "sse", URL: "https://example.com/mcp"}
	if err := m.Validate(); err != nil {
		t.Errorf("SSE with URL should be valid: %v", err)
	}
}

func TestMCPConfig_Validate_HTTP_Valid(t *testing.T) {
	m := &MCPConfig{Transport: "http", URL: "https://example.com/mcp"}
	if err := m.Validate(); err != nil {
		t.Errorf("HTTP with URL should be valid: %v", err)
	}
}

func TestMCPConfig_Validate_SSE_MissingURL(t *testing.T) {
	m := &MCPConfig{Transport: "sse"}
	err := m.Validate()
	if err == nil {
		t.Fatal("SSE without URL should fail")
	}
	if !strings.Contains(err.Error(), "url is required") {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestMCPConfig_Validate_SSE_WithCommand(t *testing.T) {
	m := &MCPConfig{Transport: "sse", URL: "https://example.com/mcp", Command: "npx"}
	err := m.Validate()
	if err == nil {
		t.Fatal("SSE with command should fail")
	}
	if !strings.Contains(err.Error(), "command is not allowed") {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestMCPConfig_Validate_SSE_WithArgs(t *testing.T) {
	m := &MCPConfig{Transport: "sse", URL: "https://example.com/mcp", Args: []string{"arg1"}}
	err := m.Validate()
	if err == nil {
		t.Fatal("SSE with args should fail")
	}
	if !strings.Contains(err.Error(), "args is not allowed") {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestMCPConfig_Validate_Stdio_WithURL(t *testing.T) {
	m := &MCPConfig{Transport: "stdio", Command: "npx", Args: []string{"server"}, URL: "https://example.com"}
	err := m.Validate()
	if err == nil {
		t.Fatal("stdio with URL should fail")
	}
	if !strings.Contains(err.Error(), "url is not allowed") {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestMCPConfig_Validate_UnknownTransport(t *testing.T) {
	m := &MCPConfig{Transport: "websocket", URL: "wss://example.com"}
	err := m.Validate()
	if err == nil {
		t.Fatal("Unknown transport should fail")
	}
	if !strings.Contains(err.Error(), "invalid transport") {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestMCPConfig_IsRemote(t *testing.T) {
	tests := []struct {
		transport string
		want      bool
	}{
		{"stdio", false},
		{"sse", true},
		{"http", true},
	}
	for _, tc := range tests {
		m := &MCPConfig{Transport: tc.transport}
		if got := m.IsRemote(); got != tc.want {
			t.Errorf("IsRemote() for transport %q = %v, want %v", tc.transport, got, tc.want)
		}
	}
}

func TestHookMetadata_Parse_CommandMode(t *testing.T) {
	tomlData := `
[asset]
name = "my-hook"
version = "1.0.0"
type = "hook"
description = "A command-only hook"

[hook]
event = "pre-tool-use"
command = "npx"
args = ["lint-check", "--fix"]
timeout = 30
matcher = "Edit|Write"
`
	meta, err := Parse([]byte(tomlData))
	if err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}
	if meta.Hook == nil {
		t.Fatal("Hook config should be present")
	}
	if meta.Hook.Command != "npx" {
		t.Errorf("Command = %q, want \"npx\"", meta.Hook.Command)
	}
	if len(meta.Hook.Args) != 2 || meta.Hook.Args[0] != "lint-check" {
		t.Errorf("Args = %v, want [\"lint-check\", \"--fix\"]", meta.Hook.Args)
	}
	if meta.Hook.Matcher != "Edit|Write" {
		t.Errorf("Matcher = %q, want \"Edit|Write\"", meta.Hook.Matcher)
	}
	if meta.Hook.Timeout != 30 {
		t.Errorf("Timeout = %d, want 30", meta.Hook.Timeout)
	}
}

func TestHookMetadata_Parse_ClientOverrides(t *testing.T) {
	tomlData := `
[asset]
name = "my-hook"
version = "1.0.0"
type = "hook"
description = "Hook with client overrides"

[hook]
event = "pre-tool-use"
command = "npx lint-check"
timeout = 30

[hook.claude-code]
async = true

[hook.cursor]
event = "afterFileEdit"
`
	meta, err := Parse([]byte(tomlData))
	if err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}
	if meta.Hook.ClaudeCode == nil {
		t.Fatal("ClaudeCode overrides should be present")
	}
	if async, ok := meta.Hook.ClaudeCode["async"].(bool); !ok || !async {
		t.Errorf("ClaudeCode async = %v, want true", meta.Hook.ClaudeCode["async"])
	}
	if meta.Hook.Cursor == nil {
		t.Fatal("Cursor overrides should be present")
	}
	if event, ok := meta.Hook.Cursor["event"].(string); !ok || event != "afterFileEdit" {
		t.Errorf("Cursor event = %v, want \"afterFileEdit\"", meta.Hook.Cursor["event"])
	}
}
