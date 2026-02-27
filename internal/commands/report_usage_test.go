package commands

import (
	"bytes"
	"os"
	"testing"
)

func TestReportUsageCodexFormat(t *testing.T) {
	// Codex passes JSON as command-line argument with type=agent-turn-complete
	codexJSON := `{"type":"agent-turn-complete","turn-id":"test-123","input-messages":["hello"],"last-assistant-message":"hi"}`

	cmd := NewReportUsageCommand()
	cmd.SetArgs([]string{codexJSON})
	cmd.Flags().Set("client", "codex")

	// Should not error
	err := cmd.Execute()
	if err != nil {
		t.Errorf("Codex format should parse successfully, got error: %v", err)
	}
}

func TestReportUsageClaudeCodeFormat(t *testing.T) {
	// Claude Code passes JSON via stdin with tool_name
	claudeJSON := `{"tool_name":"Skill","tool_input":{"skill":"test-skill"}}`

	cmd := NewReportUsageCommand()
	cmd.SetIn(bytes.NewBufferString(claudeJSON))
	cmd.Flags().Set("client", "claude-code")

	// Should not error (even if skill doesn't exist, it just won't track)
	err := cmd.Execute()
	if err != nil {
		t.Errorf("Claude Code format should parse successfully, got error: %v", err)
	}
}

func TestReportUsageInvalidJSON(t *testing.T) {
	// Invalid JSON should not crash, just log error and return nil
	invalidJSON := `{invalid json}`

	cmd := NewReportUsageCommand()
	cmd.SetArgs([]string{invalidJSON})

	// Suppress stderr during test
	cmd.SetErr(&bytes.Buffer{})

	// Should not error (returns nil on parse failure)
	err := cmd.Execute()
	if err != nil {
		t.Errorf("Invalid JSON should return nil (not crash), got error: %v", err)
	}
}

func TestReportUsageCodexVsClaudeCodeDetection(t *testing.T) {
	// This tests that Codex format is detected BEFORE trying Claude Code format
	// The bug was: Go's lenient JSON parsing accepted Codex JSON as Claude Code format
	// with empty fields, causing silent return instead of proper Codex handling

	// Codex JSON should be detected as Codex (not parsed as Claude Code with empty fields)
	codexJSON := `{"type":"agent-turn-complete","turn-id":"abc-123","input-messages":["hi"],"last-assistant-message":"hello"}`

	cmd := NewReportUsageCommand()
	cmd.SetArgs([]string{codexJSON})

	err := cmd.Execute()
	if err != nil {
		t.Errorf("Codex format should be detected and handled, got error: %v", err)
	}

	// Note: We can't easily verify the DEBUG log was emitted without capturing logs,
	// but the fact that it doesn't error and processes correctly is the key test
}

func TestReportUsageEmptyInput(t *testing.T) {
	// Empty stdin should not crash
	cmd := NewReportUsageCommand()
	cmd.SetIn(bytes.NewBufferString(""))

	// Suppress stderr
	cmd.SetErr(&bytes.Buffer{})

	err := cmd.Execute()
	if err != nil {
		t.Errorf("Empty input should return nil, got error: %v", err)
	}
}

// TestReportUsageClaudeCodeMCPFormat tests Claude Code's snake_case JSON format for MCP tools
// Actual Claude Code log: {"session_id":"...","tool_name":"mcp__sx__query","tool_input":{...}}
func TestReportUsageClaudeCodeMCPFormat(t *testing.T) {
	claudeJSON := `{"session_id":"c31e8751-8b7b-416c-a976-d4fb590202ef","transcript_path":"/home/ines/.claude/projects/-home-ines-work-sleuthio-sx/c31e8751-8b7b-416c-a976-d4fb590202ef.jsonl","cwd":"/home/ines/work/sleuthio/sx","permission_mode":"default","hook_event_name":"PostToolUse","tool_name":"mcp__sx__query","tool_input":{"query":"show pr comments","integration":"github"},"tool_response":[{"type":"text","text":"response"}],"tool_use_id":"toolu_01G8PafqkmFHtgbKL4somdT6"}`

	cmd := NewReportUsageCommand()
	cmd.SetIn(bytes.NewBufferString(claudeJSON))
	cmd.Flags().Set("client", "claude-code")

	// Should not error (parses Claude Code snake_case format)
	err := cmd.Execute()
	if err != nil {
		t.Errorf("Claude Code MCP format should parse successfully, got error: %v", err)
	}
}

