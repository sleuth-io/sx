package detectors

import (
	"strings"

	"github.com/sleuth-io/skills/internal/asset"
	"github.com/sleuth-io/skills/internal/metadata"
)

// MCPHandler handles MCP server artifact installation
type MCPDetector struct{}

// Compile-time interface checks
var (
	_ ArtifactTypeDetector = (*MCPDetector)(nil)
	_ UsageDetector        = (*MCPDetector)(nil)
)

// DetectType returns true if files indicate this is an MCP artifact
func (h *MCPDetector) DetectType(files []string) bool {
	for _, file := range files {
		if file == "package.json" {
			return true
		}
	}
	return false
}

// GetType returns the artifact type string
func (h *MCPDetector) GetType() string {
	return "mcp"
}

// CreateDefaultMetadata creates default metadata for an MCP
func (h *MCPDetector) CreateDefaultMetadata(name, version string) *metadata.Metadata {
	return &metadata.Metadata{
		MetadataVersion: "1.0",
		Artifact: metadata.Artifact{
			Name:    name,
			Version: version,
			Type:    asset.TypeMCP,
		},
		MCP: &metadata.MCPConfig{},
	}
}

// DetectUsageFromToolCall detects MCP server usage from tool calls
func (h *MCPDetector) DetectUsageFromToolCall(toolName string, toolInput map[string]interface{}) (string, bool) {
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
