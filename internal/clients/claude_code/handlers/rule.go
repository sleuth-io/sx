package handlers

import (
	"context"
	"errors"
	"fmt"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/handlers/rule"
	"github.com/sleuth-io/sx/internal/handlers/singlefile"
	"github.com/sleuth-io/sx/internal/metadata"
	"github.com/sleuth-io/sx/internal/utils"
)

// RuleHandler handles rule asset installation for Claude Code
type RuleHandler struct {
	*singlefile.Handler
	metadata *metadata.Metadata
}

// NewRuleHandler creates a new rule handler
// Note: lockConfig parameter is deprecated and ignored (kept for API compatibility)
func NewRuleHandler(meta *metadata.Metadata, _ any) *RuleHandler {
	srcFiles := []string{rule.DefaultPromptFile, "rule.md"}
	if meta.Rule != nil && meta.Rule.PromptFile != "" {
		srcFiles = []string{meta.Rule.PromptFile, "rule.md"}
	}

	return &RuleHandler{
		Handler: singlefile.New(meta, singlefile.Config{
			Dir:       "rules",
			Extension: ".md",
			SrcFiles:  srcFiles,
			Transform: singlefile.WithRuleFrontmatter("paths"), // Claude uses "paths" field
		}),
		metadata: meta,
	}
}

// Install delegates to the embedded handler
func (h *RuleHandler) Install(ctx context.Context, zipData []byte, targetBase string) error {
	return h.Handler.Install(ctx, zipData, targetBase)
}

// Remove delegates to the embedded handler
func (h *RuleHandler) Remove(ctx context.Context, targetBase string) error {
	return h.Handler.Remove(ctx, targetBase)
}

// GetInstallPath returns a description of where the rule is installed
func (h *RuleHandler) GetInstallPath() string {
	return ".claude/rules/"
}

// CanDetectInstalledState returns true since we can check for the rule file
func (h *RuleHandler) CanDetectInstalledState() bool {
	return true
}

// VerifyInstalled delegates to the embedded handler
func (h *RuleHandler) VerifyInstalled(targetBase string) (bool, string) {
	return h.Handler.VerifyInstalled(targetBase)
}

// Validate checks if the zip structure is valid for a rule asset
func (h *RuleHandler) Validate(zipData []byte) error {
	files, err := utils.ListZipFiles(zipData)
	if err != nil {
		return fmt.Errorf("failed to list zip files: %w", err)
	}

	if !containsFile(files, "metadata.toml") {
		return errors.New("metadata.toml not found in zip")
	}

	metadataBytes, err := utils.ReadZipFile(zipData, "metadata.toml")
	if err != nil {
		return fmt.Errorf("failed to read metadata.toml: %w", err)
	}

	meta, err := metadata.Parse(metadataBytes)
	if err != nil {
		return fmt.Errorf("failed to parse metadata: %w", err)
	}

	if err := meta.ValidateWithFiles(files); err != nil {
		return fmt.Errorf("metadata validation failed: %w", err)
	}

	if meta.Asset.Type != asset.TypeRule {
		return fmt.Errorf("asset type mismatch: expected rule, got %s", meta.Asset.Type)
	}

	// Check that prompt file exists
	promptFile := rule.DefaultPromptFile
	if meta.Rule != nil && meta.Rule.PromptFile != "" {
		promptFile = meta.Rule.PromptFile
	}

	if !containsFile(files, promptFile) && !containsFile(files, "rule.md") {
		return fmt.Errorf("prompt file not found in zip: %s", promptFile)
	}

	return nil
}
