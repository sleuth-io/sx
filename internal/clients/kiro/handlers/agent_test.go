package handlers

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sleuth-io/sx/internal/metadata"
)

func createTestAgentZip(t *testing.T, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, err := w.Create("AGENT.md")
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

func TestAgentHandler_Install_WritesIDEFormat(t *testing.T) {
	targetBase := t.TempDir()
	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "test-agent", Description: "A test agent"},
		Agent: &metadata.AgentConfig{PromptFile: "AGENT.md"},
	}
	handler := NewAgentHandler(meta)
	zipData := createTestAgentZip(t, "You are a helpful assistant.")

	if err := handler.Install(context.Background(), zipData, targetBase); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	filePath := filepath.Join(targetBase, DirAgents, "test-agent.md")
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("Failed to read IDE agent file: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, `description: "A test agent"`) {
		t.Errorf("expected description in frontmatter, got:\n%s", content)
	}
	if !strings.Contains(content, "---") {
		t.Errorf("expected YAML frontmatter delimiters, got:\n%s", content)
	}
	if !strings.Contains(content, "You are a helpful assistant.") {
		t.Errorf("expected agent body, got:\n%s", content)
	}
}

func TestAgentHandler_Install_WritesCLIFormat(t *testing.T) {
	targetBase := t.TempDir()
	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "test-agent", Description: "A test agent"},
		Agent: &metadata.AgentConfig{PromptFile: "AGENT.md"},
	}
	handler := NewAgentHandler(meta)
	zipData := createTestAgentZip(t, "You are a helpful assistant.")

	if err := handler.Install(context.Background(), zipData, targetBase); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	filePath := filepath.Join(targetBase, DirAgents, "test-agent.json")
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("Failed to read CLI agent file: %v", err)
	}

	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("Failed to parse CLI agent JSON: %v", err)
	}

	if config["name"] != "test-agent" {
		t.Errorf("expected name=test-agent, got %v", config["name"])
	}
	if config["description"] != "A test agent" {
		t.Errorf("expected description in CLI JSON, got %v", config["description"])
	}
	if config["prompt"] != "You are a helpful assistant." {
		t.Errorf("expected prompt in CLI JSON, got %v", config["prompt"])
	}
}

func TestAgentHandler_Install_WithKiroFields(t *testing.T) {
	targetBase := t.TempDir()
	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "smart-agent", Description: "Agent with config"},
		Agent: &metadata.AgentConfig{
			PromptFile: "AGENT.md",
			Kiro: map[string]any{
				"model": "claude-sonnet-4",
				"tools": []any{"read", "write"},
			},
		},
	}
	handler := NewAgentHandler(meta)
	zipData := createTestAgentZip(t, "You are a smart agent.")

	if err := handler.Install(context.Background(), zipData, targetBase); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	// Check IDE format has model and tools
	ideData, err := os.ReadFile(filepath.Join(targetBase, DirAgents, "smart-agent.md"))
	if err != nil {
		t.Fatalf("Failed to read IDE file: %v", err)
	}
	ideContent := string(ideData)
	if !strings.Contains(ideContent, "model: claude-sonnet-4") {
		t.Errorf("expected model in IDE frontmatter, got:\n%s", ideContent)
	}
	if !strings.Contains(ideContent, "tools:") {
		t.Errorf("expected tools in IDE frontmatter, got:\n%s", ideContent)
	}

	// Check CLI format has model and tools
	cliData, err := os.ReadFile(filepath.Join(targetBase, DirAgents, "smart-agent.json"))
	if err != nil {
		t.Fatalf("Failed to read CLI file: %v", err)
	}
	var cliConfig map[string]any
	if err := json.Unmarshal(cliData, &cliConfig); err != nil {
		t.Fatalf("Failed to parse CLI JSON: %v", err)
	}
	if cliConfig["model"] != "claude-sonnet-4" {
		t.Errorf("expected model in CLI JSON, got %v", cliConfig["model"])
	}
	if _, ok := cliConfig["tools"]; !ok {
		t.Errorf("expected tools in CLI JSON, got keys: %v", cliConfig)
	}
}

func TestAgentHandler_Install_NoDescription(t *testing.T) {
	targetBase := t.TempDir()
	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "bare-agent"},
		Agent: &metadata.AgentConfig{PromptFile: "AGENT.md"},
	}
	handler := NewAgentHandler(meta)
	zipData := createTestAgentZip(t, "Minimal agent.")

	if err := handler.Install(context.Background(), zipData, targetBase); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	// IDE format should not have description key
	ideData, _ := os.ReadFile(filepath.Join(targetBase, DirAgents, "bare-agent.md"))
	if strings.Contains(string(ideData), "description:") {
		t.Errorf("IDE file should not contain description when empty, got:\n%s", string(ideData))
	}

	// CLI format should not have description key
	cliData, _ := os.ReadFile(filepath.Join(targetBase, DirAgents, "bare-agent.json"))
	var config map[string]any
	if err := json.Unmarshal(cliData, &config); err != nil {
		t.Fatalf("Failed to parse CLI JSON: %v", err)
	}
	if _, ok := config["description"]; ok {
		t.Errorf("CLI JSON should not contain description when empty, got: %v", config)
	}
}

