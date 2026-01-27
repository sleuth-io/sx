package handlers

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/handlers/instruction"
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/metadata"
	"github.com/sleuth-io/sx/internal/utils"
)

// InstructionHandler handles instruction asset installation for Claude Code
type InstructionHandler struct {
	metadata   *metadata.Metadata
	lockConfig *lockfile.InstructionInstallConfig
}

// NewInstructionHandler creates a new instruction handler
func NewInstructionHandler(meta *metadata.Metadata, lockConfig *lockfile.InstructionInstallConfig) *InstructionHandler {
	if lockConfig == nil {
		lockConfig = &lockfile.InstructionInstallConfig{
			Heading:   lockfile.DefaultInstructionHeading,
			EndMarker: lockfile.DefaultInstructionEndMarker,
		}
	}
	return &InstructionHandler{
		metadata:   meta,
		lockConfig: lockConfig,
	}
}

// Install injects the instruction into CLAUDE.md (or AGENTS.md if configured)
func (h *InstructionHandler) Install(ctx context.Context, zipData []byte, targetBase string) error {
	// Read instruction content from zip
	content, err := h.readInstructionContent(zipData)
	if err != nil {
		return fmt.Errorf("failed to read instruction content: %w", err)
	}

	// Determine target file (CLAUDE.md or AGENTS.md)
	targetFile := h.getTargetFile(targetBase)

	// Get title (default to asset name)
	title := h.getTitle()

	// Create injection
	inj := instruction.Injection{
		Name:    h.metadata.Asset.Name,
		Title:   title,
		Content: content,
	}

	// Read existing instructions and add/update this one
	existing := h.readExistingInstructions(targetFile)
	instructions := h.mergeInstruction(existing, inj)

	// Inject all instructions
	if err := instruction.InjectInstructions(
		targetFile,
		h.lockConfig.Heading,
		h.lockConfig.EndMarker,
		instructions,
	); err != nil {
		return fmt.Errorf("failed to inject instruction: %w", err)
	}

	return nil
}

// Remove removes the instruction from the target file
func (h *InstructionHandler) Remove(ctx context.Context, targetBase string) error {
	targetFile := h.getTargetFile(targetBase)

	return instruction.RemoveInstruction(
		targetFile,
		h.lockConfig.Heading,
		h.lockConfig.EndMarker,
		h.metadata.Asset.Name,
	)
}

// GetInstallPath returns a description of where the instruction is installed
func (h *InstructionHandler) GetInstallPath() string {
	return "CLAUDE.md (or AGENTS.md)"
}

// CanDetectInstalledState returns true since we can check for the instruction in the file
func (h *InstructionHandler) CanDetectInstalledState() bool {
	return true
}

// VerifyInstalled checks if the instruction exists in the target file
func (h *InstructionHandler) VerifyInstalled(targetBase string) (bool, string) {
	targetFile := h.getTargetFile(targetBase)

	if instruction.InstructionExists(
		targetFile,
		h.lockConfig.Heading,
		h.lockConfig.EndMarker,
		h.metadata.Asset.Name,
	) {
		return true, "Found in " + filepath.Base(targetFile)
	}

	return false, "Not found in " + filepath.Base(targetFile)
}

// getTargetFile determines whether to use CLAUDE.md or AGENTS.md
func (h *InstructionHandler) getTargetFile(targetBase string) string {
	if shouldUseAgentsMd(targetBase) {
		return filepath.Join(targetBase, "AGENTS.md")
	}
	return filepath.Join(targetBase, "CLAUDE.md")
}

// getTitle returns the instruction title, defaulting to asset name
func (h *InstructionHandler) getTitle() string {
	if h.metadata.Instruction != nil && h.metadata.Instruction.Title != "" {
		return h.metadata.Instruction.Title
	}
	return h.metadata.Asset.Name
}

