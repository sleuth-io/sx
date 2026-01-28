package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
	case asset.TypeAgent, asset.TypeCommand, asset.TypeSkill:
		return createZipFromPromptFile(filePath, *detectedType, content)
	default:
		return nil, fmt.Errorf("unsupported asset type: %s", detectedType.Label)
	}
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
