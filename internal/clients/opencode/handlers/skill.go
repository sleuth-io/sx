package handlers

import (
	"context"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/handlers/dirasset"
	"github.com/sleuth-io/sx/internal/metadata"
)

// SkillOps provides directory-based operations for OpenCode skills.
// OpenCode discovers skills natively from <config>/skills/<name>/SKILL.md.
var SkillOps = dirasset.NewOperations(DirSkills, &asset.TypeSkill)

// SkillHandler installs skills into an OpenCode config directory.
type SkillHandler struct {
	metadata *metadata.Metadata
}

// NewSkillHandler creates a new OpenCode skill handler.
func NewSkillHandler(meta *metadata.Metadata) *SkillHandler {
	return &SkillHandler{metadata: meta}
}

// Install extracts the skill into <targetBase>/skills/<name>/.
func (h *SkillHandler) Install(ctx context.Context, zipData []byte, targetBase string) error {
	return SkillOps.Install(ctx, zipData, targetBase, h.metadata.Asset.Name)
}

// Remove deletes the skill directory.
func (h *SkillHandler) Remove(ctx context.Context, targetBase string) error {
	return SkillOps.Remove(ctx, targetBase, h.metadata.Asset.Name)
}

// VerifyInstalled checks if the skill is installed with the expected version.
func (h *SkillHandler) VerifyInstalled(targetBase string) (bool, string) {
	return SkillOps.VerifyInstalled(targetBase, h.metadata.Asset.Name, h.metadata.Asset.Version)
}
