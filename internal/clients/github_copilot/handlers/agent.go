package handlers

import (
	"context"

	"github.com/sleuth-io/sx/internal/handlers/singlefile"
	"github.com/sleuth-io/sx/internal/metadata"
)

// AgentHandler handles agent asset installation for GitHub Copilot
type AgentHandler struct {
	*singlefile.Handler
}

// NewAgentHandler creates a new agent handler
func NewAgentHandler(meta *metadata.Metadata) *AgentHandler {
	srcFiles := []string{"AGENT.md", "agent.md"}
	if meta.Agent != nil && meta.Agent.PromptFile != "" {
		srcFiles = []string{meta.Agent.PromptFile, "agent.md"}
	}

	return &AgentHandler{
		Handler: singlefile.New(meta, singlefile.Config{
			Dir:       DirAgents,
			Extension: ".agent.md",
			SrcFiles:  srcFiles,
			Transform: singlefile.WithDescriptionFrontmatter(),
		}),
	}
}

// Install delegates to the embedded handler
func (h *AgentHandler) Install(ctx context.Context, zipData []byte, targetBase string) error {
	return h.Handler.Install(ctx, zipData, targetBase)
}

// Remove delegates to the embedded handler
func (h *AgentHandler) Remove(ctx context.Context, targetBase string) error {
	return h.Handler.Remove(ctx, targetBase)
}

// VerifyInstalled delegates to the embedded handler
func (h *AgentHandler) VerifyInstalled(targetBase string) (bool, string) {
	return h.Handler.VerifyInstalled(targetBase)
}
