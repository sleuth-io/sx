package handlers

import (
	"context"

	"github.com/sleuth-io/sx/v2/internal/asset"
	"github.com/sleuth-io/sx/v2/internal/handlers/dirasset"
	"github.com/sleuth-io/sx/v2/internal/metadata"
)

// SkillOps is the shared dirasset operations for KiroCrew skills.
// Reused verbatim from the kiro client — the only difference between the two
// clients is the install root (crew home vs. ~/.kiro), not the extraction
// engine or the on-disk skill layout.
var SkillOps = dirasset.NewOperations(DirSkills, &asset.TypeSkill)

// SkillHandler handles skill asset installation for KiroCrew.
// Skills are extracted to {crewHome}/skills/{name}/.
type SkillHandler struct {
	metadata *metadata.Metadata
}

// NewSkillHandler creates a new skill handler
func NewSkillHandler(meta *metadata.Metadata) *SkillHandler {
	return &SkillHandler{metadata: meta}
}

// Install extracts a skill to {crewHome}/skills/{name}/
func (h *SkillHandler) Install(ctx context.Context, zipData []byte, targetBase string) error {
	return SkillOps.Install(ctx, zipData, targetBase, h.metadata.Asset.Name)
}

// Remove removes a skill from {crewHome}/skills/
func (h *SkillHandler) Remove(ctx context.Context, targetBase string) error {
	return SkillOps.Remove(ctx, targetBase, h.metadata.Asset.Name)
}

// VerifyInstalled checks if the skill is properly installed
func (h *SkillHandler) VerifyInstalled(targetBase string) (bool, string) {
	return SkillOps.VerifyInstalled(targetBase, h.metadata.Asset.Name, h.metadata.Asset.Version)
}
