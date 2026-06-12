package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/clients"
	"github.com/sleuth-io/sx/internal/metadata"
	"github.com/sleuth-io/sx/internal/utils"
)

// isSingleFileAsset checks if the path is a single file that can be treated as an asset.
// This includes agents, commands, skills, and rule files.
func isSingleFileAsset(path string) bool {
	// Check if any client or fallback recognizes this as an asset
	return clients.DetectAssetType(path, nil) != nil
}

// createZipFromSingleFile creates a zip archive from a single file.
// Detects asset type from path and content, creates appropriate metadata.
func createZipFromSingleFile(filePath string) ([]byte, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	// Ask clients/fallback to detect the asset type
	detectedType := clients.DetectAssetType(filePath, content)
	if detectedType == nil {
		return nil, fmt.Errorf("unrecognized file type: %s", filePath)
	}

	switch *detectedType {
	case asset.TypeRule:
		return createZipFromRuleFile(filePath)
	case asset.TypeAgent:
		if strings.EqualFold(filepath.Ext(filePath), ".toml") {
			return createZipFromCodexAgentFile(filePath, content)
		}
		return createZipFromPromptFile(filePath, *detectedType, content)
	case asset.TypeCommand, asset.TypeSkill:
		return createZipFromPromptFile(filePath, *detectedType, content)
	default:
		return nil, fmt.Errorf("unsupported asset type: %s", detectedType.Label)
	}
}

type codexAgentDefinition struct {
	Name                  string `toml:"name"`
	Description           string `toml:"description"`
	DeveloperInstructions string `toml:"developer_instructions"`
}

func createZipFromCodexAgentFile(filePath string, content []byte) ([]byte, error) {
	return createZipFromCodexAgentContent(filePath, content)
}

func createZipFromCodexAgentContent(source string, content []byte) ([]byte, error) {
	var def codexAgentDefinition
	if err := toml.Unmarshal(content, &def); err != nil {
		return nil, fmt.Errorf("failed to parse Codex agent TOML: %w", err)
	}

	def.Name = strings.TrimSpace(def.Name)
	def.Description = strings.TrimSpace(def.Description)
	def.DeveloperInstructions = strings.TrimSpace(def.DeveloperInstructions)

	if def.Name == "" {
		return nil, fmt.Errorf("codex agent TOML %s is missing required field: name", source)
	}
	if def.Description == "" {
		return nil, fmt.Errorf("codex agent TOML %s is missing required field: description", source)
	}
	if def.DeveloperInstructions == "" {
		return nil, fmt.Errorf("codex agent TOML %s is missing required field: developer_instructions", source)
	}

	promptFileName := def.Name + ".toml"
	zipData, err := utils.CreateZipFromContent(promptFileName, content)
	if err != nil {
		return nil, err
	}

	meta := &metadata.Metadata{
		MetadataVersion: "1.0",
		Asset: metadata.Asset{
			Name:        def.Name,
			Type:        asset.TypeAgent,
			Version:     "1.0",
			Description: def.Description,
			Clients:     []string{clients.ClientIDCodex},
		},
		Agent: &metadata.AgentConfig{PromptFile: promptFileName},
	}

	metaBytes, err := metadata.Marshal(meta)
	if err != nil {
		return nil, err
	}

	return utils.AddFileToZip(zipData, "metadata.toml", metaBytes)
}

// createZipFromPromptFile creates a zip for agent/command/skill files
func createZipFromPromptFile(filePath string, assetType asset.Type, content []byte) ([]byte, error) {
	promptFileName := filepath.Base(filePath)
	assetName := strings.TrimSuffix(promptFileName, filepath.Ext(promptFileName))

	// Create zip with the original filename
	zipData, err := utils.CreateZipFromContent(promptFileName, content)
	if err != nil {
		return nil, err
	}

	// Create metadata
	meta := &metadata.Metadata{
		MetadataVersion: "1.0",
		Asset: metadata.Asset{
			Name:    assetName,
			Type:    assetType,
			Version: "1.0", // Default version, user will confirm
		},
	}

	// Set appropriate config based on type
	switch assetType {
	case asset.TypeAgent:
		meta.Agent = &metadata.AgentConfig{PromptFile: promptFileName}
	case asset.TypeCommand:
		meta.Command = &metadata.CommandConfig{PromptFile: promptFileName}
	case asset.TypeSkill:
		meta.Skill = &metadata.SkillConfig{PromptFile: promptFileName}
	}

	// Add metadata.toml to zip
	metaBytes, err := metadata.Marshal(meta)
	if err != nil {
		return nil, err
	}

	return utils.AddFileToZip(zipData, "metadata.toml", metaBytes)
}
