package handlers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sleuth-io/sx/internal/metadata"
	"github.com/sleuth-io/sx/internal/utils"
)

// SkillHandler handles skill asset installation for Gemini
// Skills are converted to Gemini custom commands (.toml files)
type SkillHandler struct {
	metadata *metadata.Metadata
}

// NewSkillHandler creates a new skill handler
func NewSkillHandler(meta *metadata.Metadata) *SkillHandler {
	return &SkillHandler{metadata: meta}
}

// Install converts an sx skill to a Gemini custom command TOML file
func (h *SkillHandler) Install(ctx context.Context, zipData []byte, targetBase string) error {
	// Read the prompt content from the zip
	promptContent, err := h.readPromptContent(zipData)
	if err != nil {
		return fmt.Errorf("failed to read prompt content: %w", err)
	}

	// Convert sx syntax to Gemini syntax
	convertedPrompt := h.convertPromptSyntax(promptContent)

	// Build the TOML content
	tomlContent := h.buildTOMLContent(convertedPrompt)

	// Ensure commands directory exists
	// For global scope, targetBase is already ~/.gemini, so we only add commands/
	// For repo scope, targetBase is /repo, so we add .gemini/commands/
	commandsDir := filepath.Join(targetBase, ConfigDir, DirCommands)
	if filepath.Base(targetBase) == ConfigDir {
		// Global scope: targetBase is already ~/.gemini
		commandsDir = filepath.Join(targetBase, DirCommands)
	}
	if err := os.MkdirAll(commandsDir, 0755); err != nil {
		return fmt.Errorf("failed to create commands directory: %w", err)
	}

	// Write the TOML file
	tomlPath := filepath.Join(commandsDir, h.metadata.Asset.Name+".toml")
	if err := os.WriteFile(tomlPath, []byte(tomlContent), 0644); err != nil {
		return fmt.Errorf("failed to write command file: %w", err)
	}

	return nil
}

// Remove removes the skill's TOML command file
func (h *SkillHandler) Remove(ctx context.Context, targetBase string) error {
	// For global scope, targetBase is already ~/.gemini
	commandsDir := filepath.Join(targetBase, ConfigDir, DirCommands)
	if filepath.Base(targetBase) == ConfigDir {
		commandsDir = filepath.Join(targetBase, DirCommands)
	}
	tomlPath := filepath.Join(commandsDir, h.metadata.Asset.Name+".toml")

	if _, err := os.Stat(tomlPath); os.IsNotExist(err) {
		return nil // Already removed
	}

	if err := os.Remove(tomlPath); err != nil {
		return fmt.Errorf("failed to remove command file: %w", err)
	}

	return nil
}

// VerifyInstalled checks if the skill's TOML command file exists
func (h *SkillHandler) VerifyInstalled(targetBase string) (bool, string) {
	// For global scope, targetBase is already ~/.gemini
	commandsDir := filepath.Join(targetBase, ConfigDir, DirCommands)
	if filepath.Base(targetBase) == ConfigDir {
		commandsDir = filepath.Join(targetBase, DirCommands)
	}
	tomlPath := filepath.Join(commandsDir, h.metadata.Asset.Name+".toml")

	if _, err := os.Stat(tomlPath); err == nil {
		return true, "Found at " + tomlPath
	}

	return false, "Command file not found"
}

// readPromptContent reads the skill/command prompt from the zip
func (h *SkillHandler) readPromptContent(zipData []byte) (string, error) {
	promptFile := DefaultSkillPromptFile
	if h.metadata.Skill != nil && h.metadata.Skill.PromptFile != "" {
		promptFile = h.metadata.Skill.PromptFile
	} else if h.metadata.Command != nil && h.metadata.Command.PromptFile != "" {
		promptFile = h.metadata.Command.PromptFile
	}

	content, err := utils.ReadZipFile(zipData, promptFile)
	if err != nil {
		// Try lowercase variant
		content, err = utils.ReadZipFile(zipData, "skill.md")
		if err != nil {
			// Try COMMAND.md for command assets
			content, err = utils.ReadZipFile(zipData, "COMMAND.md")
			if err != nil {
				content, err = utils.ReadZipFile(zipData, "command.md")
				if err != nil {
					return "", fmt.Errorf("prompt file not found: %s", promptFile)
				}
			}
		}
	}

	return string(content), nil
}

// convertPromptSyntax converts sx prompt syntax to Gemini syntax
func (h *SkillHandler) convertPromptSyntax(content string) string {
	// Convert $ARGUMENTS to {{args}}
	content = strings.ReplaceAll(content, "$ARGUMENTS", "{{args}}")

	// Convert @file references to Gemini's @{file} syntax
	// sx uses @./path/file.md or @/absolute/path or @path/file.md
	// Gemini uses @{path/file.md}
	// Only convert explicit file path references:
	// - @./ for relative paths (e.g., @./src/file.ts)
	// - @/ for absolute paths (e.g., @/etc/config)
	// We intentionally do NOT convert @user/repo or @org/package style references
	// as those are GitHub/npm references, not file paths.
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		// Look for @./ (relative path)
		if before, after, found := strings.Cut(line, "@./"); found {
			// Convert @./path to @{path}
			endIdx := findPathEnd(after)
			if endIdx > 0 {
				path := after[:endIdx]
				lines[i] = before + "@{" + path + "}" + after[endIdx:]
			}
		} else if idx := strings.Index(line, "@/"); idx != -1 && idx+1 < len(line) {
			// Look for @/ (absolute path) - must start with /
			rest := line[idx+1:]
			endIdx := findPathEnd(rest)
			if endIdx > 0 {
				path := rest[:endIdx]
				lines[i] = line[:idx] + "@{" + path + "}" + rest[endIdx:]
			}
		}
	}

	return strings.Join(lines, "\n")
}

// findPathEnd finds the end of a file path in a string
func findPathEnd(s string) int {
	for i, c := range s {
		if c == ' ' || c == '\t' || c == '\n' || c == ')' || c == ']' || c == '}' {
			return i
		}
	}
	return len(s)
}

// buildTOMLContent creates the TOML content for the Gemini command
func (h *SkillHandler) buildTOMLContent(prompt string) string {
	var sb strings.Builder

	// Add description if available
	description := h.getDescription()
	if description != "" {
		fmt.Fprintf(&sb, "description = %q\n", description)
	}

	// Add the prompt as a multi-line string
	sb.WriteString("prompt = \"\"\"\n")
	sb.WriteString(strings.TrimSpace(prompt))
	sb.WriteString("\n\"\"\"\n")

	return sb.String()
}

// getDescription returns the skill description
func (h *SkillHandler) getDescription() string {
	if h.metadata.Asset.Description != "" {
		return h.metadata.Asset.Description
	}
	return ""
}
