package handlers

import (
	"context"
	"errors"
	"strings"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/handlers/fileasset"
	"github.com/sleuth-io/sx/internal/metadata"
)

var commandOps = fileasset.NewOperations(DirCommands, &asset.TypeCommand)

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
	return commandOps.Install(ctx, zipData, targetBase, h.metadata.Asset.Name, h.metadata.Command.PromptFile)
}

// Remove uninstalls the command asset
func (h *CommandHandler) Remove(ctx context.Context, targetBase string) error {
	return commandOps.Remove(ctx, targetBase, h.metadata.Asset.Name)
}

// GetInstallPath returns the installation path relative to targetBase
func (h *CommandHandler) GetInstallPath() string {
	return commandOps.GetInstallPath(h.metadata.Asset.Name)
}

// CanDetectInstalledState returns true since commands preserve metadata via adjacent files
func (h *CommandHandler) CanDetectInstalledState() bool {
	return true
}

// VerifyInstalled checks if the command is properly installed
func (h *CommandHandler) VerifyInstalled(targetBase string) (bool, string) {
	return commandOps.VerifyInstalled(targetBase, h.metadata.Asset.Name, h.metadata.Asset.Version)
}
