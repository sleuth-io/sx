package commands

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/metadata"
	"github.com/sleuth-io/sx/internal/utils"
)

// makeZip is a tiny helper that produces a zip containing the given files.
func makeZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	w := zip.NewWriter(buf)
	for name, content := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatalf("create %q: %v", name, err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			t.Fatalf("write %q: %v", name, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

// TestNormalizePromptFileCase covers the canonicalization step that fixes
// issue #138 — a skill authored with lowercase "skill.md" (which Claude Code
// permits) is renamed to the spec's canonical "SKILL.md" so every client gets
// a filename it recognizes and metadata validation succeeds.
func TestNormalizePromptFileCase(t *testing.T) {
	tests := []struct {
		name        string
		assetType   asset.Type
		input       map[string]string
		wantPresent []string
		wantAbsent  []string
	}{
		{
			name:        "skill.md -> SKILL.md",
			assetType:   asset.TypeSkill,
			input:       map[string]string{"skill.md": "body", "README.md": "rm"},
			wantPresent: []string{"SKILL.md", "README.md"},
			wantAbsent:  []string{"skill.md"},
		},
		{
			name:        "Skill.Md -> SKILL.md (mixed case)",
			assetType:   asset.TypeSkill,
			input:       map[string]string{"Skill.Md": "body"},
			wantPresent: []string{"SKILL.md"},
			wantAbsent:  []string{"Skill.Md"},
		},
		{
			name:        "SKILL.md untouched",
			assetType:   asset.TypeSkill,
			input:       map[string]string{"SKILL.md": "body"},
			wantPresent: []string{"SKILL.md"},
		},
		{
			name:        "agent.md -> AGENT.md",
			assetType:   asset.TypeAgent,
			input:       map[string]string{"agent.md": "body"},
			wantPresent: []string{"AGENT.md"},
			wantAbsent:  []string{"agent.md"},
		},
		{
			name:        "command.md -> COMMAND.md",
			assetType:   asset.TypeCommand,
			input:       map[string]string{"command.md": "body"},
			wantPresent: []string{"COMMAND.md"},
			wantAbsent:  []string{"command.md"},
		},
		{
			name:        "rule.md -> RULE.md",
			assetType:   asset.TypeRule,
			input:       map[string]string{"rule.md": "body"},
			wantPresent: []string{"RULE.md"},
			wantAbsent:  []string{"rule.md"},
		},
		{
			name:        "non-prompt asset type is no-op",
			assetType:   asset.TypeMCP,
			input:       map[string]string{"server.js": "x"},
			wantPresent: []string{"server.js"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			zipData := makeZip(t, tt.input)

			got, err := normalizePromptFileCase(zipData, tt.assetType)
			if err != nil {
				t.Fatalf("normalizePromptFileCase: %v", err)
			}

			names, err := utils.ListZipFiles(got)
			if err != nil {
				t.Fatalf("ListZipFiles: %v", err)
			}
			present := map[string]bool{}
			for _, n := range names {
				present[n] = true
			}
			for _, want := range tt.wantPresent {
				if !present[want] {
					t.Errorf("expected %q in zip, got %v", want, names)
				}
			}
			for _, dontWant := range tt.wantAbsent {
				if present[dontWant] {
					t.Errorf("expected %q absent from zip, got %v", dontWant, names)
				}
			}
		})
	}
}

// TestNormalizePromptFileCase_RewritesMetadata covers the user-authored
// metadata.toml path: a metadata.toml that declares `prompt-file = "skill.md"`
// must be rewritten to the canonical "SKILL.md" so file and metadata stay in
// lockstep across every client.
func TestNormalizePromptFileCase_RewritesMetadata(t *testing.T) {
	userMeta := strings.TrimSpace(`
metadata-version = "1.0"

[asset]
  name = "my-skill"
  version = "1"
  type = "skill"

[skill]
  prompt-file = "skill.md"
`) + "\n"

	zipData := makeZip(t, map[string]string{
		"skill.md":      "body",
		"metadata.toml": userMeta,
	})

	got, err := normalizePromptFileCase(zipData, asset.TypeSkill)
	if err != nil {
		t.Fatalf("normalizePromptFileCase: %v", err)
	}

	// File on disk is the canonical name.
	files, err := utils.ListZipFiles(got)
	if err != nil {
		t.Fatalf("ListZipFiles: %v", err)
	}
	names := map[string]bool{}
	for _, f := range files {
		names[f] = true
	}
	if !names["SKILL.md"] || names["skill.md"] {
		t.Errorf("expected SKILL.md and no skill.md, got %v", files)
	}

	// Metadata.toml is rewritten to canonical.
	rewrittenBytes, err := utils.ReadZipFile(got, "metadata.toml")
	if err != nil {
		t.Fatalf("ReadZipFile metadata.toml: %v", err)
	}
	rewritten, err := metadata.Parse(rewrittenBytes)
	if err != nil {
		t.Fatalf("Parse rewritten metadata: %v", err)
	}
	if rewritten.Skill == nil || rewritten.Skill.PromptFile != "SKILL.md" {
		t.Errorf("expected prompt-file rewritten to SKILL.md, got %+v", rewritten.Skill)
	}
}

// TestNormalizePromptFileCase_CanonicalWinsOverVariants documents the
// collision rule for the rare case (only reachable on case-sensitive
// filesystems) where a zip contains both the canonical SKILL.md and one or
// more lowercase variants. The canonical content must be preserved verbatim
// — variants are dropped, not promoted.
func TestNormalizePromptFileCase_CanonicalWinsOverVariants(t *testing.T) {
	zipData := makeZip(t, map[string]string{
		"SKILL.md": "canonical body",
		"skill.md": "variant body",
		"Skill.md": "another variant",
	})

	got, err := normalizePromptFileCase(zipData, asset.TypeSkill)
	if err != nil {
		t.Fatalf("normalizePromptFileCase: %v", err)
	}

	files, err := utils.ListZipFiles(got)
	if err != nil {
		t.Fatalf("ListZipFiles: %v", err)
	}
	names := map[string]bool{}
	for _, f := range files {
		names[f] = true
	}
	if !names["SKILL.md"] {
		t.Errorf("expected SKILL.md retained, got %v", files)
	}
	for _, stray := range []string{"skill.md", "Skill.md"} {
		if names[stray] {
			t.Errorf("expected %q dropped, got %v", stray, files)
		}
	}

	body, err := utils.ReadZipFile(got, "SKILL.md")
	if err != nil {
		t.Fatalf("ReadZipFile: %v", err)
	}
	if string(body) != "canonical body" {
		t.Errorf("canonical content was overwritten: got %q", body)
	}
}

// TestNormalizePromptFileCase_MultipleVariantsDeterministic confirms that
// when no canonical entry is present but multiple variants are, the
// lexicographically first variant is the one promoted to canonical and the
// rest are dropped.
func TestNormalizePromptFileCase_MultipleVariantsDeterministic(t *testing.T) {
	zipData := makeZip(t, map[string]string{
		"skill.md": "lowercase body",
		"Skill.md": "title-case body",
	})

	got, err := normalizePromptFileCase(zipData, asset.TypeSkill)
	if err != nil {
		t.Fatalf("normalizePromptFileCase: %v", err)
	}

	files, err := utils.ListZipFiles(got)
	if err != nil {
		t.Fatalf("ListZipFiles: %v", err)
	}
	names := map[string]bool{}
	for _, f := range files {
		names[f] = true
	}
	if !names["SKILL.md"] {
		t.Errorf("expected SKILL.md present, got %v", files)
	}
	if names["skill.md"] || names["Skill.md"] {
		t.Errorf("expected variants dropped, got %v", files)
	}

	// "Skill.md" sorts before "skill.md" (uppercase < lowercase in ASCII), so
	// it should be the winner.
	body, err := utils.ReadZipFile(got, "SKILL.md")
	if err != nil {
		t.Fatalf("ReadZipFile: %v", err)
	}
	if string(body) != "title-case body" {
		t.Errorf("expected lexicographically-first variant's content, got %q", body)
	}
}

// TestNormalizePromptFileCase_MetadataOnlyRewrite covers the path where the
// file on disk is already canonical but the user-supplied metadata.toml
// declares the lowercase form. We still rewrite metadata so the lockfile
// content matches the file shipped to clients.
func TestNormalizePromptFileCase_MetadataOnlyRewrite(t *testing.T) {
	userMeta := strings.TrimSpace(`
metadata-version = "1.0"

[asset]
  name = "my-skill"
  version = "1"
  type = "skill"

[skill]
  prompt-file = "skill.md"
`) + "\n"

	zipData := makeZip(t, map[string]string{
		"SKILL.md":      "body",
		"metadata.toml": userMeta,
	})

	got, err := normalizePromptFileCase(zipData, asset.TypeSkill)
	if err != nil {
		t.Fatalf("normalizePromptFileCase: %v", err)
	}

	rewrittenBytes, err := utils.ReadZipFile(got, "metadata.toml")
	if err != nil {
		t.Fatalf("ReadZipFile metadata.toml: %v", err)
	}
	rewritten, err := metadata.Parse(rewrittenBytes)
	if err != nil {
		t.Fatalf("Parse rewritten metadata: %v", err)
	}
	if rewritten.Skill == nil || rewritten.Skill.PromptFile != "SKILL.md" {
		t.Errorf("expected metadata.toml prompt-file rewritten to SKILL.md, got %+v", rewritten.Skill)
	}

	// File should still be canonical (no rename happened).
	files, err := utils.ListZipFiles(got)
	if err != nil {
		t.Fatalf("ListZipFiles: %v", err)
	}
	names := map[string]bool{}
	for _, f := range files {
		names[f] = true
	}
	if !names["SKILL.md"] {
		t.Errorf("expected SKILL.md preserved, got %v", files)
	}
}

// TestNormalizePromptFileCase_LeavesCustomPromptFileAlone verifies that we
// only touch metadata when the declared prompt-file case-insensitively matches
// the canonical name. A user who deliberately picked an unrelated filename
// must not have it silently overwritten.
func TestNormalizePromptFileCase_LeavesCustomPromptFileAlone(t *testing.T) {
	userMeta := strings.TrimSpace(`
metadata-version = "1.0"

[asset]
  name = "my-skill"
  version = "1"
  type = "skill"

[skill]
  prompt-file = "MY-CUSTOM.md"
`) + "\n"

	zipData := makeZip(t, map[string]string{
		"MY-CUSTOM.md":  "body",
		"metadata.toml": userMeta,
	})

	got, err := normalizePromptFileCase(zipData, asset.TypeSkill)
	if err != nil {
		t.Fatalf("normalizePromptFileCase: %v", err)
	}

	rewrittenBytes, err := utils.ReadZipFile(got, "metadata.toml")
	if err != nil {
		t.Fatalf("ReadZipFile metadata.toml: %v", err)
	}
	rewritten, err := metadata.Parse(rewrittenBytes)
	if err != nil {
		t.Fatalf("Parse rewritten metadata: %v", err)
	}
	if rewritten.Skill == nil || rewritten.Skill.PromptFile != "MY-CUSTOM.md" {
		t.Errorf("expected custom prompt-file preserved, got %+v", rewritten.Skill)
	}
}