// TestReportUsageClaudeCodeSkillFormat tests Claude Code's snake_case JSON format for skills
// Actual Claude Code format: {"tool_name":"Skill","tool_input":{"skill":"my-skill"}}
func TestReportUsageClaudeCodeSkillFormat(t *testing.T) {
	claudeJSON := `{"session_id":"abc-123","cwd":"/home/ines/work/sleuthio/sx","hook_event_name":"PostToolUse","tool_name":"Skill","tool_input":{"skill":"fix-pr"},"tool_response":{"success":true},"tool_use_id":"toolu_123"}`

	cmd := NewReportUsageCommand()
	cmd.SetIn(bytes.NewBufferString(claudeJSON))
	cmd.Flags().Set("client", "claude-code")

	// Should not error
	err := cmd.Execute()
	if err != nil {
		t.Errorf("Claude Code Skill format should parse successfully, got error: %v", err)
	}
}

// TestReportUsageClaudeCodeNonAssetToolIgnored tests that Claude Code's non-asset tools are ignored
// Actual Claude Code log: {"tool_name":"TaskOutput","tool_input":{"task_id":"bb1eb1b",...}}
func TestReportUsageClaudeCodeNonAssetToolIgnored(t *testing.T) {
	claudeJSON := `{"session_id":"613f4723-d86e-4c5c-9b54-3880b1af8763","transcript_path":"/home/ines/.claude/projects/-home-ines-work-sleuthio-sx/613f4723-d86e-4c5c-9b54-3880b1af8763.jsonl","cwd":"/home/ines/work/sleuthio/sx","permission_mode":"acceptEdits","hook_event_name":"PostToolUse","tool_name":"TaskOutput","tool_input":{"task_id":"bb1eb1b","block":true,"timeout":5000},"tool_response":{"retrieval_status":"timeout"},"tool_use_id":"toolu_01KqvWRQkquZrN2BYeM134EY"}`

	cmd := NewReportUsageCommand()
	cmd.SetIn(bytes.NewBufferString(claudeJSON))
	cmd.Flags().Set("client", "claude-code")

	// Should not error (just silently ignores non-asset tools)
	err := cmd.Execute()
	if err != nil {
		t.Errorf("Claude Code TaskOutput should be silently ignored, got error: %v", err)
	}
}

// TestReportUsageCopilotSkillFormat tests Copilot's camelCase JSON format for skills
// Actual Copilot log: {"timestamp":1772181780833,"cwd":"/home/ines/work/sleuthio/sx",
//
//	"toolName":"skill","toolArgs":{"skill":"fix-pr"},"toolResult":{"resultType":"success",...}}
func TestReportUsageCopilotSkillFormat(t *testing.T) {
	copilotJSON := `{"sessionId":"cf4360fc-8ee6-4e7d-b659-bbf6ae161a3b","timestamp":1772181780833,"cwd":"/home/ines/work/sleuthio/sx","toolName":"skill","toolArgs":{"skill":"fix-pr"},"toolResult":{"resultType":"success","textResultForLlm":"Skill \"fix-pr\" loaded successfully. Follow the instructions in the skill context."}}`

	cmd := NewReportUsageCommand()
	cmd.SetIn(bytes.NewBufferString(copilotJSON))
	cmd.Flags().Set("client", "github-copilot")

	// Should not error (parses Copilot camelCase format)
	err := cmd.Execute()
	if err != nil {
		t.Errorf("Copilot skill format should parse successfully, got error: %v", err)
	}
}

// TestReportUsageCopilotMCPFormat tests Copilot's MCP tool format (server-tool)
// Actual Copilot log: {"toolName":"sx-query","toolArgs":{"query":"..."}}
func TestReportUsageCopilotMCPFormat(t *testing.T) {
	copilotJSON := `{"sessionId":"abc-123","timestamp":1772181780833,"cwd":"/tmp","toolName":"sx-query","toolArgs":{"query":"get PR comments","integration":"github"},"toolResult":{"resultType":"success"}}`

	cmd := NewReportUsageCommand()
	cmd.SetIn(bytes.NewBufferString(copilotJSON))
	cmd.Flags().Set("client", "github-copilot")

	// Should not error
	err := cmd.Execute()
	if err != nil {
		t.Errorf("Copilot MCP format should parse successfully, got error: %v", err)
	}
}

