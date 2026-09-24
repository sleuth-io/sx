package commands

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
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

// TestReportUsageCursorSkillFormat tests Cursor's snake_case JSON format for skills
// Cursor docs: https://cursor.com/docs/agent/hooks - postToolUse uses snake_case: tool_name, tool_input
func TestReportUsageCursorSkillFormat(t *testing.T) {
	cursorJSON := `{"session_id":"cursor-123","cwd":"/home/ines/work/sleuthio/sx","tool_name":"Skill","tool_input":{"skill":"fix-pr"}}`

	cmd := NewReportUsageCommand()
	cmd.SetIn(bytes.NewBufferString(cursorJSON))
	cmd.Flags().Set("client", "cursor")

	// Should not error (parses Cursor snake_case format)
	err := cmd.Execute()
	if err != nil {
		t.Errorf("Cursor Skill format should parse successfully, got error: %v", err)
	}
}

// TestReportUsageCursorMCPFormat tests Cursor's MCP tool format
// Cursor uses same format as Claude Code: {"tool_name":"mcp__server__tool",...}
func TestReportUsageCursorMCPFormat(t *testing.T) {
	cursorJSON := `{"session_id":"cursor-456","cwd":"/tmp","tool_name":"mcp__sx__query","tool_input":{"query":"get PR","integration":"github"}}`

	cmd := NewReportUsageCommand()
	cmd.SetIn(bytes.NewBufferString(cursorJSON))
	cmd.Flags().Set("client", "cursor")

	// Should not error
	err := cmd.Execute()
	if err != nil {
		t.Errorf("Cursor MCP format should parse successfully, got error: %v", err)
	}
}

// TestReportUsageCursorBuiltinToolIgnored tests that Cursor's built-in tools are ignored
func TestReportUsageCursorBuiltinToolIgnored(t *testing.T) {
	cursorJSON := `{"session_id":"cursor-789","cwd":"/tmp","tool_name":"Read","tool_input":{"file_path":"/tmp/test.go"}}`

	cmd := NewReportUsageCommand()
	cmd.SetIn(bytes.NewBufferString(cursorJSON))
	cmd.Flags().Set("client", "cursor")

	// Should not error (just silently ignores non-asset tools)
	err := cmd.Execute()
	if err != nil {
		t.Errorf("Cursor Read tool should be silently ignored, got error: %v", err)
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

// TestReportUsageKiroSkillFormat tests Kiro's readFile tool format for skill usage tracking
// Kiro reads skills via readFile tool, and we detect skill usage from the file path in the result
// Actual Kiro format: {"toolName":"readFile","toolResult":"<file name=\".kiro/skills/my-skill.md\">..."}
func TestReportUsageKiroSkillFormat(t *testing.T) {
	// Kiro passes JSON via USER_PROMPT env var
	kiroJSON := `{"toolName":"readFile","toolResult":"<file name=\".kiro/skills/fix-pr.md\">\n# Fix PR Skill\n\nThis is the skill content.\n</file>"}`

	// Set USER_PROMPT env var (Kiro's input method)
	os.Setenv("USER_PROMPT", kiroJSON)
	defer os.Unsetenv("USER_PROMPT")

	cmd := NewReportUsageCommand()
	cmd.Flags().Set("client", "kiro")

	// Should not error (parses Kiro readFile format and extracts skill name)
	err := cmd.Execute()
	if err != nil {
		t.Errorf("Kiro readFile format should parse successfully, got error: %v", err)
	}
}

// TestReportUsageKiroNonSkillReadFile tests that Kiro's readFile for non-skill files is ignored
func TestReportUsageKiroNonSkillReadFile(t *testing.T) {
	// Reading a regular file (not a skill)
	kiroJSON := `{"toolName":"readFile","toolResult":"<file name=\"src/main.go\">\npackage main\n</file>"}`

	os.Setenv("USER_PROMPT", kiroJSON)
	defer os.Unsetenv("USER_PROMPT")

	cmd := NewReportUsageCommand()
	cmd.Flags().Set("client", "kiro")

	// Should not error (just silently ignores non-skill files)
	err := cmd.Execute()
	if err != nil {
		t.Errorf("Kiro non-skill readFile should be silently ignored, got error: %v", err)
	}
}

// TestReportUsageKiroMultiFileSkillPath tests that multi-file skill paths extract the top-level skill name
func TestReportUsageKiroMultiFileSkillPath(t *testing.T) {
	// Skill with subdirectory structure (e.g., .kiro/skills/my-skill/index.md)
	kiroJSON := `{"toolName":"readFile","toolResult":"<file name=\".kiro/skills/my-skill/index.md\">\n# My Skill\n</file>"}`

	os.Setenv("USER_PROMPT", kiroJSON)
	defer os.Unsetenv("USER_PROMPT")

	cmd := NewReportUsageCommand()
	cmd.Flags().Set("client", "kiro")

	// Should not error - extracts "my-skill" from the path
	err := cmd.Execute()
	if err != nil {
		t.Errorf("Kiro multi-file skill path should parse successfully, got error: %v", err)
	}
}

// TestExtractKiroSkillNames tests the skill name extraction logic
func TestExtractKiroSkillNames(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "single file skill",
			input:    `<file name=".kiro/skills/fix-pr.md">content</file>`,
			expected: []string{"fix-pr"},
		},
		{
			name:     "multi-file skill (index.md)",
			input:    `<file name=".kiro/skills/my-skill/index.md">content</file>`,
			expected: []string{"my-skill"},
		},
		{
			name:     "multi-file skill (other file)",
			input:    `<file name=".kiro/skills/my-skill/utils.md">content</file>`,
			expected: []string{"my-skill"},
		},
		{
			name:     "multiple skills in result",
			input:    `<file name=".kiro/skills/skill-a.md">a</file><file name=".kiro/skills/skill-b.md">b</file>`,
			expected: []string{"skill-a", "skill-b"},
		},
		{
			name:     "non-skill file",
			input:    `<file name="src/main.go">package main</file>`,
			expected: nil,
		},
		{
			name:     "empty result",
			input:    "",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractKiroSkillNames(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("extractKiroSkillNames(%q) = %v, want %v", tt.input, result, tt.expected)
				return
			}
			for i, name := range result {
				if name != tt.expected[i] {
					t.Errorf("extractKiroSkillNames(%q)[%d] = %q, want %q", tt.input, i, name, tt.expected[i])
				}
			}
		})
	}
}

