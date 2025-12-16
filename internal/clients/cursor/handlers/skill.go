package handlers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sleuth-io/skills/internal/asset"
	"github.com/sleuth-io/skills/internal/handlers/dirasset"
	"github.com/sleuth-io/skills/internal/metadata"
	"github.com/sleuth-io/skills/internal/utils"
)

var skillOps = dirasset.NewOperations("skills", &asset.TypeSkill)

// SkillHandler handles skill artifact installation for Cursor
// Skills are extracted to .cursor/skills/{name}/ (not transformed to commands)
type SkillHandler struct {
	metadata *metadata.Metadata
}

// NewSkillHandler creates a new skill handler
func NewSkillHandler(meta *metadata.Metadata) *SkillHandler {
	return &SkillHandler{metadata: meta}
}

// Install extracts a skill to .cursor/skills/{name}/
func (h *SkillHandler) Install(ctx context.Context, zipData []byte, targetBase string) error {
	skillsDir := filepath.Join(targetBase, "skills", h.metadata.Artifact.Name)

	// Remove existing installation if present
	if utils.IsDirectory(skillsDir) {
		if err := os.RemoveAll(skillsDir); err != nil {
			return fmt.Errorf("failed to remove existing installation: %w", err)
		}
	}

	// Create installation directory
	if err := utils.EnsureDir(skillsDir); err != nil {
		return fmt.Errorf("failed to create installation directory: %w", err)
	}

	// Extract entire zip to skills directory
	if err := utils.ExtractZip(zipData, skillsDir); err != nil {
		return fmt.Errorf("failed to extract skill: %w", err)
	}

	return nil
}

// Remove removes a skill from .cursor/skills/
func (h *SkillHandler) Remove(ctx context.Context, targetBase string) error {
	skillsDir := filepath.Join(targetBase, "skills", h.metadata.Artifact.Name)

	if !utils.IsDirectory(skillsDir) {
		// Already removed or never installed
		return nil
	}

	if err := os.RemoveAll(skillsDir); err != nil {
		return fmt.Errorf("failed to remove skill: %w", err)
	}

	return nil
}

// VerifyInstalled checks if the skill is properly installed
func (h *SkillHandler) VerifyInstalled(targetBase string) (bool, string) {
	return skillOps.VerifyInstalled(targetBase, h.metadata.Artifact.Name, h.metadata.Artifact.Version)
}
