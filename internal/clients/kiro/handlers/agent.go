package handlers

import (
	"context"
	"encoding/json"
	"errors"
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

// v2Only lists fields that only apply to the CLI v2 JSON format and must be
// excluded from the .md frontmatter (which targets both the IDE and CLI v3).
var v2Only = map[string]bool{
	"allowedTools": true, "toolAliases": true, "toolsSettings": true,
	"hooks": true, "includeMcpJson": true, "keyboardShortcut": true,
}

// NewAgentHandler creates a new agent handler
func NewAgentHandler(meta *metadata.Metadata) *AgentHandler {
	return &AgentHandler{metadata: meta}
}

// Install writes both IDE (.md) and CLI (.json) agent files to {targetBase}/agents/.
// The two writes are not atomic; a failure between them leaves a half-installed
// agent, which VerifyInstalled reports as broken so --repair rewrites both files.
func (h *AgentHandler) Install(ctx context.Context, zipData []byte, targetBase string) error {
	// {targetBase}/agents/default.json is the sx-managed Kiro CLI hooks config;
	// an agent with that name would clobber it on install and delete it on remove.
	if h.metadata.Asset.Name == "default" {
		return errors.New(`agent name "default" is reserved: .kiro/agents/default.json holds sx-managed Kiro CLI hooks`)
	}

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
	files := []string{
		filepath.Join(targetBase, DirAgents, h.metadata.Asset.Name+".md"),
	}
	// Never touch default.json — it is the sx-managed Kiro CLI hooks config,
	// not an installed agent (Install rejects the name, but Remove can be
	// invoked from lockfile cleanup regardless).
	if h.metadata.Asset.Name != "default" {
		files = append(files, filepath.Join(targetBase, DirAgents, h.metadata.Asset.Name+".json"))
	}
	for _, filename := range files {
		if err := os.Remove(filename); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove agent file %s: %w", filename, err)
		}
	}
	return nil
}

// VerifyInstalled checks that both agent files exist; a missing half means the
// install is broken for one of the Kiro engines and should be repaired
func (h *AgentHandler) VerifyInstalled(targetBase string) (bool, string) {
	mdPath := filepath.Join(targetBase, DirAgents, h.metadata.Asset.Name+".md")
	if _, err := os.Stat(mdPath); err != nil {
		return false, "IDE agent file not found"
	}
	jsonPath := filepath.Join(targetBase, DirAgents, h.metadata.Asset.Name+".json")
	if _, err := os.Stat(jsonPath); err != nil {
		return false, "CLI agent file not found"
	}
	return true, "Found at " + mdPath
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
		if err := writeYAMLField(&sb, "description", desc); err != nil {
			return err
		}
	}

	kiro := h.getKiroFields()

	// handled tracks fields consumed by the field-specific logic below
	// (whether emitted or intentionally omitted when empty). A known field
	// with an unexpected type is left unhandled so the generic pass-through
	// still emits it rather than dropping it silently. name and description
	// are reserved top-level fields (description is already written from
	// [asset].description above) — never pass them through a second time,
	// mirroring the reserved-key guard in writeCLIFormat.
	handled := map[string]bool{"permissions": true, "name": true, "description": true}

	if model, ok := kiro["model"].(string); ok {
		handled["model"] = true
		if model != "" {
			if err := writeYAMLField(&sb, "model", model); err != nil {
				return err
			}
		}
	}

	// Known collection fields — skip if empty to avoid no-op keys
	for _, key := range []string{"tools", "resources"} {
		if val, ok := kiro[key]; ok {
			if sl, isList := val.([]any); isList {
				handled[key] = true
				if len(sl) > 0 {
					if err := writeYAMLField(&sb, key, val); err != nil {
						return err
					}
				}
			}
		}
	}

	if mcpServers, ok := kiro["mcpServers"]; ok {
		if m, isMap := mcpServers.(map[string]any); isMap {
			handled["mcpServers"] = true
			if len(m) > 0 {
				if err := writeYAMLField(&sb, "mcpServers", mcpServers); err != nil {
					return err
				}
			}
		}
	}

	if wm, ok := kiro["welcomeMessage"].(string); ok {
		handled["welcomeMessage"] = true
		if wm != "" {
			if err := writeYAMLField(&sb, "welcomeMessage", wm); err != nil {
				return err
			}
		}
	}

	if permissions, ok := kiro["permissions"]; ok {
		rules, err := normalizePermissions(permissions)
		if err != nil {
			return err
		}
		if len(rules) > 0 {
			// Kiro expects permissions as an object with a "rules" key: { rules: [...] }
			if err := writeYAMLField(&sb, "permissions", map[string]any{"rules": rules}); err != nil {
				return err
			}
		}
	}

	// Pass through unknown fields for forward compatibility; exclude v2-only fields
	// that have no meaning in the IDE / v3 format.
	for _, key := range sortedKeys(kiro) {
		if handled[key] || v2Only[key] {
			continue
		}
		val := kiro[key]
		if isEmptyValue(val) {
			continue
		}
		if err := writeYAMLField(&sb, key, val); err != nil {
			return err
		}
	}

	sb.WriteString("---\n\n")
	sb.WriteString(strings.TrimSpace(content))
	sb.WriteString("\n")

	filePath := filepath.Join(agentsDir, h.metadata.Asset.Name+".md")
	return os.WriteFile(filePath, []byte(sb.String()), 0644)
}

