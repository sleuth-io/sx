package detectors

import "testing"

func TestMCPDetector_DetectUsageFromToolCall(t *testing.T) {
	detector := &MCPDetector{}

	tests := []struct {
		name         string
		toolName     string
		toolInput    map[string]any
		wantAsset    string
		wantDetected bool
	}{
		{
			// Claude Code format: mcp__server__tool
			name:         "Claude Code MCP tool",
			toolName:     "mcp__sx__query",
			toolInput:    map[string]any{"query": "get PR"},
			wantAsset:    "sx",
			wantDetected: true,
		},
		{
			// Claude Code format with different server
			name:         "Claude Code MCP github tool",
			toolName:     "mcp__github__list_prs",
			toolInput:    map[string]any{},
			wantAsset:    "github",
			wantDetected: true,
		},
		{
			// Copilot format: server-tool
			// Actual log: {"toolName":"sx-query",...}
			name:         "Copilot MCP tool sx-query",
			toolName:     "sx-query",
			toolInput:    map[string]any{"query": "get PR"},
			wantAsset:    "sx",
			wantDetected: true,
		},
		{
			// Copilot format with different server
			name:         "Copilot MCP tool github-list_prs",
			toolName:     "github-list_prs",
			toolInput:    map[string]any{},
			wantAsset:    "github",
			wantDetected: true,
		},
		{
			// Should NOT match - no mcp__ prefix and no hyphen
			name:         "plain tool name not detected",
			toolName:     "Read",
			toolInput:    map[string]any{},
			wantAsset:    "",
			wantDetected: false,
		},
		{
			// Edge case: leading hyphen should not be detected
			name:         "leading hyphen not detected",
			toolName:     "-query",
			toolInput:    map[string]any{},
			wantAsset:    "",
			wantDetected: false,
		},
		{
			// Should NOT match - Copilot internal tool
			name:         "report_intent not detected",
			toolName:     "report_intent",
			toolInput:    map[string]any{"intent": "Fixing PR"},
			wantAsset:    "",
			wantDetected: false,
		},
		{
			// Edge case: mcp__ prefix but no server name - should not be detected
			name:         "mcp__ prefix without server",
			toolName:     "mcp__",
			toolInput:    map[string]any{},
			wantAsset:    "",
			wantDetected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			asset, detected := detector.DetectUsageFromToolCall(tt.toolName, tt.toolInput)
			if detected != tt.wantDetected {
				t.Errorf("DetectUsageFromToolCall() detected = %v, want %v", detected, tt.wantDetected)
			}
			if asset != tt.wantAsset {
				t.Errorf("DetectUsageFromToolCall() asset = %q, want %q", asset, tt.wantAsset)
			}
		})
	}
}
