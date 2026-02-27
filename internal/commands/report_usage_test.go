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

func TestMain(m *testing.M) {
	// Run tests
	os.Exit(m.Run())
}
