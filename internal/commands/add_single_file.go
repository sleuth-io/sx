package commands

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/metadata"
	"github.com/sleuth-io/sx/internal/utils"
)

// isSingleFileAsset checks if the path is a single .md file that can be treated as an asset
// This includes agents, commands, and rule files
func isSingleFileAsset(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, ".md")
}

// createZipFromSingleFile creates a zip archive from a single .md file
// Detects asset type from path and content, creates appropriate metadata
func createZipFromSingleFile(filePath string) ([]byte, error) {
	// Check if this is a rule file (e.g., .claude/rules/my-rule.md)
	if isRuleFile(filePath) {
		return createZipFromRuleFile(filePath)
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	// Determine asset type from path and content
	assetType := detectSingleFileAssetType(filePath, content)

	// Keep the original filename instead of renaming to AGENT.md/COMMAND.md
	promptFileName := filepath.Base(filePath)
	assetName := strings.TrimSuffix(promptFileName, filepath.Ext(promptFileName))

	// Create zip with the original filename
	zipData, err := utils.CreateZipFromContent(promptFileName, content)
	if err != nil {
		return nil, err
	}

	// Create metadata with correct PromptFile
	meta := &metadata.Metadata{
		MetadataVersion: "1.0",
		Asset: metadata.Asset{
			Name:    assetName,
			Type:    assetType,
			Version: "1.0", // Default version, user will confirm
		},
	}

	if assetType == asset.TypeAgent {
		meta.Agent = &metadata.AgentConfig{PromptFile: promptFileName}
	} else {
		meta.Command = &metadata.CommandConfig{PromptFile: promptFileName}
	}

	// Add metadata.toml to zip
	metaBytes, err := metadata.Marshal(meta)
	if err != nil {
		return nil, err
	}

	return utils.AddFileToZip(zipData, "metadata.toml", metaBytes)
}

// detectSingleFileAssetType analyzes path and content to determine if it's an agent or command
func detectSingleFileAssetType(filePath string, content []byte) asset.Type {
	lowerPath := strings.ToLower(filePath)

	// Check path for hints - most reliable indicator
	if strings.Contains(lowerPath, "/agents/") || strings.Contains(lowerPath, "\\agents\\") {
		return asset.TypeAgent
	}
	if strings.Contains(lowerPath, "/commands/") || strings.Contains(lowerPath, "\\commands\\") {
		return asset.TypeCommand
	}

	// Check for YAML frontmatter (agents typically have this)
	contentStr := string(content)
	if strings.HasPrefix(contentStr, "---") {
		lines := strings.Split(contentStr, "\n")
		inFrontmatter := false
		for _, line := range lines {
			if line == "---" {
				if inFrontmatter {
					break
				}
				inFrontmatter = true
				continue
			}
			if inFrontmatter {
				lower := strings.ToLower(line)
				// Agent frontmatter typically has: tools, model, permissionMode
				if strings.HasPrefix(lower, "tools:") ||
					strings.HasPrefix(lower, "model:") ||
					strings.HasPrefix(lower, "permissionmode:") {
					return asset.TypeAgent
				}
			}
		}
	}

	// Default to command if no agent indicators found
	return asset.TypeCommand
}
