package handlers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sleuth-io/skills/internal/metadata"
	"github.com/sleuth-io/skills/internal/utils"
)

// SkillHandler handles skill artifact installation
type SkillHandler struct {
	metadata *metadata.Metadata
}

// NewSkillHandler creates a new skill handler
func NewSkillHandler(meta *metadata.Metadata) *SkillHandler {
	return &SkillHandler{
		metadata: meta,
	}
}

// DetectType returns true if files indicate this is a skill artifact
func (h *SkillHandler) DetectType(files []string) bool {
	for _, file := range files {
		if file == "SKILL.md" || file == "skill.md" {
			return true
		}
	}
	return false
}

// GetType returns the artifact type string
func (h *SkillHandler) GetType() string {
	return "skill"
}

// CreateDefaultMetadata creates default metadata for a skill
func (h *SkillHandler) CreateDefaultMetadata(name, version string) *metadata.Metadata {
	return &metadata.Metadata{
		MetadataVersion: "1.0",
		Artifact: metadata.Artifact{
			Name:    name,
			Version: version,
			Type:    "skill",
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
		return fmt.Errorf("skill configuration missing")
	}
	if meta.Skill.PromptFile == "" {
		return fmt.Errorf("skill prompt-file is required")
	}
	return nil
}

// DetectUsageFromToolCall detects skill usage from tool calls
func (h *SkillHandler) DetectUsageFromToolCall(toolName string, toolInput map[string]interface{}) (string, bool) {
	if toolName != "Skill" {
		return "", false
	}
	skillName, ok := toolInput["skill"].(string)
	return skillName, ok
}

// Install extracts and installs the skill artifact
func (h *SkillHandler) Install(ctx context.Context, zipData []byte, targetBase string) error {
	// Validate zip structure
	if err := h.Validate(zipData); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	// Determine installation path
	installPath := filepath.Join(targetBase, h.GetInstallPath())

	// Remove existing installation if present
	if utils.IsDirectory(installPath) {
		if err := os.RemoveAll(installPath); err != nil {
			return fmt.Errorf("failed to remove existing installation: %w", err)
		}
	}

	// Create installation directory
	if err := utils.EnsureDir(installPath); err != nil {
		return fmt.Errorf("failed to create installation directory: %w", err)
	}

	// Extract zip to installation directory
	if err := utils.ExtractZip(zipData, installPath); err != nil {
		return fmt.Errorf("failed to extract zip: %w", err)
	}

	return nil
}

// Remove uninstalls the skill artifact
func (h *SkillHandler) Remove(ctx context.Context, targetBase string) error {
	installPath := filepath.Join(targetBase, h.GetInstallPath())

	if !utils.IsDirectory(installPath) {
		// Already removed or never installed
		return nil
	}

	if err := os.RemoveAll(installPath); err != nil {
		return fmt.Errorf("failed to remove skill: %w", err)
	}

	return nil
}

// GetInstallPath returns the installation path relative to targetBase
func (h *SkillHandler) GetInstallPath() string {
	return filepath.Join("skills", h.metadata.Artifact.Name)
}

// Validate checks if the zip structure is valid for a skill artifact
func (h *SkillHandler) Validate(zipData []byte) error {
	// List files in zip
	files, err := utils.ListZipFiles(zipData)
	if err != nil {
		return fmt.Errorf("failed to list zip files: %w", err)
	}

	// Check that metadata.toml exists
	if !containsFile(files, "metadata.toml") {
		return fmt.Errorf("metadata.toml not found in zip")
	}

	// Extract and validate metadata
	metadataBytes, err := utils.ReadZipFile(zipData, "metadata.toml")
	if err != nil {
		return fmt.Errorf("failed to read metadata.toml: %w", err)
	}

	meta, err := metadata.Parse(metadataBytes)
	if err != nil {
		return fmt.Errorf("failed to parse metadata: %w", err)
	}

	// Validate metadata with file list
	if err := meta.ValidateWithFiles(files); err != nil {
		return fmt.Errorf("metadata validation failed: %w", err)
	}

	// Verify artifact type matches
	if meta.Artifact.Type != "skill" {
		return fmt.Errorf("artifact type mismatch: expected skill, got %s", meta.Artifact.Type)
	}

	// Check that prompt file exists
	if meta.Skill == nil {
		return fmt.Errorf("[skill] section missing in metadata")
	}

	if !containsFile(files, meta.Skill.PromptFile) {
		return fmt.Errorf("prompt file not found in zip: %s", meta.Skill.PromptFile)
	}

	return nil
}

// CanDetectInstalledState returns true since skills preserve metadata.toml
func (h *SkillHandler) CanDetectInstalledState() bool {
	return true
}

// ScanInstalled scans for installed skill artifacts in the target directory
func (h *SkillHandler) ScanInstalled(targetBase string) ([]InstalledArtifactInfo, error) {
	var artifacts []InstalledArtifactInfo

	skillsPath := filepath.Join(targetBase, "skills")
	if !utils.IsDirectory(skillsPath) {
		return artifacts, nil
	}

	dirs, err := os.ReadDir(skillsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read skills directory: %w", err)
	}

	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}

		metaPath := filepath.Join(skillsPath, dir.Name(), "metadata.toml")
		meta, err := metadata.ParseFile(metaPath)
		if err != nil {
			continue // Skip if can't parse
		}

		// Only include if it's actually a skill (not agent which also uses skills/ dir)
		if meta.Artifact.Type != "skill" {
			continue
		}

		artifacts = append(artifacts, InstalledArtifactInfo{
			Name:        meta.Artifact.Name,
			Version:     meta.Artifact.Version,
			Type:        meta.Artifact.Type,
			InstallPath: filepath.Join("skills", dir.Name()),
		})
	}

	return artifacts, nil
}

// containsFile checks if a file exists in the file list
func containsFile(files []string, filename string) bool {
	for _, f := range files {
		if f == filename {
			return true
		}
	}
	return false
}
