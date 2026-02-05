package handlers

import (
	"context"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/handlers/dirasset"
	"github.com/sleuth-io/sx/internal/metadata"
)

var skillOps = dirasset.NewOperations("skills", &asset.TypeSkill)

// SkillHandler handles skill asset installation for GitHub Copilot.
// Skills are extracted to {targetBase}/skills/{name}/
type SkillHandler struct {
	metadata *metadata.Metadata
}

// NewSkillHandler creates a new skill handler
func NewSkillHandler(meta *metadata.Metadata) *SkillHandler {
	return &SkillHandler{metadata: meta}
}

// Install extracts a skill to {targetBase}/skills/{name}/
func (h *SkillHandler) Install(ctx context.Context, zipData []byte, targetBase string) error {
	return skillOps.Install(ctx, zipData, targetBase, h.metadata.Asset.Name)
}

// Remove removes a skill from {targetBase}/skills/
func (h *SkillHandler) Remove(ctx context.Context, targetBase string) error {
	return skillOps.Remove(ctx, targetBase, h.metadata.Asset.Name)
}

// VerifyInstalled checks if the skill is properly installed
func (h *SkillHandler) VerifyInstalled(targetBase string) (bool, string) {
	return skillOps.VerifyInstalled(targetBase, h.metadata.Asset.Name, h.metadata.Asset.Version)
}