// writeYAMLField appends one `key: value` mapping to the frontmatter, using
// yaml.Marshal so quoting and escaping are always valid YAML
func writeYAMLField(sb *strings.Builder, key string, val any) error {
	out, err := yaml.Marshal(map[string]any{key: val})
	if err != nil {
		return fmt.Errorf("failed to marshal frontmatter field %s: %w", key, err)
	}
	sb.Write(out)
	return nil
}

// normalizePermissions converts the decoded [[agent.kiro.permissions]] value
// into a []any of rule tables. BurntSushi decodes a TOML array of tables held
// in a map[string]any as []map[string]any, while hand-built values and JSON
// decode as []any — both are accepted. A single table
// ([agent.kiro.permissions]) is an authoring mistake that would otherwise be
// dropped silently, so it is rejected with guidance.
func normalizePermissions(val any) ([]any, error) {
	switch p := val.(type) {
	case []any:
		return p, nil
	case []map[string]any:
		rules := make([]any, len(p))
		for i, rule := range p {
			rules[i] = rule
		}
		return rules, nil
	case map[string]any:
		return nil, errors.New("[agent.kiro.permissions] must be an array of tables — use [[agent.kiro.permissions]]")
	default:
		return nil, fmt.Errorf("unsupported permissions value of type %T", val)
	}
}

// writeCLIFormat writes the Kiro CLI v2 agent JSON file.
// All [agent.kiro] fields pass through to JSON except permissions (CLI v2 has no
// permissions model) and the reserved top-level keys (name, prompt, description)
// which are set from Asset metadata and zip content. Unknown fields are preserved
// for forward compatibility — CLI v2 JSON tolerates extra keys (verified by live
// harness test). CLI v3 (kiro-cli --v3) reads the .md file written by writeIDEFormat
// directly.
func (h *AgentHandler) writeCLIFormat(agentsDir string, content string) error {
	config := map[string]any{
		"name":   h.metadata.Asset.Name,
		"prompt": strings.TrimSpace(content),
	}

	if desc := h.getDescription(); desc != "" {
		config["description"] = desc
	}

	// Pass all [agent.kiro] fields through to v2 JSON except the reserved top-level
	// keys (name, prompt, description) set above, and permissions (no v2 permissions
	// model). Unknown fields are preserved (sx forward-compat principle).
	kiro := h.getKiroFields()
	for key, val := range kiro {
		if key == "permissions" || key == "name" || key == "prompt" || key == "description" {
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
		// Fall back to the canonical default name for assets that declare a
		// custom prompt-file but ship the default layout.
		const fallback = "AGENT.md"
		if promptFile == fallback {
			return "", fmt.Errorf("prompt file %q not found in asset", promptFile)
		}
		content, err = utils.ReadZipFile(zipData, fallback)
		if err != nil {
			return "", fmt.Errorf("prompt file not found (tried %q and %q)", promptFile, fallback)
		}
	}

	return string(content), nil
}
