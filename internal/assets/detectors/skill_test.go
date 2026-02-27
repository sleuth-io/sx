package detectors

import "testing"

func TestSkillDetector_DetectUsageFromToolCall(t *testing.T) {
	detector := &SkillDetector{}

	tests := []struct {
		name         string
		toolName     string
		toolInput    map[string]any
		wantAsset    string
		wantDetected bool
	}{
		{
			// Claude Code format: tool_name="Skill" (capital S)
			name:         "Claude Code skill invocation",
			toolName:     "Skill",
			toolInput:    map[string]any{"skill": "fix-pr"},
			wantAsset:    "fix-pr",
			wantDetected: true,
		},
		{
			// Copilot format: toolName="skill" (lowercase)
			// Actual log: {"toolName":"skill","toolArgs":{"skill":"fix-pr"},...}
			name:         "Copilot skill invocation",
			toolName:     "skill",
			toolInput:    map[string]any{"skill": "fix-pr"},
			wantAsset:    "fix-pr",
			wantDetected: true,
		},
		{
			// Copilot format with different skill name
			// Actual log: {"toolName":"skill","toolArgs":{"skill":"add-ai-client-support"},...}
			name:         "Copilot skill with different name",
			toolName:     "skill",
			toolInput:    map[string]any{"skill": "add-ai-client-support"},
			wantAsset:    "add-ai-client-support",
			wantDetected: true,
		},
		{
			// Copilot internal tool - should NOT be detected
			// Actual log: {"toolName":"report_intent","toolArgs":{"intent":"Fixing PR issues"},...}
			name:         "Copilot report_intent not detected",
			toolName:     "report_intent",
			toolInput:    map[string]any{"intent": "Fixing PR issues"},
			wantAsset:    "",
			wantDetected: false,
		},
		{
			name:         "other tool not detected",
			toolName:     "Read",
			toolInput:    map[string]any{"path": "/some/file"},
			wantAsset:    "",
			wantDetected: false,
		},
		{
			name:         "missing skill name in input",
			toolName:     "Skill",
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
