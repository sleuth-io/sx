package handlers

import (
	"context"

	"github.com/sleuth-io/sx/internal/handlers/singlefile"
	"github.com/sleuth-io/sx/internal/metadata"
)

// CommandHandler handles command asset installation for GitHub Copilot
type CommandHandler struct {
	*singlefile.Handler
}

// NewCommandHandler creates a new command handler
func NewCommandHandler(meta *metadata.Metadata) *CommandHandler {
	srcFiles := []string{"COMMAND.md", "command.md"}
	if meta.Command != nil && meta.Command.PromptFile != "" {
		srcFiles = []string{meta.Command.PromptFile, "command.md"}
	} else if meta.Skill != nil && meta.Skill.PromptFile != "" {
		srcFiles = []string{meta.Skill.PromptFile, "command.md"}
	}

	return &CommandHandler{
		Handler: singlefile.New(meta, singlefile.Config{
			Dir:       DirPrompts,
			Extension: ".prompt.md",
			SrcFiles:  srcFiles,
			Transform: singlefile.WithDescriptionFrontmatter(),
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
