package handlers

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sleuth-io/sx/internal/metadata"
)

func createTestRuleZip(t *testing.T, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, err := w.Create("RULE.md")
	if err != nil {
		t.Fatalf("Failed to create zip entry: %v", err)
	}
	if _, err := f.Write([]byte(content)); err != nil {
		t.Fatalf("Failed to write zip content: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Failed to close zip: %v", err)
	}
	return buf.Bytes()
}

func TestBuildSteeringContent_DefaultInclusion(t *testing.T) {
	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "test-rule", Description: "A test rule"},
		Rule:  &metadata.RuleConfig{},
	}
	handler := NewRuleHandler(meta, "")
	content := handler.buildSteeringContent("Some rule content.")

	if !strings.Contains(content, "inclusion: always") {
		t.Errorf("expected inclusion: always, got:\n%s", content)
	}
}

func TestBuildSteeringContent_KiroInclusionOverride(t *testing.T) {
	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "my-rule", Description: "Manual rule"},
		Rule: &metadata.RuleConfig{
			Description: "Manual rule",
			Kiro: map[string]any{
				"inclusion": "manual",
			},
		},
	}
	handler := NewRuleHandler(meta, "")
	content := handler.buildSteeringContent("Manual content.")

	if !strings.Contains(content, "inclusion: manual") {
		t.Errorf("expected inclusion: manual from [rule.kiro], got:\n%s", content)
	}
	if strings.Contains(content, "inclusion: always") {
		t.Errorf("should not contain inclusion: always, got:\n%s", content)
	}
}

func TestBuildSteeringContent_KiroInclusionAuto(t *testing.T) {
	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "auto-rule", Description: "Auto rule"},
		Rule: &metadata.RuleConfig{
			Description: "Auto rule",
			Kiro: map[string]any{
				"inclusion": "auto",
			},
		},
	}
	handler := NewRuleHandler(meta, "")
	content := handler.buildSteeringContent("Auto content.")

	if !strings.Contains(content, "inclusion: auto") {
		t.Errorf("expected inclusion: auto from [rule.kiro], got:\n%s", content)
	}
	if strings.Contains(content, "inclusion: always") {
		t.Errorf("should not contain inclusion: always, got:\n%s", content)
	}
}

func TestBuildSteeringContent_GlobsOverrideKiroInclusion(t *testing.T) {
	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "glob-rule"},
		Rule: &metadata.RuleConfig{
			Globs: []string{"**/*.go"},
			Kiro: map[string]any{
				"inclusion": "manual",
			},
		},
	}
	handler := NewRuleHandler(meta, "")
	content := handler.buildSteeringContent("Go content.")

	if !strings.Contains(content, "inclusion: fileMatch") {
		t.Errorf("globs should force fileMatch, got:\n%s", content)
	}
}

func TestBuildSteeringContent_Description(t *testing.T) {
	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "desc-rule", Description: "Asset desc"},
		Rule: &metadata.RuleConfig{
			Description: "Rule desc",
		},
	}
	handler := NewRuleHandler(meta, "")
	content := handler.buildSteeringContent("Content.")

	if !strings.Contains(content, `description: "Rule desc"`) {
		t.Errorf("expected rule-level description, got:\n%s", content)
	}
}

func TestBuildSteeringContent_FallbackDescription(t *testing.T) {
	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "desc-rule", Description: "Asset desc"},
		Rule:  &metadata.RuleConfig{},
	}
	handler := NewRuleHandler(meta, "")
	content := handler.buildSteeringContent("Content.")

	if !strings.Contains(content, `description: "Asset desc"`) {
		t.Errorf("expected asset-level description fallback, got:\n%s", content)
	}
}

func TestRuleHandler_Install(t *testing.T) {
	targetBase := t.TempDir()
	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "install-test"},
		Rule: &metadata.RuleConfig{
			PromptFile: "RULE.md",
			Kiro: map[string]any{
				"inclusion": "manual",
			},
		},
	}
	handler := NewRuleHandler(meta, "")
	zipData := createTestRuleZip(t, "Install test content.")

	err := handler.Install(context.Background(), zipData, targetBase)
	if err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	filePath := filepath.Join(targetBase, DirSteering, "install-test.md")
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("Failed to read installed file: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "inclusion: manual") {
		t.Errorf("installed file should have inclusion: manual, got:\n%s", content)
	}
	if !strings.Contains(content, "Install test content.") {
		t.Errorf("installed file should contain rule body, got:\n%s", content)
	}
}

func TestRuleHandler_Remove(t *testing.T) {
	targetBase := t.TempDir()
	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "remove-test"},
		Rule:  &metadata.RuleConfig{PromptFile: "RULE.md"},
	}
	handler := NewRuleHandler(meta, "")

	// Install first
	zipData := createTestRuleZip(t, "To be removed.")
	if err := handler.Install(context.Background(), zipData, targetBase); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	// Verify exists
	filePath := filepath.Join(targetBase, DirSteering, "remove-test.md")
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Fatal("File should exist after install")
	}

	// Remove
	if err := handler.Remove(context.Background(), targetBase); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Error("File should not exist after remove")
	}
}
