package metadata

import (
	"bytes"
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

// Metadata represents the complete metadata.toml structure
type Metadata struct {
	MetadataVersion string   `toml:"metadata-version,omitempty"`
	Artifact        Artifact `toml:"artifact"`

	// Type-specific sections (only one should be present based on artifact.type)
	Skill   *SkillConfig           `toml:"skill,omitempty"`
	Command *CommandConfig         `toml:"command,omitempty"`
	Agent   *AgentConfig           `toml:"agent,omitempty"`
	Hook    *HookConfig            `toml:"hook,omitempty"`
	MCP     *MCPConfig             `toml:"mcp,omitempty"`
	Custom  map[string]interface{} `toml:"custom,omitempty"`
}

// Artifact represents the [artifact] section
type Artifact struct {
	Name          string   `toml:"name"`
	Version       string   `toml:"version"`
	Type          string   `toml:"type"`
	Description   string   `toml:"description,omitempty"`
	License       string   `toml:"license,omitempty"`
	Authors       []string `toml:"authors,omitempty"`
	Keywords      []string `toml:"keywords,omitempty"`
	Homepage      string   `toml:"homepage,omitempty"`
	Repository    string   `toml:"repository,omitempty"`
	Documentation string   `toml:"documentation,omitempty"`
	Readme        string   `toml:"readme,omitempty"`
	Dependencies  []string `toml:"dependencies,omitempty"`
}

// SkillConfig represents the [skill] section
type SkillConfig struct {
	PromptFile         string   `toml:"prompt-file"`
	Triggers           []string `toml:"triggers,omitempty"`
	Requires           []string `toml:"requires,omitempty"`
	SupportedLanguages []string `toml:"supported-languages,omitempty"`
}

// CommandConfig represents the [command] section
type CommandConfig struct {
	PromptFile   string   `toml:"prompt-file"`
	Aliases      []string `toml:"aliases,omitempty"`
	RequiresAuth bool     `toml:"requires-auth,omitempty"`
	Dangerous    bool     `toml:"dangerous,omitempty"`
}

// AgentConfig represents the [agent] section
type AgentConfig struct {
	PromptFile string   `toml:"prompt-file"`
	Triggers   []string `toml:"triggers,omitempty"`
	Requires   []string `toml:"requires,omitempty"`
}

// HookConfig represents the [hook] section
type HookConfig struct {
	Event       string `toml:"event"`
	ScriptFile  string `toml:"script-file"`
	Async       bool   `toml:"async,omitempty"`
	FailOnError bool   `toml:"fail-on-error,omitempty"`
	Timeout     int    `toml:"timeout,omitempty"`
}

// MCPConfig represents the [mcp] section (for both mcp and mcp-remote)
type MCPConfig struct {
	Command      string            `toml:"command"`
	Args         []string          `toml:"args"`
	Env          map[string]string `toml:"env,omitempty"`
	Timeout      int               `toml:"timeout,omitempty"`
	Capabilities []string          `toml:"capabilities,omitempty"`
}

// Parse parses metadata from bytes
func Parse(data []byte) (*Metadata, error) {
	var metadata Metadata

	if err := toml.Unmarshal(data, &metadata); err != nil {
		return nil, fmt.Errorf("failed to parse metadata: %w", err)
	}

	return &metadata, nil
}

// ParseFile parses metadata from a file path
func ParseFile(filePath string) (*Metadata, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read metadata file: %w", err)
	}

	return Parse(data)
}

// Marshal converts metadata to TOML bytes
func Marshal(metadata *Metadata) ([]byte, error) {
	buf := new(bytes.Buffer)
	encoder := toml.NewEncoder(buf)

	if err := encoder.Encode(metadata); err != nil {
		return nil, fmt.Errorf("failed to marshal metadata: %w", err)
	}

	return buf.Bytes(), nil
}

// Write writes metadata to a file path
func Write(metadata *Metadata, filePath string) error {
	data, err := Marshal(metadata)
	if err != nil {
		return err
	}

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write metadata file: %w", err)
	}

	return nil
}

// GetTypeConfig returns the type-specific configuration section
func (m *Metadata) GetTypeConfig() interface{} {
	switch m.Artifact.Type {
	case "skill":
		return m.Skill
	case "command":
		return m.Command
	case "agent":
		return m.Agent
	case "hook":
		return m.Hook
	case "mcp", "mcp-remote":
		return m.MCP
	}
	return nil
}

// GetPromptFile returns the prompt file path for prompt-based artifacts
func (m *Metadata) GetPromptFile() string {
	switch m.Artifact.Type {
	case "skill":
		if m.Skill != nil {
			return m.Skill.PromptFile
		}
	case "command":
		if m.Command != nil {
			return m.Command.PromptFile
		}
	case "agent":
		if m.Agent != nil {
			return m.Agent.PromptFile
		}
	}
	return ""
}

// GetScriptFile returns the script file path for script-based artifacts
func (m *Metadata) GetScriptFile() string {
	if m.Artifact.Type == "hook" && m.Hook != nil {
		return m.Hook.ScriptFile
	}
	return ""
}

// GetRequiredFiles returns a list of files that must exist in the artifact
func (m *Metadata) GetRequiredFiles() []string {
	var files []string

	// Add prompt or script file
	if promptFile := m.GetPromptFile(); promptFile != "" {
		files = append(files, promptFile)
	}
	if scriptFile := m.GetScriptFile(); scriptFile != "" {
		files = append(files, scriptFile)
	}

	// Add readme if specified
	if m.Artifact.Readme != "" {
		files = append(files, m.Artifact.Readme)
	}

	return files
}
