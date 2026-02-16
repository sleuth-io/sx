package handlers

import (
	"context"

	"github.com/sleuth-io/sx/internal/handlers/rule"
	"github.com/sleuth-io/sx/internal/handlers/singlefile"
	"github.com/sleuth-io/sx/internal/metadata"
)

// RuleHandler handles rule asset installation for Cursor
type RuleHandler struct {
	*singlefile.Handler
}

// NewRuleHandler creates a new rule handler
func NewRuleHandler(meta *metadata.Metadata, pathScope string) *RuleHandler {
	srcFiles := []string{rule.DefaultPromptFile, "rule.md"}
	if meta.Rule != nil && meta.Rule.PromptFile != "" {
		srcFiles = []string{meta.Rule.PromptFile, "rule.md"}
	}

	// Build transform with pathScope for glob generation
	transform := buildCursorRuleTransform(meta, pathScope)

	return &RuleHandler{
		Handler: singlefile.New(meta, singlefile.Config{
			Dir:       "rules",
			Extension: ".mdc",
			SrcFiles:  srcFiles,
			Transform: transform,
		}),
	}
}

// buildCursorRuleTransform creates the MDC frontmatter transform
func buildCursorRuleTransform(meta *metadata.Metadata, pathScope string) func(*metadata.Metadata, []byte) []byte {
	return singlefile.WithCursorRuleFrontmatter(pathScope)
}

// Install delegates to the embedded handler
func (h *RuleHandler) Install(ctx context.Context, zipData []byte, targetBase string) error {
	return h.Handler.Install(ctx, zipData, targetBase)
}

// Remove delegates to the embedded handler
func (h *RuleHandler) Remove(ctx context.Context, targetBase string) error {
	return h.Handler.Remove(ctx, targetBase)
}

// VerifyInstalled delegates to the embedded handler
func (h *RuleHandler) VerifyInstalled(targetBase string) (bool, string) {
	return h.Handler.VerifyInstalled(targetBase)
}
