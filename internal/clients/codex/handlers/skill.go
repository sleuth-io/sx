package handlers

import (
	"context"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/handlers/dirasset"
	"github.com/sleuth-io/sx/internal/metadata"
)

// SkillOps provides directory-based operations for skill assets
var SkillOps = dirasset.NewOperations(DirSkills, &asset.TypeSkill)

// SkillHandler handles skill asset installation for Codex
type SkillHandler struct {
	metadata *metadata.Metadata
}

// NewSkillHandler creates a new skill handler
func NewSkillHandler(meta *metadata.Metadata) *SkillHandler {
	return &SkillHandler{
		metadata: meta,
	}
}

// Install extracts and installs the skill asset
func (h *SkillHandler) Install(ctx context.Context, zipData []byte, targetBase string) error {
	return SkillOps.Install(ctx, zipData, targetBase, h.metadata.Asset.Name)
}

// Remove uninstalls the skill asset
func (h *SkillHandler) Remove(ctx context.Context, targetBase string) error {
	return SkillOps.Remove(ctx, targetBase, h.metadata.Asset.Name)
}

// VerifyInstalled checks if the skill is properly installed
func (h *SkillHandler) VerifyInstalled(targetBase string) (bool, string) {
	return SkillOps.VerifyInstalled(targetBase, h.metadata.Asset.Name, h.metadata.Asset.Version)
}