// getPromptFile returns the prompt file name, defaulting to INSTRUCTION.md
func (h *InstructionHandler) getPromptFile() string {
	if h.metadata.Instruction != nil && h.metadata.Instruction.PromptFile != "" {
		return h.metadata.Instruction.PromptFile
	}
	return instruction.DefaultPromptFile
}

// readInstructionContent reads the instruction content from the zip
func (h *InstructionHandler) readInstructionContent(zipData []byte) (string, error) {
	promptFile := h.getPromptFile()

	content, err := utils.ReadZipFile(zipData, promptFile)
	if err != nil {
		// Try lowercase variant
		content, err = utils.ReadZipFile(zipData, "instruction.md")
		if err != nil {
			return "", fmt.Errorf("prompt file not found: %s", promptFile)
		}
	}

	return string(content), nil
}

// readExistingInstructions reads existing instructions from the target file
func (h *InstructionHandler) readExistingInstructions(targetFile string) []instruction.Injection {
	content, err := os.ReadFile(targetFile)
	if err != nil {
		return nil
	}

	return instruction.ExtractInstructions(
		string(content),
		h.lockConfig.Heading,
		h.lockConfig.EndMarker,
	)
}

// mergeInstruction adds or updates an instruction in the list
func (h *InstructionHandler) mergeInstruction(existing []instruction.Injection, new instruction.Injection) []instruction.Injection {
	for i, inst := range existing {
		if inst.Name == new.Name {
			existing[i] = new
			return existing
		}
	}
	return append(existing, new)
}

// AGENTS.md detection

// agentsReferencePattern matches @AGENTS.md import pattern in CLAUDE.md
var agentsReferencePattern = regexp.MustCompile(`(?m)^\s*@AGENTS\.md\s*$`)

// shouldUseAgentsMd detects if CLAUDE.md defers to AGENTS.md
func shouldUseAgentsMd(dir string) bool {
	claudePath := filepath.Join(dir, "CLAUDE.md")
	agentsPath := filepath.Join(dir, "AGENTS.md")

	// Check if AGENTS.md exists
	if _, err := os.Stat(agentsPath); os.IsNotExist(err) {
		return false
	}

	// Check if CLAUDE.md is a symlink to AGENTS.md
	if isSymlinkTo(claudePath, agentsPath) {
		return true
	}

	// Check if CLAUDE.md contains @AGENTS.md reference
	if containsAgentsReference(claudePath) {
		return true
	}

	return false
}

// isSymlinkTo checks if source is a symlink pointing to target
func isSymlinkTo(source, target string) bool {
	info, err := os.Lstat(source)
	if err != nil {
		return false
	}

	if info.Mode()&os.ModeSymlink == 0 {
		return false
	}

	linkTarget, err := os.Readlink(source)
	if err != nil {
		return false
	}

	if !filepath.IsAbs(linkTarget) {
		linkTarget = filepath.Join(filepath.Dir(source), linkTarget)
	}

	sourceAbs, _ := filepath.Abs(linkTarget)
	targetAbs, _ := filepath.Abs(target)

	return sourceAbs == targetAbs
}

// containsAgentsReference checks if file contains @AGENTS.md import
func containsAgentsReference(path string) bool {
	content, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return agentsReferencePattern.Match(content)
}

// Validate checks if the zip structure is valid for an instruction asset
func (h *InstructionHandler) Validate(zipData []byte) error {
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

	if meta.Asset.Type != asset.TypeInstruction {
		return fmt.Errorf("asset type mismatch: expected instruction, got %s", meta.Asset.Type)
	}

	// Check that prompt file exists
	promptFile := instruction.DefaultPromptFile
	if meta.Instruction != nil && meta.Instruction.PromptFile != "" {
		promptFile = meta.Instruction.PromptFile
	}

	if !containsFile(files, promptFile) && !containsFile(files, "instruction.md") {
		return fmt.Errorf("prompt file not found in zip: %s", promptFile)
	}

	return nil
}
