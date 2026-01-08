package detectors

import (
	"strings"
)

// MCPRemoteDetector detects MCP remote assets
// MCP remote assets contain only configuration, no server code
type MCPRemoteDetector struct{}

// Compile-time interface check
var _ UsageDetector = (*MCPRemoteDetector)(nil)

// DetectUsageFromToolCall detects MCP remote server usage from tool calls
// MCP remote uses the same tool naming pattern as regular MCP, so we use the same logic
func (h *MCPRemoteDetector) DetectUsageFromToolCall(toolName string, toolInput map[string]any) (string, bool) {
	// MCP tools follow pattern: mcp__server__tool
	if !strings.HasPrefix(toolName, "mcp__") {
		return "", false
	}
	// Parse: "mcp__github__list_prs" -> "github"
	parts := strings.Split(toolName, "__")
	if len(parts) < 2 {
		return "", false
	}
	serverName := parts[1]
	return serverName, true
}