func TestAgentHandler_Install_WithPermissions(t *testing.T) {
	targetBase := t.TempDir()
	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "secure-agent", Description: "Agent with permissions"},
		Agent: &metadata.AgentConfig{
			PromptFile: "AGENT.md",
			Kiro: map[string]any{
				"permissions": []any{
					map[string]any{"capability": "filesystem", "effect": "allow", "match": []any{"src/**"}},
					map[string]any{"capability": "shell", "effect": "deny"},
				},
			},
		},
	}
	handler := NewAgentHandler(meta)
	zipData := createTestAgentZip(t, "A secure agent.")

	if err := handler.Install(context.Background(), zipData, targetBase); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	// IDE format should contain permissions wrapped in a "rules" object: permissions.rules[...]
	ideData, err := os.ReadFile(filepath.Join(targetBase, DirAgents, "secure-agent.md"))
	if err != nil {
		t.Fatalf("Failed to read IDE file: %v", err)
	}
	ideContent := string(ideData)
	if !strings.Contains(ideContent, "permissions:") {
		t.Errorf("IDE file should contain permissions, got:\n%s", ideContent)
	}
	if !strings.Contains(ideContent, "rules:") {
		t.Errorf("IDE file permissions should have a rules key, got:\n%s", ideContent)
	}
	if !strings.Contains(ideContent, "filesystem") {
		t.Errorf("IDE file should contain capability names, got:\n%s", ideContent)
	}

	// CLI format (v2) should NOT contain permissions — no permissions model in v2
	cliData, err := os.ReadFile(filepath.Join(targetBase, DirAgents, "secure-agent.json"))
	if err != nil {
		t.Fatalf("Failed to read CLI file: %v", err)
	}
	var cliConfig map[string]any
	if err := json.Unmarshal(cliData, &cliConfig); err != nil {
		t.Fatalf("Failed to parse CLI JSON: %v", err)
	}
	if _, ok := cliConfig["permissions"]; ok {
		t.Errorf("CLI JSON should not contain permissions (CLI v2 has no permissions model), got: %v", cliConfig)
	}
}

func TestAgentHandler_Install_V2FieldsInCLINotInIDE(t *testing.T) {
	targetBase := t.TempDir()
	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "v2-agent", Description: "Agent with v2-specific fields"},
		Agent: &metadata.AgentConfig{
			PromptFile: "AGENT.md",
			Kiro: map[string]any{
				"model":            "claude-sonnet-4",
				"allowedTools":     []any{"fs_read", "shell"},
				"keyboardShortcut": "ctrl+a",
				"welcomeMessage":   "Hello from v2 agent",
				"unknownFuture":    "somevalue", // unknown field — passes through to both formats
				"hooks": map[string]any{
					"agentSpawn": []any{map[string]any{"command": "echo spawned"}},
				},
			},
		},
	}
	handler := NewAgentHandler(meta)
	zipData := createTestAgentZip(t, "A v2 agent.")

	if err := handler.Install(context.Background(), zipData, targetBase); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	// CLI v2 JSON: known v2-only fields should be present
	cliData, err := os.ReadFile(filepath.Join(targetBase, DirAgents, "v2-agent.json"))
	if err != nil {
		t.Fatalf("Failed to read CLI file: %v", err)
	}
	var cliConfig map[string]any
	if err := json.Unmarshal(cliData, &cliConfig); err != nil {
		t.Fatalf("Failed to parse CLI JSON: %v", err)
	}
	for _, key := range []string{"allowedTools", "keyboardShortcut", "welcomeMessage", "hooks"} {
		if _, ok := cliConfig[key]; !ok {
			t.Errorf("CLI JSON should contain v2 field %q, got keys: %v", key, cliConfig)
		}
	}
	// Unknown fields pass through to CLI JSON (sx forward-compat principle)
	if _, ok := cliConfig["unknownFuture"]; !ok {
		t.Errorf("CLI JSON should preserve unknown field 'unknownFuture' for forward compat, got: %v", cliConfig)
	}

	// IDE .md: v2-only fields must NOT appear; welcomeMessage and unknownFuture should
	ideData, err := os.ReadFile(filepath.Join(targetBase, DirAgents, "v2-agent.md"))
	if err != nil {
		t.Fatalf("Failed to read IDE file: %v", err)
	}
	ideContent := string(ideData)
	for _, key := range []string{"allowedTools", "keyboardShortcut", "hooks"} {
		if strings.Contains(ideContent, key+":") {
			t.Errorf("IDE .md should not contain v2-only field %q, got:\n%s", key, ideContent)
		}
	}
	// welcomeMessage is a known v3/IDE field — should be present
	if !strings.Contains(ideContent, "welcomeMessage:") {
		t.Errorf("IDE .md should contain welcomeMessage (known v3/IDE field), got:\n%s", ideContent)
	}
	// Unknown fields pass through to IDE .md for forward compat
	if !strings.Contains(ideContent, "unknownFuture:") {
		t.Errorf("IDE .md should preserve unknown field 'unknownFuture' for forward compat, got:\n%s", ideContent)
	}
}

