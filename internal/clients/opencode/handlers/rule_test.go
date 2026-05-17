package handlers

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/metadata"
)

func TestOpenCodeRuleHandler_InstallWritesFileAndRegistersInstruction(t *testing.T) {
	targetBase := t.TempDir()
	registerPath := filepath.Join(DirRules, "go-style.md")

	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name:    "go-style",
			Version: "1.0.0",
			Type:    asset.TypeRule,
		},
		Rule: &metadata.RuleConfig{PromptFile: "RULE.md"},
	}

	zipData := createTestZip(t, map[string]string{
		"metadata.toml": `[asset]
name = "go-style"
type = "rule"
version = "1.0.0"

[rule]
prompt-file = "RULE.md"
`,
		"RULE.md": "# Go Style\n\nUse tabs.\n",
	})

	h := NewRuleHandler(meta, registerPath)
	if err := h.Install(context.Background(), zipData, targetBase); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	ruleFile := filepath.Join(targetBase, DirRules, "go-style.md")
	body, err := os.ReadFile(ruleFile)
	if err != nil {
		t.Fatalf("Rule file should exist: %v", err)
	}
	if string(body) == "" {
		t.Error("Rule file should not be empty")
	}

	// opencode.json should now reference the rule path under `instructions`.
	configBytes, err := os.ReadFile(filepath.Join(targetBase, ConfigFile))
	if err != nil {
		t.Fatalf("opencode.json should exist after install: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(configBytes, &raw); err != nil {
		t.Fatalf("opencode.json should be valid JSON: %v", err)
	}
	instr, ok := raw["instructions"].([]any)
	if !ok {
		t.Fatalf("instructions should be an array, got %T", raw["instructions"])
	}
	if len(instr) != 1 || instr[0] != registerPath {
		t.Errorf("instructions should contain %q, got %v", registerPath, instr)
	}

	installed, msg := h.VerifyInstalled(targetBase)
	if !installed {
		t.Errorf("Should report installed: %s", msg)
	}
}

func TestOpenCodeRuleHandler_Remove(t *testing.T) {
	targetBase := t.TempDir()
	registerPath := filepath.Join(DirRules, "secrets.md")

	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name:    "secrets",
			Version: "1.0.0",
			Type:    asset.TypeRule,
		},
		Rule: &metadata.RuleConfig{PromptFile: "RULE.md"},
	}

	zipData := createTestZip(t, map[string]string{
		"RULE.md": "no secrets in code",
	})

	h := NewRuleHandler(meta, registerPath)
	if err := h.Install(context.Background(), zipData, targetBase); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	if err := h.Remove(context.Background(), targetBase); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	ruleFile := filepath.Join(targetBase, DirRules, "secrets.md")
	if _, err := os.Stat(ruleFile); !os.IsNotExist(err) {
		t.Error("Rule file should be removed")
	}

	configBytes, err := os.ReadFile(filepath.Join(targetBase, ConfigFile))
	if err != nil {
		t.Fatalf("opencode.json should still exist after remove: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(configBytes, &raw); err != nil {
		t.Fatalf("opencode.json should be valid JSON: %v", err)
	}
	if instr, ok := raw["instructions"].([]any); ok {
		strs := make([]string, 0, len(instr))
		for _, v := range instr {
			if s, ok := v.(string); ok {
				strs = append(strs, s)
			}
		}
		if slices.Contains(strs, registerPath) {
			t.Errorf("instructions should no longer contain %q, got %v", registerPath, strs)
		}
	}
}

func TestOpenCodeRuleHandler_FallsBackToLowercaseRuleMd(t *testing.T) {
	targetBase := t.TempDir()

	meta := &metadata.Metadata{
		Asset: metadata.Asset{
			Name:    "lower",
			Version: "1.0.0",
			Type:    asset.TypeRule,
		},
		// No Rule config: handler should default to RULE.md, then fall back.
	}

	zipData := createTestZip(t, map[string]string{
		"rule.md": "lowercase ok",
	})

	h := NewRuleHandler(meta, "rules/lower.md")
	if err := h.Install(context.Background(), zipData, targetBase); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	body, err := os.ReadFile(filepath.Join(targetBase, DirRules, "lower.md"))
	if err != nil {
		t.Fatalf("Rule file should exist: %v", err)
	}
	if string(body) != "lowercase ok" {
		t.Errorf("Rule content mismatch: got %q", string(body))
	}
}

func TestAddInstruction_DeduplicatesEntries(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "opencode.json")

	if err := AddInstruction(configPath, "rules/a.md"); err != nil {
		t.Fatalf("first AddInstruction failed: %v", err)
	}
	if err := AddInstruction(configPath, "rules/a.md"); err != nil {
		t.Fatalf("second AddInstruction failed: %v", err)
	}

	cfg, err := ReadOpenCodeConfig(configPath)
	if err != nil {
		t.Fatalf("ReadOpenCodeConfig failed: %v", err)
	}
	if got := len(cfg.Instructions); got != 1 {
		t.Errorf("Expected 1 instruction after dedup, got %d (%v)", got, cfg.Instructions)
	}
}

func TestAddInstruction_PreservesUnknownFields(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "opencode.json")

	// Pre-write a config with a field sx doesn't model.
	if err := os.WriteFile(configPath, []byte(`{"$schema":"https://opencode.ai/config.json","model":"anthropic/claude-sonnet-4-6"}`), 0644); err != nil {
		t.Fatalf("seed write failed: %v", err)
	}

	if err := AddInstruction(configPath, "rules/foo.md"); err != nil {
		t.Fatalf("AddInstruction failed: %v", err)
	}

	body, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if raw["model"] != "anthropic/claude-sonnet-4-6" {
		t.Errorf("model should be preserved, got %v", raw["model"])
	}
}
