package detectors

import (
	"slices"
	"strings"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/metadata"
)

// MCPDetector detects MCP server assets
type MCPDetector struct{}

// Compile-time interface checks
var (
	_ AssetTypeDetector = (*MCPDetector)(nil)
	_ UsageDetector     = (*MCPDetector)(nil)
)

// DetectType returns true if files indicate this is an MCP asset
func (h *MCPDetector) DetectType(files []string) bool {
	return slices.Contains(files, "package.json")
}

// GetType returns the asset type string
func (h *MCPDetector) GetType() string {
	return "mcp"
}

// CreateDefaultMetadata creates default metadata for an MCP
func (h *MCPDetector) CreateDefaultMetadata(name, version string) *metadata.Metadata {
	return &metadata.Metadata{
		MetadataVersion: "1.0",
		Asset: metadata.Asset{
			Name:    name,
			Version: version,
			Type:    asset.TypeMCP,
		},
		MCP: &metadata.MCPConfig{},
	}
}

// DetectUsageFromToolCall detects MCP server usage from tool calls
func (h *MCPDetector) DetectUsageFromToolCall(toolName string, toolInput map[string]any) (string, bool) {
	// Claude Code format: mcp__server__tool (e.g., "mcp__sx__query")
	if strings.HasPrefix(toolName, "mcp__") {
		parts := strings.Split(toolName, "__")
		if len(parts) >= 2 {
			return parts[1], true
		}
		return "", false
	}

	// Copilot format: server-tool (e.g., "sx-query")
	if strings.Contains(toolName, "-") {
		parts := strings.SplitN(toolName, "-", 2)
		if len(parts) == 2 {
			return parts[0], true
		}
	}

	return "", false
}
