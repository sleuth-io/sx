package handlers

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/metadata"
)

func createTestZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	w := zip.NewWriter(buf)
	for name, content := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatalf("Failed to create zip entry %q: %v", name, err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			t.Fatalf("Failed to write zip entry %q: %v", name, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Failed to close zip: %v", err)
	}
	return buf.Bytes()
}

func TestOpenCodeSkillHandler_Install(t *testing.T) {
	targetBase := t.TempDir()

	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name:    "demo-skill",
			Version: "1.0.0",
			Type:    asset.TypeSkill,
		},
		Skill: &metadata.SkillConfig{PromptFile: "SKILL.md"},
	}

	zipData := createTestZip(t, map[string]string{
		"metadata.toml": `[asset]
name = "demo-skill"
version = "1.0.0"
type = "skill"

[skill]
prompt-file = "SKILL.md"
`,
		"SKILL.md": "---\nname: demo-skill\ndescription: A test skill.\n---\n\n# Demo Skill",
	})

	h := NewSkillHandler(meta)
	if err := h.Install(context.Background(), zipData, targetBase); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	skillDir := filepath.Join(targetBase, DirSkills, "demo-skill")
	if _, err := os.Stat(filepath.Join(skillDir, "SKILL.md")); err != nil {
		t.Errorf("SKILL.md should exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(skillDir, "metadata.toml")); err != nil {
		t.Errorf("metadata.toml should exist: %v", err)
	}

	installed, msg := h.VerifyInstalled(targetBase)
	if !installed {
		t.Errorf("Should report installed: %s", msg)
	}

	if err := h.Remove(context.Background(), targetBase); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}
	if _, err := os.Stat(skillDir); !os.IsNotExist(err) {
		t.Error("Skill directory should be removed")
	}
}
