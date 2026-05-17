package handlers

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/sleuth-io/sx/internal/metadata"
	"github.com/sleuth-io/sx/internal/utils"
)

// RuleHandler installs rules into OpenCode. OpenCode reads rule files
// through the top-level `instructions` array in opencode.json, so a rule
// install both materializes the markdown content at <config>/rules/<name>.md
// and registers that path in opencode.json. Repo-scoped installs register a
// path relative to the project root; global installs register an absolute
// path under ~/.config/opencode.
type RuleHandler struct {
	metadata     *metadata.Metadata
	registerPath string
}

// NewRuleHandler creates a new OpenCode rule handler. registerPath is the
// string that should be written into opencode.json's `instructions` array;
// pass the absolute path for global installs or a path relative to the
// config file's directory for project installs.
func NewRuleHandler(meta *metadata.Metadata, registerPath string) *RuleHandler {
	return &RuleHandler{metadata: meta, registerPath: registerPath}
}

// Install writes the rule markdown to <targetBase>/rules/<name>.md and
// adds the configured registerPath to opencode.json's instructions array.
func (h *RuleHandler) Install(ctx context.Context, zipData []byte, targetBase string) error {
	rulesDir := filepath.Join(targetBase, DirRules)
	if err := os.MkdirAll(rulesDir, 0755); err != nil {
		return fmt.Errorf("failed to create rules directory: %w", err)
	}

	promptFile := h.getPromptFile()
	if promptFile == "" {
		return errors.New("no prompt file specified in metadata")
	}

	content, err := utils.ReadZipFile(zipData, promptFile)
	if err != nil {
		// Fall back to lowercase rule.md for compatibility with packagers
		// that don't follow the uppercase convention.
		content, err = utils.ReadZipFile(zipData, "rule.md")
		if err != nil {
			return fmt.Errorf("failed to read prompt file: %w", err)
		}
	}

	destPath := filepath.Join(rulesDir, h.metadata.Asset.Name+".md")
	if err := os.WriteFile(destPath, content, 0644); err != nil {
		return fmt.Errorf("failed to write rule file: %w", err)
	}

	configPath := filepath.Join(targetBase, ConfigFile)
	return AddInstruction(configPath, h.registerPath)
}

// Remove deletes the rule file and unregisters its instructions entry.
func (h *RuleHandler) Remove(ctx context.Context, targetBase string) error {
	configPath := filepath.Join(targetBase, ConfigFile)
	if err := RemoveInstruction(configPath, h.registerPath); err != nil {
		return err
	}

	filePath := filepath.Join(targetBase, DirRules, h.metadata.Asset.Name+".md")
	if err := os.Remove(filePath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to remove rule file: %w", err)
	}
	return nil
}

// VerifyInstalled returns whether the rule file exists on disk. We don't
// also check the instructions registration because the verify caller may
// not know the exact register path the original install used (absolute vs.
// relative depends on scope at install time), but the file's presence
// under <config>/rules/<name>.md is a stable signal owned by sx.
func (h *RuleHandler) VerifyInstalled(targetBase string) (bool, string) {
	filePath := filepath.Join(targetBase, DirRules, h.metadata.Asset.Name+".md")
	if !utils.FileExists(filePath) {
		return false, "rule file not found"
	}
	return true, "installed"
}

func (h *RuleHandler) getPromptFile() string {
	if h.metadata.Rule != nil && h.metadata.Rule.PromptFile != "" {
		return h.metadata.Rule.PromptFile
	}
	return DefaultRulePromptFile
}

// AddInstruction appends path to opencode.json's `instructions` array if
// it isn't already present.
func AddInstruction(configPath, path string) error {
	if path == "" {
		return errors.New("empty instruction path")
	}

	config, err := ReadOpenCodeConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to read opencode.json: %w", err)
	}

	if !slices.Contains(config.Instructions, path) {
		config.Instructions = append(config.Instructions, path)
	}

	if err := WriteOpenCodeConfig(configPath, config); err != nil {
		return fmt.Errorf("failed to write opencode.json: %w", err)
	}
	return nil
}

// RemoveInstruction removes path from opencode.json's `instructions` array.
// If the config file doesn't exist, this is a no-op.
func RemoveInstruction(configPath, path string) error {
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return nil
	}

	config, err := ReadOpenCodeConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to read opencode.json: %w", err)
	}

	filtered := config.Instructions[:0]
	for _, p := range config.Instructions {
		if p != path {
			filtered = append(filtered, p)
		}
	}
	config.Instructions = filtered

	if err := WriteOpenCodeConfig(configPath, config); err != nil {
		return fmt.Errorf("failed to write opencode.json: %w", err)
	}
	return nil
}
