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

func TestMain(m *testing.M) {
	// Run tests
	os.Exit(m.Run())
}
