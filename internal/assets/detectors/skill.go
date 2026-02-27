package detectors

import (
	"strings"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/metadata"
)

// SkillDetector detects skill assets
type SkillDetector struct{}

// Compile-time interface checks
var (
	_ AssetTypeDetector = (*SkillDetector)(nil)
	_ UsageDetector     = (*SkillDetector)(nil)
)

// DetectType returns true if files indicate this is a skill asset
func (h *SkillDetector) DetectType(files []string) bool {
	for _, file := range files {
		if file == "SKILL.md" || file == "skill.md" {
			return true
		}
	}
	return false
}

// GetType returns the asset type string
func (h *SkillDetector) GetType() string {
	return "skill"
}

// CreateDefaultMetadata creates default metadata for a skill
func (h *SkillDetector) CreateDefaultMetadata(name, version string) *metadata.Metadata {
	return &metadata.Metadata{
		MetadataVersion: "1.0",
		Asset: metadata.Asset{
			Name:    name,
			Version: version,
			Type:    asset.TypeSkill,
		},
		Skill: &metadata.SkillConfig{
			PromptFile: "SKILL.md",
		},
	}
}

// DetectUsageFromToolCall detects skill usage from tool calls
func (h *SkillDetector) DetectUsageFromToolCall(toolName string, toolInput map[string]any) (string, bool) {
	if !strings.EqualFold(toolName, "Skill") {
		return "", false
	}
	skillName, ok := toolInput["skill"].(string)
	return skillName, ok
}