// TestReportUsageCopilotReportIntentIgnored tests that Copilot's report_intent tool is ignored
// Actual Copilot log: {"toolName":"report_intent","toolArgs":{"intent":"Fixing PR issues"},...}
func TestReportUsageCopilotReportIntentIgnored(t *testing.T) {
	copilotJSON := `{"sessionId":"cf4360fc-8ee6-4e7d-b659-bbf6ae161a3b","timestamp":1772181780807,"cwd":"/home/ines/work/sleuthio/sx","toolName":"report_intent","toolArgs":{"intent":"Fixing PR issues"},"toolResult":{"resultType":"success","textResultForLlm":"Intent logged"}}`

	cmd := NewReportUsageCommand()
	cmd.SetIn(bytes.NewBufferString(copilotJSON))
	cmd.Flags().Set("client", "github-copilot")

	// Should not error (just silently ignores non-asset tools)
	err := cmd.Execute()
	if err != nil {
		t.Errorf("Copilot report_intent should be silently ignored, got error: %v", err)
	}
}

// TestReportUsageGeminiSkillFormat tests Gemini's snake_case JSON format for skills
// Gemini uses same format as Claude Code: {"tool_name":"Skill","tool_input":{...}}
func TestReportUsageGeminiSkillFormat(t *testing.T) {
	geminiJSON := `{"session_id":"b4576730-3466-4e78-877c-6376b070d9a4","transcript_path":"/home/ines/.gemini/tmp/sx/chats/session-2026-02-27T12-34-b4576730.json","cwd":"/home/ines/work/sleuthio/sx","hook_event_name":"AfterTool","timestamp":"2026-02-27T12:46:42.726Z","tool_name":"Skill","tool_input":{"skill":"fix-pr"}}`

	cmd := NewReportUsageCommand()
	cmd.SetIn(bytes.NewBufferString(geminiJSON))
	cmd.Flags().Set("client", "gemini")

	// Should not error (parses Gemini snake_case format)
	err := cmd.Execute()
	if err != nil {
		t.Errorf("Gemini Skill format should parse successfully, got error: %v", err)
	}
}

// TestReportUsageGeminiBuiltinToolIgnored tests that Gemini's built-in tools are ignored
// Actual Gemini log: {"tool_name":"replace","tool_input":{"file_path":"...","new_string":"..."}}
func TestReportUsageGeminiBuiltinToolIgnored(t *testing.T) {
	geminiJSON := `{"session_id":"b4576730-3466-4e78-877c-6376b070d9a4","transcript_path":"/home/ines/.gemini/tmp/sx/chats/session-2026-02-27T12-34-b4576730.json","cwd":"/home/ines/work/sleuthio/sx","hook_event_name":"AfterTool","timestamp":"2026-02-27T12:46:42.726Z","tool_name":"replace","tool_input":{"file_path":"/tmp/test.go","new_string":"test"}}`

	cmd := NewReportUsageCommand()
	cmd.SetIn(bytes.NewBufferString(geminiJSON))
	cmd.Flags().Set("client", "gemini")

	// Should not error (just silently ignores non-asset tools)
	err := cmd.Execute()
	if err != nil {
		t.Errorf("Gemini replace tool should be silently ignored, got error: %v", err)
	}
}

// TestReportUsageGeminiReadFileIgnored tests that Gemini's read_file tool is ignored
// Actual Gemini log: {"tool_name":"read_file","tool_input":{"file_path":"..."}}
func TestReportUsageGeminiReadFileIgnored(t *testing.T) {
	geminiJSON := `{"session_id":"b4576730-3466-4e78-877c-6376b070d9a4","transcript_path":"/home/ines/.gemini/tmp/sx/chats/session-2026-02-27T12-34-b4576730.json","cwd":"/home/ines/work/sleuthio/sx","hook_event_name":"AfterTool","timestamp":"2026-02-27T12:46:49.361Z","tool_name":"read_file","tool_input":{"file_path":"internal/commands/init.go"}}`

	cmd := NewReportUsageCommand()
	cmd.SetIn(bytes.NewBufferString(geminiJSON))
	cmd.Flags().Set("client", "gemini")

	// Should not error (just silently ignores non-asset tools)
	err := cmd.Execute()
	if err != nil {
		t.Errorf("Gemini read_file tool should be silently ignored, got error: %v", err)
	}
}

func TestMain(m *testing.M) {
	// Run tests
	os.Exit(m.Run())
}
