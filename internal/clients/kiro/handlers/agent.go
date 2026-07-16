package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/sleuth-io/sx/internal/metadata"
	"github.com/sleuth-io/sx/internal/utils"
)

// AgentHandler handles agent asset installation for Kiro.
// Agents are installed in two formats to .kiro/agents/:
//
//   - {name}.md  — IDE + CLI v3 format (YAML frontmatter + system prompt body)
//     Known fields: model, tools, mcpServers, resources, welcomeMessage, permissions.
//     permissions is wrapped as {rules:[...]} per the Kiro schema.
//     Unknown [agent.kiro] fields pass through to frontmatter — both the IDE and v3
//     engine tolerate extra keys (verified by live harness test).
//
//   - {name}.json — CLI v2 format (JSON config with prompt field)
//     Known fields: model, tools, mcpServers, resources, welcomeMessage, allowedTools,
//     toolAliases, toolsSettings, hooks, includeMcpJson, keyboardShortcut.
//     Unknown [agent.kiro] fields pass through — CLI v2 JSON also tolerates extra keys.
//     permissions is excluded — CLI v2 has no permissions model.
//
// Forward compatibility: unknown fields are preserved in both output formats so vault
// assets remain compatible with future Kiro schema additions without requiring sx updates.
type AgentHandler struct {
	metadata *metadata.Metadata
}

// NewAgentHandler creates a new agent handler
func NewAgentHandler(meta *metadata.Metadata) *AgentHandler {
	return &AgentHandler{metadata: meta}
}

// Install writes both IDE (.md) and CLI (.json) agent files to {targetBase}/agents/
func (h *AgentHandler) Install(ctx context.Context, zipData []byte, targetBase string) error {
	content, err := h.readAgentContent(zipData)
	if err != nil {
		return fmt.Errorf("failed to read agent content: %w", err)
	}

	agentsDir := filepath.Join(targetBase, DirAgents)
	if err := utils.EnsureDir(agentsDir); err != nil {
		return fmt.Errorf("failed to create agents directory: %w", err)
	}

	if err := h.writeIDEFormat(agentsDir, content); err != nil {
		return fmt.Errorf("failed to write IDE agent file: %w", err)
	}

	if err := h.writeCLIFormat(agentsDir, content); err != nil {
		return fmt.Errorf("failed to write CLI agent file: %w", err)
	}

	return nil
}

// Remove removes both IDE and CLI agent files
func (h *AgentHandler) Remove(ctx context.Context, targetBase string) error {
	for _, filename := range []string{
		filepath.Join(targetBase, DirAgents, h.metadata.Asset.Name+".md"),
		filepath.Join(targetBase, DirAgents, h.metadata.Asset.Name+".json"),
	} {
		if err := os.Remove(filename); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove agent file %s: %w", filename, err)
		}
	}
	return nil
}

// VerifyInstalled checks if the IDE agent file exists (canonical format)
func (h *AgentHandler) VerifyInstalled(targetBase string) (bool, string) {
	filePath := filepath.Join(targetBase, DirAgents, h.metadata.Asset.Name+".md")
	if _, err := os.Stat(filePath); err == nil {
		return true, "Found at " + filePath
	}
	return false, "Agent file not found"
}