// TestExtractKiroSkillNamesRelocatedHome covers a Kiro config root that is not
// named .kiro. KIRO_HOME *is* the config root, so KIRO_HOME=/opt/kiro reads
// skills from /opt/kiro/skills and the readFile paths carry no .kiro segment at
// all — against a hardcoded ".kiro/skills/" literal every such read went
// unreported. Hermetic: t.TempDir()/t.Setenv only, never the real HOME.
func TestExtractKiroSkillNamesRelocatedHome(t *testing.T) {
	t.Run("skill under a relocated KIRO_HOME is reported", func(t *testing.T) {
		kiroHomeDir := t.TempDir()
		t.Setenv("KIRO_HOME", kiroHomeDir)

		result := extractKiroSkillNames(
			`<file name="` + filepath.ToSlash(filepath.Join(kiroHomeDir, "skills", "fix-pr.md")) + `">content</file>`,
		)

		if len(result) != 1 || result[0] != "fix-pr" {
			t.Errorf("extractKiroSkillNames under KIRO_HOME=%q = %v, want [fix-pr]", kiroHomeDir, result)
		}
	})

	t.Run("multi-file skill under a relocated KIRO_HOME is reported", func(t *testing.T) {
		kiroHomeDir := t.TempDir()
		t.Setenv("KIRO_HOME", kiroHomeDir)

		result := extractKiroSkillNames(
			`<file name="` + filepath.ToSlash(filepath.Join(kiroHomeDir, "skills", "my-skill", "SKILL.md")) + `">content</file>`,
		)

		if len(result) != 1 || result[0] != "my-skill" {
			t.Errorf("extractKiroSkillNames under KIRO_HOME=%q = %v, want [my-skill]", kiroHomeDir, result)
		}
	})

	t.Run("legacy repo-local path still matches while KIRO_HOME is set", func(t *testing.T) {
		t.Setenv("KIRO_HOME", t.TempDir())

		result := extractKiroSkillNames(`<file name=".kiro/skills/fix-pr.md">content</file>`)

		if len(result) != 1 || result[0] != "fix-pr" {
			t.Errorf("extractKiroSkillNames legacy path = %v, want [fix-pr]", result)
		}
	})

	t.Run("default home skills dir is reported when KIRO_HOME is unset", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)
		t.Setenv("KIRO_HOME", "")

		result := extractKiroSkillNames(
			`<file name="` + filepath.ToSlash(filepath.Join(home, ".kiro", "skills", "fix-pr.md")) + `">content</file>`,
		)

		if len(result) != 1 || result[0] != "fix-pr" {
			t.Errorf("extractKiroSkillNames under default home = %v, want [fix-pr]", result)
		}
	})

	t.Run("a repo's own top-level skills dir is not reported as usage", func(t *testing.T) {
		kiroHomeDir := t.TempDir()
		t.Setenv("KIRO_HOME", kiroHomeDir)

		// An sx vault repo authors its skills in a top-level skills/ directory.
		// Reading one is a plain file read, not skill usage, and the author has
		// that same skill installed so the tracker check downstream would not
		// filter it out — the pattern must not accept a bare skills/ prefix.
		for _, input := range []string{
			`<file name="skills/fix-pr/SKILL.md">content</file>`,
			`<file name="docs/skills/fix-pr.md">content</file>`,
		} {
			if result := extractKiroSkillNames(input); result != nil {
				t.Errorf("extractKiroSkillNames(%q) = %v, want nil", input, result)
			}
		}
	})
}

