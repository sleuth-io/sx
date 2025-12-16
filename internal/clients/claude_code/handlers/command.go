package handlers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sleuth-io/skills/internal/asset"
	"github.com/sleuth-io/skills/internal/metadata"
	"github.com/sleuth-io/skills/internal/utils"
)

// CommandHandler handles command artifact installation
type CommandHandler struct {
	metadata *metadata.Metadata
}

// NewCommandHandler creates a new command handler
func NewCommandHandler(meta *metadata.Metadata) *CommandHandler {
	return &CommandHandler{
		metadata: meta,
	}
}

// DetectType returns true if files indicate this is a command artifact
func (h *CommandHandler) DetectType(files []string) bool {
	for _, file := range files {
		if file == "COMMAND.md" || file == "command.md" {
			return true
		}
	}
	return false
}

// GetType returns the artifact type string
func (h *CommandHandler) GetType() string {
	return "command"
}

// CreateDefaultMetadata creates default metadata for a command
func (h *CommandHandler) CreateDefaultMetadata(name, version string) *metadata.Metadata {
	return &metadata.Metadata{
		MetadataVersion: "1.0",
		Artifact: metadata.Artifact{
			Name:    name,
			Version: version,
			Type:    asset.TypeCommand,
		},
		Command: &metadata.CommandConfig{
			PromptFile: "COMMAND.md",
		},
	}
}

// GetPromptFile returns the prompt file path for commands
func (h *CommandHandler) GetPromptFile(meta *metadata.Metadata) string {
	if meta.Command != nil {
		return meta.Command.PromptFile
	}
	return ""
}

// GetScriptFile returns empty for commands (not applicable)
func (h *CommandHandler) GetScriptFile(meta *metadata.Metadata) string {
	return ""
}

// ValidateMetadata validates command-specific metadata
func (h *CommandHandler) ValidateMetadata(meta *metadata.Metadata) error {
	if meta.Command == nil {
		return fmt.Errorf("command configuration missing")
	}
	if meta.Command.PromptFile == "" {
		return fmt.Errorf("command prompt-file is required")
	}
	return nil
}

// DetectUsageFromToolCall detects command usage from tool calls
func (h *CommandHandler) DetectUsageFromToolCall(toolName string, toolInput map[string]interface{}) (string, bool) {
	if toolName != "SlashCommand" {
		return "", false
	}
	command, ok := toolInput["command"].(string)
	if !ok {
		return "", false
	}
	// Strip leading slash: "/my-command" -> "my-command"
	commandName := strings.TrimPrefix(command, "/")
	return commandName, true
}

// Install extracts and installs the command artifact
func (h *CommandHandler) Install(ctx context.Context, zipData []byte, targetBase string) error {
	// Validate zip structure
	if err := h.Validate(zipData); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	// Get the prompt file from metadata
	promptFile := h.metadata.Command.PromptFile

	// Read the prompt file from zip
	promptData, err := utils.ReadZipFile(zipData, promptFile)
	if err != nil {
		return fmt.Errorf("failed to read prompt file from zip: %w", err)
	}

	// Determine installation path (commands/{name}.md)
	installPath := filepath.Join(targetBase, h.GetInstallPath())

	// Ensure parent directory exists
	if err := utils.EnsureDir(filepath.Dir(installPath)); err != nil {
		return fmt.Errorf("failed to create commands directory: %w", err)
	}

	// Write the command file
	if err := os.WriteFile(installPath, promptData, 0644); err != nil {
		return fmt.Errorf("failed to write command file: %w", err)
	}

	// Write metadata file for version tracking
	if err := h.writeMetadataFile(zipData, installPath); err != nil {
		return err
	}

	return nil
}

// Remove uninstalls the command artifact
func (h *CommandHandler) Remove(ctx context.Context, targetBase string) error {
	installPath := filepath.Join(targetBase, h.GetInstallPath())

	if !utils.FileExists(installPath) {
		// Already removed or never installed
		return nil
	}

	if err := os.Remove(installPath); err != nil {
		return fmt.Errorf("failed to remove command: %w", err)
	}

	// Remove metadata file if it exists
	h.removeMetadataFile(installPath)

	return nil
}

