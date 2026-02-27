package handlers

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/metadata"
)

func TestCodexSkillHandler_Install(t *testing.T) {
	targetBase := t.TempDir()

	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name:    "test-skill",
			Version: "1.0.0",
			Type:    asset.TypeSkill,
		},
		Skill: &metadata.SkillConfig{
			PromptFile: "SKILL.md",
		},
	}

	zipData := createTestZip(t, map[string]string{
		"metadata.toml": `[asset]
name = "test-skill"
version = "1.0.0"
type = "skill"

[skill]
prompt-file = "SKILL.md"
`,
		"SKILL.md": "# Test Skill\n\nThis is a test skill.",
	})

	handler := NewSkillHandler(meta)
	if err := handler.Install(context.Background(), zipData, targetBase); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	// Verify skill directory was created
	skillDir := filepath.Join(targetBase, DirSkills, "test-skill")
	if _, err := os.Stat(skillDir); os.IsNotExist(err) {
		t.Error("Skill directory should exist")
	}

	// Verify SKILL.md exists
	skillFile := filepath.Join(skillDir, "SKILL.md")
	if _, err := os.Stat(skillFile); os.IsNotExist(err) {
		t.Error("SKILL.md should exist")
	}

	// Verify metadata.toml exists
	metaFile := filepath.Join(skillDir, "metadata.toml")
	if _, err := os.Stat(metaFile); os.IsNotExist(err) {
		t.Error("metadata.toml should exist")
	}
}

func TestCodexSkillHandler_Remove(t *testing.T) {
	targetBase := t.TempDir()

	// Create skill directory manually
	skillDir := filepath.Join(targetBase, DirSkills, "test-skill")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatalf("Failed to create skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to write SKILL.md: %v", err)
	}

	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name:    "test-skill",
			Version: "1.0.0",
			Type:    asset.TypeSkill,
		},
	}

	handler := NewSkillHandler(meta)
	if err := handler.Remove(context.Background(), targetBase); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	// Verify skill directory was removed
	if _, err := os.Stat(skillDir); !os.IsNotExist(err) {
		t.Error("Skill directory should be removed")
	}
}

func TestCodexSkillHandler_VerifyInstalled(t *testing.T) {
	targetBase := t.TempDir()

	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name:    "test-skill",
			Version: "1.0.0",
			Type:    asset.TypeSkill,
		},
	}

	handler := NewSkillHandler(meta)

	// Not installed
	installed, _ := handler.VerifyInstalled(targetBase)
	if installed {
		t.Error("Should not be installed initially")
	}

	// Create skill directory with metadata
	skillDir := filepath.Join(targetBase, DirSkills, "test-skill")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	metaContent := `[asset]
name = "test-skill"
version = "1.0.0"
type = "skill"
`
	if err := os.WriteFile(filepath.Join(skillDir, "metadata.toml"), []byte(metaContent), 0644); err != nil {
		t.Fatal(err)
	}

	installed, msg := handler.VerifyInstalled(targetBase)
	if !installed {
		t.Errorf("Should be installed: %s", msg)
	}
}

func TestCodexSkillHandler_VerifyInstalled_WrongVersion(t *testing.T) {
	targetBase := t.TempDir()

	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name:    "test-skill",
			Version: "2.0.0", // Looking for version 2.0.0
			Type:    asset.TypeSkill,
		},
	}

	handler := NewSkillHandler(meta)

	// Create skill with version 1.0.0
	skillDir := filepath.Join(targetBase, DirSkills, "test-skill")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	metaContent := `[asset]
name = "test-skill"
version = "1.0.0"
type = "skill"
`
	if err := os.WriteFile(filepath.Join(skillDir, "metadata.toml"), []byte(metaContent), 0644); err != nil {
		t.Fatal(err)
	}

	installed, msg := handler.VerifyInstalled(targetBase)
	if installed {
		t.Errorf("Should detect version mismatch: %s", msg)
	}
}

func TestCodexSkillHandler_InstallWithReferences(t *testing.T) {
	targetBase := t.TempDir()

	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name:    "complex-skill",
			Version: "1.0.0",
			Type:    asset.TypeSkill,
		},
		Skill: &metadata.SkillConfig{
			PromptFile: "SKILL.md",
		},
	}

	zipData := createTestZip(t, map[string]string{
		"metadata.toml": `[asset]
name = "complex-skill"
version = "1.0.0"
type = "skill"

[skill]
prompt-file = "SKILL.md"
`,
		"SKILL.md":             "# Complex Skill\n\nSee references/guide.md",
		"references/guide.md":  "# Guide\n\nDetailed instructions.",
		"references/helper.sh": "#!/bin/bash\necho hello",
	})

	handler := NewSkillHandler(meta)
	if err := handler.Install(context.Background(), zipData, targetBase); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	// Verify all files exist
	skillDir := filepath.Join(targetBase, DirSkills, "complex-skill")
	files := []string{
		"SKILL.md",
		"metadata.toml",
		"references/guide.md",
		"references/helper.sh",
	}
	for _, f := range files {
		path := filepath.Join(skillDir, f)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("File should exist: %s", f)
		}
	}
}

func TestCodexSkillHandler_MultipleSKills(t *testing.T) {
	targetBase := t.TempDir()

	// Install first skill
	meta1 := &metadata.Metadata{
		Asset: metadata.Asset{Name: "skill-a", Version: "1.0.0", Type: asset.TypeSkill},
		Skill: &metadata.SkillConfig{PromptFile: "SKILL.md"},
	}
	zip1 := createTestZip(t, map[string]string{
		"metadata.toml": `[asset]
name = "skill-a"
version = "1.0.0"
type = "skill"

[skill]
prompt-file = "SKILL.md"
`,
		"SKILL.md": "Skill A",
	})
	handler1 := NewSkillHandler(meta1)
	if err := handler1.Install(context.Background(), zip1, targetBase); err != nil {
		t.Fatalf("Install skill-a failed: %v", err)
	}

	// Install second skill
	meta2 := &metadata.Metadata{
		Asset: metadata.Asset{Name: "skill-b", Version: "1.0.0", Type: asset.TypeSkill},
		Skill: &metadata.SkillConfig{PromptFile: "SKILL.md"},
	}
	zip2 := createTestZip(t, map[string]string{
		"metadata.toml": `[asset]
name = "skill-b"
version = "1.0.0"
type = "skill"

[skill]
prompt-file = "SKILL.md"
`,
		"SKILL.md": "Skill B",
	})
	handler2 := NewSkillHandler(meta2)
	if err := handler2.Install(context.Background(), zip2, targetBase); err != nil {
		t.Fatalf("Install skill-b failed: %v", err)
	}

	// Both should exist
	if _, err := os.Stat(filepath.Join(targetBase, DirSkills, "skill-a")); os.IsNotExist(err) {
		t.Error("skill-a should exist")
	}
	if _, err := os.Stat(filepath.Join(targetBase, DirSkills, "skill-b")); os.IsNotExist(err) {
		t.Error("skill-b should exist")
	}

	// Remove first, second should remain
	if err := handler1.Remove(context.Background(), targetBase); err != nil {
		t.Fatalf("Remove skill-a failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(targetBase, DirSkills, "skill-a")); !os.IsNotExist(err) {
		t.Error("skill-a should be removed")
	}
	if _, err := os.Stat(filepath.Join(targetBase, DirSkills, "skill-b")); os.IsNotExist(err) {
		t.Error("skill-b should still exist")
	}
}