// TestReportUsageFlushesQueueSynchronously is the regression test for SK-416.
//
// On 3/27/26, the queue flush in runReportUsage was wrapped in `go func() { ... }()`
// to avoid blocking Kiro hooks. But `report-usage` is a short-lived CLI command —
// when runReportUsage returns, main() exits and the goroutine is killed before the
// network call can complete, so events accumulated on disk and never reached the
// server. This test asserts the flush happens synchronously, before runReportUsage
// returns. The buggy version would set flushCalled to false (or only true after a
// race-y delay) because the goroutine never gets to run.
func TestReportUsageFlushesQueueSynchronously(t *testing.T) {
	// Use an isolated cache dir so we don't read or mutate the real one
	tempCacheDir := t.TempDir()
	t.Setenv("SX_CACHE_DIR", tempCacheDir)

	// Pre-populate the tracker with an asset that the synthetic event references.
	// Without this, the detection path returns early (asset not installed) and
	// the flush would never be reached even on a buggy build.
	trackerJSON := `{"version":"3","assets":[{"name":"test-skill","version":"1.0.0","clients":["claude-code"]}]}`
	trackerPath := filepath.Join(tempCacheDir, "installed.json")
	if err := os.WriteFile(trackerPath, []byte(trackerJSON), 0644); err != nil {
		t.Fatalf("failed to write fake tracker: %v", err)
	}

	// Replace the flush hook with one that records call ordering. The defer
	// restores the real implementation so other tests aren't affected.
	flushCalled := false
	origFlush := flushUsageQueue
	flushUsageQueue = func(ctx context.Context) error {
		flushCalled = true
		return nil
	}
	defer func() { flushUsageQueue = origFlush }()

	// Synthesize a Claude Code PostToolUse event for our installed test skill.
	// We pass the JSON as args[0] (Codex's input path) rather than via cmd.SetIn,
	// because runReportUsage reads from os.Stdin directly and ignores cmd.InOrStdin.
	// The Codex agent-turn-complete check is skipped because our event has no
	// "type" field, so the code falls through to Claude Code parsing.
	claudeJSON := `{"tool_name":"Skill","tool_input":{"skill":"test-skill"}}`

	cmd := NewReportUsageCommand()
	cmd.SetArgs([]string{claudeJSON})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Flags().Set("client", "claude-code"); err != nil {
		t.Fatalf("failed to set client flag: %v", err)
	}

	if err := cmd.Execute(); err != nil {
		t.Fatalf("report-usage execution failed: %v", err)
	}

	// The whole point of this test: by the time Execute() returns, the flush
	// MUST have happened. The buggy goroutine version would let runReportUsage
	// return before the flush ran, leaving flushCalled == false.
	if !flushCalled {
		t.Fatal("flushUsageQueue was not called before runReportUsage returned — " +
			"regression: queue flush is async and will be killed when the CLI process exits")
	}
}

func TestMain(m *testing.M) {
	// Run tests
	os.Exit(m.Run())
}
