package dirasset

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/metadata"
	"github.com/sleuth-io/sx/internal/utils"
)

// Operations provides common operations for directory-based asset installation.
// This handles assets that are extracted into their own directory with a metadata.toml file,
// such as skills, commands, and agents.
type Operations struct {
	// subdir is the subdirectory under targetBase where assets are stored (e.g., "skills", "commands")
	subdir string
	// expectedType is the asset type expected when scanning (optional, nil means accept any)
	expectedType *asset.Type
}

// NewOperations creates a new Operations instance for a specific asset subdirectory
func NewOperations(subdir string, expectedType *asset.Type) *Operations {
	return &Operations{
		subdir:       subdir,
		expectedType: expectedType,
	}
}

// Install validates and extracts an asset zip to {targetBase}/{subdir}/{name}/
func (o *Operations) Install(ctx context.Context, zipData []byte, targetBase string, assetName string) error {
	// Validate zip contents before extracting
	if err := metadata.ValidateZip(zipData, o.expectedType); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	assetDir := filepath.Join(targetBase, o.subdir, assetName)

	// Remove existing installation if present
	if utils.IsDirectory(assetDir) {
		if err := os.RemoveAll(assetDir); err != nil {
			return fmt.Errorf("failed to remove existing installation: %w", err)
		}
	}

	// Create installation directory
	if err := utils.EnsureDir(assetDir); err != nil {
		return fmt.Errorf("failed to create installation directory: %w", err)
	}

	// Extract entire zip to asset directory
	if err := utils.ExtractZip(zipData, assetDir); err != nil {
		return fmt.Errorf("failed to extract asset: %w", err)
	}

	return nil
}

// Remove removes an asset from {targetBase}/{subdir}/{name}/
func (o *Operations) Remove(ctx context.Context, targetBase string, assetName string) error {
	assetDir := filepath.Join(targetBase, o.subdir, assetName)

	if !utils.IsDirectory(assetDir) {
		// Already removed or never installed
		return nil
	}

	if err := os.RemoveAll(assetDir); err != nil {
		return fmt.Errorf("failed to remove asset: %w", err)
	}

	return nil
}

// ScanInstalled scans for installed assets in {targetBase}/{subdir}/
func (o *Operations) ScanInstalled(targetBase string) ([]InstalledAssetInfo, error) {
	var assets []InstalledAssetInfo

	assetsPath := filepath.Join(targetBase, o.subdir)
	if !utils.IsDirectory(assetsPath) {
		return assets, nil
	}

	dirs, err := os.ReadDir(assetsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s directory: %w", o.subdir, err)
	}

	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}

		metaPath := filepath.Join(assetsPath, dir.Name(), "metadata.toml")
		meta, err := metadata.ParseFile(metaPath)
		if err != nil {
			continue // Skip if can't parse
		}

		// Filter by expected type if specified
		if o.expectedType != nil && meta.Asset.Type != *o.expectedType {
			continue
		}

		assets = append(assets, InstalledAssetInfo{
			Name:        meta.Asset.Name,
			Description: meta.Asset.Description,
			Version:     meta.Asset.Version,
			Type:        meta.Asset.Type,
			InstallPath: filepath.Join(o.subdir, dir.Name()),
		})
	}

	return assets, nil
}

// GetAssetDir returns the full path to an asset's directory
func (o *Operations) GetAssetDir(targetBase string, assetName string) string {
	return filepath.Join(targetBase, o.subdir, assetName)
}

// VerifyInstalled checks if an asset is installed with the expected version
func (o *Operations) VerifyInstalled(targetBase string, assetName string, expectedVersion string) (bool, string) {
	assetDir := o.GetAssetDir(targetBase, assetName)

	if !utils.IsDirectory(assetDir) {
		return false, "directory not found"
	}

	metaPath := filepath.Join(assetDir, "metadata.toml")
	if !utils.FileExists(metaPath) {
		return false, "metadata.toml not found"
	}

	meta, err := metadata.ParseFile(metaPath)
	if err != nil {
		return false, "failed to parse metadata: " + err.Error()
	}

	if meta.Asset.Version != expectedVersion {
		return false, fmt.Sprintf("version mismatch: installed %s, expected %s", meta.Asset.Version, expectedVersion)
	}

	return true, "installed"
}

// PromptFileGetter is a function that extracts the prompt file path from metadata.
// Returns empty string if not specified, in which case the default will be used.
type PromptFileGetter func(meta *metadata.Metadata) string

// PromptContent contains the result of reading an asset's prompt file
type PromptContent struct {
	Content     string // The prompt file contents
	BaseDir     string // Directory where the asset is installed
	Description string // Asset description from metadata
	Version     string // Asset version from metadata
}

// ReadPromptContent reads the prompt/content file for an asset.
// promptFileGetter extracts the prompt file from metadata; if nil or returns empty, defaultPromptFile is used.
func (o *Operations) ReadPromptContent(targetBase string, assetName string, defaultPromptFile string, promptFileGetter PromptFileGetter) (*PromptContent, error) {
	assetDir := o.GetAssetDir(targetBase, assetName)

	// Check if asset directory exists
	if _, err := os.Stat(assetDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("asset not found: %s", assetName)
	}

	// Read metadata
	metaPath := filepath.Join(assetDir, "metadata.toml")
	meta, err := metadata.ParseFile(metaPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read asset metadata: %w", err)
	}

	// Determine prompt file
	promptFile := defaultPromptFile
	if promptFileGetter != nil {
		if pf := promptFileGetter(meta); pf != "" {
			promptFile = pf
		}
	}

	// Read the prompt file content
	promptPath := filepath.Join(assetDir, promptFile)
	contentBytes, err := os.ReadFile(promptPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read prompt content: %w", err)
	}

	return &PromptContent{
		Content:     string(contentBytes),
		BaseDir:     assetDir,
		Description: meta.Asset.Description,
		Version:     meta.Asset.Version,
	}, nil
}
