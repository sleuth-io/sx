package detectors

import (
	"strings"

	"github.com/sleuth-io/sx/internal/asset"
)

// DetectAssetTypeFromPath detects asset type from a single file path and optional content.
// This is for single files (not zip archives). Returns nil if no type can be determined.
func DetectAssetTypeFromPath(path string, content []byte) *asset.Type {
	lower := strings.ToLower(path)

	// Only handle .md files
	if !strings.HasSuffix(lower, ".md") {
		return nil
	}

	// Path-based hints
	if strings.Contains(lower, "/rules/") {
		return &asset.TypeRule
	}
	if strings.Contains(lower, "/skills/") {
		return &asset.TypeSkill
	}
	if strings.Contains(lower, "/agents/") {
		return &asset.TypeAgent
	}
	if strings.Contains(lower, "/commands/") {
		return &asset.TypeCommand
	}

	// Content-based: check YAML frontmatter for agent indicators
	if content != nil && looksLikeAgent(content) {
		return &asset.TypeAgent
	}

	// Default .md files to command
	return &asset.TypeCommand
}

// looksLikeAgent checks if content has YAML frontmatter with agent-specific fields
func looksLikeAgent(content []byte) bool {
	contentStr := string(content)
	if !strings.HasPrefix(contentStr, "---") {
		return false
	}

	lines := strings.Split(contentStr, "\n")
	inFrontmatter := false
	for _, line := range lines {
		if line == "---" {
			if inFrontmatter {
				break
			}
			inFrontmatter = true
			continue
		}
		if inFrontmatter {
			lower := strings.ToLower(line)
			// Agent frontmatter typically has: tools, model, permissionMode
			if strings.HasPrefix(lower, "tools:") ||
				strings.HasPrefix(lower, "model:") ||
				strings.HasPrefix(lower, "permissionmode:") {
				return true
			}
		}
	}
	return false
}