// GetInstallPath returns the installation path relative to targetBase
func (h *CommandHandler) GetInstallPath() string {
	return filepath.Join("commands", h.metadata.Artifact.Name+".md")
}

// Validate checks if the zip structure is valid for a command artifact
func (h *CommandHandler) Validate(zipData []byte) error {
	// List files in zip
	files, err := utils.ListZipFiles(zipData)
	if err != nil {
		return fmt.Errorf("failed to list zip files: %w", err)
	}

	// Check that metadata.toml exists
	if !containsFile(files, "metadata.toml") {
		return fmt.Errorf("metadata.toml not found in zip")
	}

	// Extract and validate metadata
	metadataBytes, err := utils.ReadZipFile(zipData, "metadata.toml")
	if err != nil {
		return fmt.Errorf("failed to read metadata.toml: %w", err)
	}

	meta, err := metadata.Parse(metadataBytes)
	if err != nil {
		return fmt.Errorf("failed to parse metadata: %w", err)
	}

	// Validate metadata with file list
	if err := meta.ValidateWithFiles(files); err != nil {
		return fmt.Errorf("metadata validation failed: %w", err)
	}

	// Verify artifact type matches
	if meta.Artifact.Type != asset.TypeCommand {
		return fmt.Errorf("artifact type mismatch: expected command, got %s", meta.Artifact.Type)
	}

	// Check that prompt file exists
	if meta.Command == nil {
		return fmt.Errorf("[command] section missing in metadata")
	}

	if !containsFile(files, meta.Command.PromptFile) {
		return fmt.Errorf("prompt file not found in zip: %s", meta.Command.PromptFile)
	}

	return nil
}

// writeMetadataFile writes the metadata file alongside the command for version tracking
func (h *CommandHandler) writeMetadataFile(zipData []byte, installPath string) error {
	metadataPath := strings.TrimSuffix(installPath, ".md") + "-metadata.toml"
	metadataBytes, err := utils.ReadZipFile(zipData, "metadata.toml")
	if err != nil {
		// metadata.toml doesn't exist in zip, that's okay (backwards compatibility)
		return nil
	}

	// Write metadata file alongside the command
	if err := os.WriteFile(metadataPath, metadataBytes, 0644); err != nil {
		return fmt.Errorf("failed to write metadata file: %w", err)
	}

	return nil
}

// removeMetadataFile removes the metadata file if it exists
func (h *CommandHandler) removeMetadataFile(installPath string) {
	metadataPath := strings.TrimSuffix(installPath, ".md") + "-metadata.toml"
	if utils.FileExists(metadataPath) {
		os.Remove(metadataPath) // Ignore errors, metadata is optional
	}
}

// CanDetectInstalledState returns true since commands now preserve metadata via adjacent files
func (h *CommandHandler) CanDetectInstalledState() bool {
	return true
}

// VerifyInstalled checks if the command is properly installed
func (h *CommandHandler) VerifyInstalled(targetBase string) (bool, string) {
	commandPath := filepath.Join(targetBase, h.GetInstallPath())

	if !utils.FileExists(commandPath) {
		return false, "command file not found"
	}

	// Check metadata file for version verification
	metadataPath := strings.TrimSuffix(commandPath, ".md") + "-metadata.toml"
	if !utils.FileExists(metadataPath) {
		// No metadata file - can only verify file exists
		return true, "installed (no version info)"
	}

	meta, err := metadata.ParseFile(metadataPath)
	if err != nil {
		return false, "failed to parse metadata: " + err.Error()
	}

	if meta.Artifact.Version != h.metadata.Artifact.Version {
		return false, fmt.Sprintf("version mismatch: installed %s, expected %s", meta.Artifact.Version, h.metadata.Artifact.Version)
	}

	return true, "installed"
}