func TestAgentHandler_Install_EmptyCollectionsOmitted(t *testing.T) {
	targetBase := t.TempDir()
	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "sparse-agent", Description: "Agent with empty collections"},
		Agent: &metadata.AgentConfig{
			PromptFile: "AGENT.md",
			Kiro: map[string]any{
				"model":       "claude-sonnet-4",
				"tools":       []any{},
				"mcpServers":  map[string]any{},
				"permissions": []any{},
			},
		},
	}
	handler := NewAgentHandler(meta)
	zipData := createTestAgentZip(t, "Sparse agent.")

	if err := handler.Install(context.Background(), zipData, targetBase); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	// IDE: empty tools/mcpServers/permissions should not appear in frontmatter
	ideData, err := os.ReadFile(filepath.Join(targetBase, DirAgents, "sparse-agent.md"))
	if err != nil {
		t.Fatalf("Failed to read IDE file: %v", err)
	}
	ideContent := string(ideData)
	for _, key := range []string{"tools:", "mcpServers:", "permissions:"} {
		if strings.Contains(ideContent, key) {
			t.Errorf("IDE file should not contain %q when value is empty, got:\n%s", key, ideContent)
		}
	}
	if !strings.Contains(ideContent, "model: claude-sonnet-4") {
		t.Errorf("IDE file should still contain non-empty fields, got:\n%s", ideContent)
	}

	// CLI JSON: empty tools/mcpServers should not appear
	cliData, err := os.ReadFile(filepath.Join(targetBase, DirAgents, "sparse-agent.json"))
	if err != nil {
		t.Fatalf("Failed to read CLI file: %v", err)
	}
	var cliConfig map[string]any
	if err := json.Unmarshal(cliData, &cliConfig); err != nil {
		t.Fatalf("Failed to parse CLI JSON: %v", err)
	}
	for _, key := range []string{"tools", "mcpServers"} {
		if _, ok := cliConfig[key]; ok {
			t.Errorf("CLI JSON should not contain %q when value is empty, got: %v", key, cliConfig)
		}
	}
	if cliConfig["model"] != "claude-sonnet-4" {
		t.Errorf("CLI JSON should still contain non-empty fields, got: %v", cliConfig)
	}
}

func TestAgentHandler_Remove(t *testing.T) {
	targetBase := t.TempDir()
	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "remove-agent"},
		Agent: &metadata.AgentConfig{PromptFile: "AGENT.md"},
	}
	handler := NewAgentHandler(meta)

	zipData := createTestAgentZip(t, "To be removed.")
	if err := handler.Install(context.Background(), zipData, targetBase); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	mdPath := filepath.Join(targetBase, DirAgents, "remove-agent.md")
	jsonPath := filepath.Join(targetBase, DirAgents, "remove-agent.json")

	if _, err := os.Stat(mdPath); os.IsNotExist(err) {
		t.Fatal(".md file should exist after install")
	}
	if _, err := os.Stat(jsonPath); os.IsNotExist(err) {
		t.Fatal(".json file should exist after install")
	}

	if err := handler.Remove(context.Background(), targetBase); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	if _, err := os.Stat(mdPath); !os.IsNotExist(err) {
		t.Error(".md file should not exist after remove")
	}
	if _, err := os.Stat(jsonPath); !os.IsNotExist(err) {
		t.Error(".json file should not exist after remove")
	}
}

func TestAgentHandler_VerifyInstalled(t *testing.T) {
	targetBase := t.TempDir()
	meta := &metadata.Metadata{
		Asset: metadata.Asset{Name: "verify-agent"},
		Agent: &metadata.AgentConfig{PromptFile: "AGENT.md"},
	}
	handler := NewAgentHandler(meta)

	installed, _ := handler.VerifyInstalled(targetBase)
	if installed {
		t.Error("should not be installed before install")
	}

	zipData := createTestAgentZip(t, "Verify me.")
	if err := handler.Install(context.Background(), zipData, targetBase); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	installed, msg := handler.VerifyInstalled(targetBase)
	if !installed {
		t.Errorf("should be installed after install, got: %s", msg)
	}

	if err := handler.Remove(context.Background(), targetBase); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	installed, _ = handler.VerifyInstalled(targetBase)
	if installed {
		t.Error("should not be installed after remove")
	}
}
