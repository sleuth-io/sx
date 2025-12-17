package fileasset

import (
	"github.com/sleuth-io/sx/internal/asset"
)

// InstalledAssetInfo represents information about an installed single-file asset
type InstalledAssetInfo struct {
	Name        string
	Description string
	Version     string
	Type        asset.Type
	InstallPath string // Path to the .md file
}

// PromptContent contains the result of reading an asset's prompt file
type PromptContent struct {
	Content     string // The prompt file contents
	FilePath    string // Full path to the installed .md file
	Description string // Asset description from metadata
	Version     string // Asset version from metadata
}
