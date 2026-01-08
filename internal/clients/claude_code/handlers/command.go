package handlers

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/handlers/fileasset"
	"github.com/sleuth-io/sx/internal/metadata"
	"github.com/sleuth-io/sx/internal/utils"
)

var commandOps = fileasset.NewOperations("commands", &asset.TypeCommand)

// CommandHandler handles command asset installation
type CommandHandler struct {
	metadata *metadata.Metadata
}

// NewCommandHandler creates a new command handler
func NewCommandHandler(meta *metadata.Metadata) *CommandHandler {
	return &CommandHandler{
		metadata: meta,
	}
}

// DetectType returns true if files indicate this is a command asset
func (h *CommandHandler) DetectType(files []string) bool {
	for _, file := range files {
		if file == "COMMAND.md" || file == "command.md" {
			return true
		}
	}
	return false
}

// GetType returns the asset type string
func (h *CommandHandler) GetType() string {
	return "command"
}

// CreateDefaultMetadata creates default metadata for a command
func (h *CommandHandler) CreateDefaultMetadata(name, version string) *metadata.Metadata {
	return &metadata.Metadata{
		MetadataVersion: "1.0",
		Asset: metadata.Asset{
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
		return errors.New("command configuration missing")
	}
	if meta.Command.PromptFile == "" {
		return errors.New("command prompt-file is required")
	}
	return nil
}

// DetectUsageFromToolCall detects command usage from tool calls
func (h *CommandHandler) DetectUsageFromToolCall(toolName string, toolInput map[string]any) (string, bool) {
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

// Install extracts and installs the command asset as a single .md file
func (h *CommandHandler) Install(ctx context.Context, zipData []byte, targetBase string) error {
	// Validate zip structure
	if err := h.Validate(zipData); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	// Get the prompt file from metadata
	promptFile := h.metadata.Command.PromptFile

	return commandOps.Install(ctx, zipData, targetBase, h.metadata.Asset.Name, promptFile)
}

// Remove uninstalls the command asset
func (h *CommandHandler) Remove(ctx context.Context, targetBase string) error {
	return commandOps.Remove(ctx, targetBase, h.metadata.Asset.Name)
}

// GetInstallPath returns the installation path relative to targetBase
func (h *CommandHandler) GetInstallPath() string {
	return commandOps.GetInstallPath(h.metadata.Asset.Name)
}

// Validate checks if the zip structure is valid for a command asset
func (h *CommandHandler) Validate(zipData []byte) error {
	// List files in zip
	files, err := utils.ListZipFiles(zipData)
	if err != nil {
		return fmt.Errorf("failed to list zip files: %w", err)
	}

	// Check that metadata.toml exists
	if !containsFile(files, "metadata.toml") {
		return errors.New("metadata.toml not found in zip")
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

	// Verify asset type matches
	if meta.Asset.Type != asset.TypeCommand {
		return fmt.Errorf("asset type mismatch: expected command, got %s", meta.Asset.Type)
	}

	// Check that prompt file exists
	if meta.Command == nil {
		return errors.New("[command] section missing in metadata")
	}

	if !containsFile(files, meta.Command.PromptFile) {
		return fmt.Errorf("prompt file not found in zip: %s", meta.Command.PromptFile)
	}

	return nil
}

// CanDetectInstalledState returns true since commands preserve metadata via adjacent files
func (h *CommandHandler) CanDetectInstalledState() bool {
	return true
}

// VerifyInstalled checks if the command is properly installed
func (h *CommandHandler) VerifyInstalled(targetBase string) (bool, string) {
	return commandOps.VerifyInstalled(targetBase, h.metadata.Asset.Name, h.metadata.Asset.Version)
}