// writeIDEFormat writes the Kiro IDE / CLI v3 agent markdown file with YAML frontmatter.
// Known fields are written with field-specific handling (e.g. permissions wrapping).
// Unknown [agent.kiro] fields pass through to frontmatter for forward compatibility.
// v2-only fields (allowedTools, toolAliases, toolsSettings, hooks, includeMcpJson,
// keyboardShortcut) are excluded — they have no meaning in the IDE / v3 format.
func (h *AgentHandler) writeIDEFormat(agentsDir string, content string) error {
	var sb strings.Builder

	sb.WriteString("---\n")

	if desc := h.getDescription(); desc != "" {
		fmt.Fprintf(&sb, "description: %q\n", desc)
	}

	kiro := h.getKiroFields()

	if model, ok := kiro["model"].(string); ok && model != "" {
		fmt.Fprintf(&sb, "model: %s\n", model)
	}

	// Known collection fields — skip if empty to avoid no-op keys
	for _, key := range []string{"tools", "resources"} {
		if val, ok := kiro[key]; ok {
			if sl, isList := val.([]any); !isList || len(sl) > 0 {
				if out, err := yaml.Marshal(map[string]any{key: val}); err == nil {
					sb.Write(out)
				}
			}
		}
	}

	if mcpServers, ok := kiro["mcpServers"]; ok {
		if m, isMap := mcpServers.(map[string]any); !isMap || len(m) > 0 {
			if out, err := yaml.Marshal(map[string]any{"mcpServers": mcpServers}); err == nil {
				sb.Write(out)
			}
		}
	}

	if wm, ok := kiro["welcomeMessage"].(string); ok && wm != "" {
		if out, err := yaml.Marshal(map[string]any{"welcomeMessage": wm}); err == nil {
			sb.Write(out)
		}
	}

	if permissions, ok := kiro["permissions"]; ok {
		if sl, isList := permissions.([]any); !isList || len(sl) > 0 {
			// Kiro expects permissions as an object with a "rules" key: { rules: [...] }
			if out, err := yaml.Marshal(map[string]any{"permissions": map[string]any{"rules": permissions}}); err == nil {
				sb.Write(out)
			}
		}
	}

	// Pass through unknown fields for forward compatibility; exclude v2-only fields
	// that have no meaning in the IDE / v3 format.
	v2Only := map[string]bool{
		"allowedTools": true, "toolAliases": true, "toolsSettings": true,
		"hooks": true, "includeMcpJson": true, "keyboardShortcut": true,
	}
	knownHandled := map[string]bool{
		"model": true, "tools": true, "resources": true,
		"mcpServers": true, "welcomeMessage": true, "permissions": true,
	}
	for _, key := range sortedKeys(kiro) {
		if knownHandled[key] || v2Only[key] {
			continue
		}
		val := kiro[key]
		if isEmptyValue(val) {
			continue
		}
		if out, err := yaml.Marshal(map[string]any{key: val}); err == nil {
			sb.Write(out)
		}
	}

	sb.WriteString("---\n\n")
	sb.WriteString(strings.TrimSpace(content))
	sb.WriteString("\n")

	filePath := filepath.Join(agentsDir, h.metadata.Asset.Name+".md")
	return os.WriteFile(filePath, []byte(sb.String()), 0644)
}

// writeCLIFormat writes the Kiro CLI v2 agent JSON file.
// All [agent.kiro] fields pass through to JSON except permissions (CLI v2 has no
// permissions model). Unknown fields are preserved for forward compatibility —
// CLI v2 JSON tolerates extra keys (verified by live harness test).
// CLI v3 (kiro-cli --v3) reads the .md file written by writeIDEFormat directly.
func (h *AgentHandler) writeCLIFormat(agentsDir string, content string) error {
	config := map[string]any{
		"name":   h.metadata.Asset.Name,
		"prompt": strings.TrimSpace(content),
	}

	if desc := h.getDescription(); desc != "" {
		config["description"] = desc
	}

	// Pass all [agent.kiro] fields through to v2 JSON except permissions.
	// Unknown fields are preserved (sx forward-compat principle).
	kiro := h.getKiroFields()
	for key, val := range kiro {
		if key == "permissions" {
			continue
		}
		if isEmptyValue(val) {
			continue
		}
		config[key] = val
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal CLI agent JSON: %w", err)
	}

	filePath := filepath.Join(agentsDir, h.metadata.Asset.Name+".json")
	return os.WriteFile(filePath, append(data, '\n'), 0644)
}

func (h *AgentHandler) getPromptFile() string {
	if h.metadata.Agent != nil && h.metadata.Agent.PromptFile != "" {
		return h.metadata.Agent.PromptFile
	}
	return "AGENT.md"
}

func (h *AgentHandler) getDescription() string {
	return h.metadata.Asset.Description
}

func (h *AgentHandler) getKiroFields() map[string]any {
	if h.metadata.Agent != nil && h.metadata.Agent.Kiro != nil {
		return h.metadata.Agent.Kiro
	}
	return map[string]any{}
}

func isEmptyValue(val any) bool {
	switch v := val.(type) {
	case []any:
		return len(v) == 0
	case map[string]any:
		return len(v) == 0
	case string:
		return v == ""
	}
	return false
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (h *AgentHandler) readAgentContent(zipData []byte) (string, error) {
	promptFile := h.getPromptFile()

	content, err := utils.ReadZipFile(zipData, promptFile)
	if err != nil {
		content, err = utils.ReadZipFile(zipData, "agent.md")
		if err != nil {
			return "", fmt.Errorf("prompt file not found: %s", promptFile)
		}
	}

	return string(content), nil
}
