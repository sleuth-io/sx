package detectors

import (
	"github.com/sleuth-io/skills/internal/asset"
	"github.com/sleuth-io/skills/internal/metadata"
)

// SkillDetector detects skill artifacts
type SkillDetector struct{}

// Compile-time interface checks
var (
	_ ArtifactTypeDetector = (*SkillDetector)(nil)
	_ UsageDetector        = (*SkillDetector)(nil)
)

// DetectType returns true if files indicate this is a skill artifact
func (h *SkillDetector) DetectType(files []string) bool {
	for _, file := range files {
		if file == "SKILL.md" || file == "skill.md" {
			return true
		}
	}
	return false
}

// GetType returns the artifact type string
func (h *SkillDetector) GetType() string {
	return "skill"
}

// CreateDefaultMetadata creates default metadata for a skill
func (h *SkillDetector) CreateDefaultMetadata(name, version string) *metadata.Metadata {
	return &metadata.Metadata{
		MetadataVersion: "1.0",
		Artifact: metadata.Artifact{
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
func (h *SkillDetector) DetectUsageFromToolCall(toolName string, toolInput map[string]interface{}) (string, bool) {
	if toolName != "Skill" {
		return "", false
	}
	skillName, ok := toolInput["skill"].(string)
	return skillName, ok
}
