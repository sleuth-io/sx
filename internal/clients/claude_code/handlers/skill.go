package handlers

import (
	"context"
	"errors"
	"path/filepath"
	"slices"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/handlers/dirasset"
	"github.com/sleuth-io/sx/internal/metadata"
)

var skillOps = dirasset.NewOperations(DirSkills, &asset.TypeSkill)

// SkillHandler handles skill asset installation
type SkillHandler struct {
	metadata *metadata.Metadata
}

// NewSkillHandler creates a new skill handler
func NewSkillHandler(meta *metadata.Metadata) *SkillHandler {
	return &SkillHandler{
		metadata: meta,
	}
}

// DetectType returns true if files indicate this is a skill asset
func (h *SkillHandler) DetectType(files []string) bool {
	for _, file := range files {
		if file == "SKILL.md" || file == "skill.md" {
			return true
		}
	}
	return false
}

// GetType returns the asset type string
func (h *SkillHandler) GetType() string {
	return "skill"
}

// CreateDefaultMetadata creates default metadata for a skill
func (h *SkillHandler) CreateDefaultMetadata(name, version string) *metadata.Metadata {
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

// GetPromptFile returns the prompt file path for skills
func (h *SkillHandler) GetPromptFile(meta *metadata.Metadata) string {
	if meta.Skill != nil {
		return meta.Skill.PromptFile
	}
	return ""
}

// GetScriptFile returns empty for skills (not applicable)
func (h *SkillHandler) GetScriptFile(meta *metadata.Metadata) string {
	return ""
}

// ValidateMetadata validates skill-specific metadata
func (h *SkillHandler) ValidateMetadata(meta *metadata.Metadata) error {
	if meta.Skill == nil {
		return errors.New("skill configuration missing")
	}
	if meta.Skill.PromptFile == "" {
		return errors.New("skill prompt-file is required")
	}
	return nil
}

// DetectUsageFromToolCall detects skill usage from tool calls
func (h *SkillHandler) DetectUsageFromToolCall(toolName string, toolInput map[string]any) (string, bool) {
	if toolName != "Skill" {
		return "", false
	}
	skillName, ok := toolInput["skill"].(string)
	return skillName, ok
}

// Install extracts and installs the skill asset
func (h *SkillHandler) Install(ctx context.Context, zipData []byte, targetBase string) error {
	return skillOps.Install(ctx, zipData, targetBase, h.metadata.Asset.Name)
}

// Remove uninstalls the skill asset
func (h *SkillHandler) Remove(ctx context.Context, targetBase string) error {
	return skillOps.Remove(ctx, targetBase, h.metadata.Asset.Name)
}

// GetInstallPath returns the installation path relative to targetBase
func (h *SkillHandler) GetInstallPath() string {
	return filepath.Join(DirSkills, h.metadata.Asset.Name)
}

// CanDetectInstalledState returns true since skills preserve metadata.toml
func (h *SkillHandler) CanDetectInstalledState() bool {
	return true
}

// VerifyInstalled checks if the skill is properly installed
func (h *SkillHandler) VerifyInstalled(targetBase string) (bool, string) {
	return skillOps.VerifyInstalled(targetBase, h.metadata.Asset.Name, h.metadata.Asset.Version)
}

// containsFile checks if a file exists in the file list
func containsFile(files []string, filename string) bool {
	return slices.Contains(files, filename)
}
