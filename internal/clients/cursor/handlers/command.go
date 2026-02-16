package handlers

import (
	"context"

	"github.com/sleuth-io/sx/internal/handlers/singlefile"
	"github.com/sleuth-io/sx/internal/metadata"
)

// CommandHandler handles command/skill installation for Cursor
type CommandHandler struct {
	*singlefile.Handler
}

// NewCommandHandler creates a new command handler
func NewCommandHandler(meta *metadata.Metadata) *CommandHandler {
	srcFiles := []string{"COMMAND.md", "command.md"}
	if meta.Skill != nil && meta.Skill.PromptFile != "" {
		srcFiles = []string{meta.Skill.PromptFile, "command.md"}
	} else if meta.Command != nil && meta.Command.PromptFile != "" {
		srcFiles = []string{meta.Command.PromptFile, "command.md"}
	}

	return &CommandHandler{
		Handler: singlefile.New(meta, singlefile.Config{
			Dir:       DirCommands,
			Extension: ".md",
			SrcFiles:  srcFiles,
			Transform: nil, // No transform for Cursor commands
		}),
	}
}

// Install delegates to the embedded handler
func (h *CommandHandler) Install(ctx context.Context, zipData []byte, targetBase string) error {
	return h.Handler.Install(ctx, zipData, targetBase)
}

// Remove delegates to the embedded handler
func (h *CommandHandler) Remove(ctx context.Context, targetBase string) error {
	return h.Handler.Remove(ctx, targetBase)
}

// VerifyInstalled delegates to the embedded handler
func (h *CommandHandler) VerifyInstalled(targetBase string) (bool, string) {
	return h.Handler.VerifyInstalled(targetBase)
}
