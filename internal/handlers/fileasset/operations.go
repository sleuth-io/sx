package fileasset

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/metadata"
	"github.com/sleuth-io/sx/internal/utils"
)

// Operations provides common operations for single-file asset installation.
// This handles assets that are installed as a single .md file with an adjacent metadata file,
// such as commands and agents.
type Operations struct {
	// subdir is the subdirectory under targetBase where assets are stored (e.g., "commands", "agents")
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

// Install extracts a prompt file from zip and installs it as {targetBase}/{subdir}/{name}.md
// Also writes a companion metadata file as {name}-metadata.toml
func (o *Operations) Install(ctx context.Context, zipData []byte, targetBase string, assetName string, promptFile string) error {
	// Read the prompt file from zip
	promptData, err := utils.ReadZipFile(zipData, promptFile)
	if err != nil {
		return fmt.Errorf("failed to read prompt file from zip: %w", err)
	}

	// Determine installation path
	installPath := o.GetAssetPath(targetBase, assetName)

	// Ensure parent directory exists
	if err := utils.EnsureDir(filepath.Dir(installPath)); err != nil {
		return fmt.Errorf("failed to create %s directory: %w", o.subdir, err)
	}

	// Write the asset file
	if err := os.WriteFile(installPath, promptData, 0644); err != nil {
		return fmt.Errorf("failed to write asset file: %w", err)
	}

	// Write metadata file for version tracking
	if err := o.writeMetadataFile(zipData, installPath); err != nil {
		return err
	}

	return nil
}

// Remove removes an asset file and its companion metadata from {targetBase}/{subdir}/{name}.md
func (o *Operations) Remove(ctx context.Context, targetBase string, assetName string) error {
	installPath := o.GetAssetPath(targetBase, assetName)

	if !utils.FileExists(installPath) {
		// Already removed or never installed
		return nil
	}

	if err := os.Remove(installPath); err != nil {
		return fmt.Errorf("failed to remove asset: %w", err)
	}

	// Remove metadata file if it exists
	o.removeMetadataFile(installPath)

	return nil
}

// ScanInstalled scans for installed single-file assets in {targetBase}/{subdir}/
func (o *Operations) ScanInstalled(targetBase string) ([]InstalledAssetInfo, error) {
	var assets []InstalledAssetInfo

	assetsPath := filepath.Join(targetBase, o.subdir)
	if !utils.IsDirectory(assetsPath) {
		return assets, nil
	}

	entries, err := os.ReadDir(assetsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s directory: %w", o.subdir, err)
	}

	for _, entry := range entries {
		// Skip directories
		if entry.IsDir() {
			continue
		}

		name := entry.Name()

		// Only consider .md files
		if !strings.HasSuffix(strings.ToLower(name), ".md") {
			continue
		}

		// Skip metadata files
		if strings.HasSuffix(name, "-metadata.toml") {
			continue
		}

		// Get asset name (strip .md extension)
		assetName := strings.TrimSuffix(name, filepath.Ext(name))

		// Try to read companion metadata file
		metaPath := o.getMetadataPath(filepath.Join(assetsPath, name))
		meta, err := metadata.ParseFile(metaPath)
		if err != nil {
			// No valid metadata, skip this asset
			continue
		}

		// Filter by expected type if specified
		if o.expectedType != nil && meta.Asset.Type != *o.expectedType {
			continue
		}

		assets = append(assets, InstalledAssetInfo{
			Name:        assetName,
			Description: meta.Asset.Description,
			Version:     meta.Asset.Version,
			Type:        meta.Asset.Type,
			InstallPath: filepath.Join(o.subdir, name),
		})
	}

	return assets, nil
}

// GetAssetPath returns the full path to an asset's .md file
func (o *Operations) GetAssetPath(targetBase string, assetName string) string {
	return filepath.Join(targetBase, o.subdir, assetName+".md")
}

// GetInstallPath returns the relative installation path for an asset
func (o *Operations) GetInstallPath(assetName string) string {
	return filepath.Join(o.subdir, assetName+".md")
}

// VerifyInstalled checks if an asset is installed with the expected version
func (o *Operations) VerifyInstalled(targetBase string, assetName string, expectedVersion string) (bool, string) {
	assetPath := o.GetAssetPath(targetBase, assetName)

	if !utils.FileExists(assetPath) {
		return false, "file not found"
	}

	// Check metadata file for version verification
	metaPath := o.getMetadataPath(assetPath)
	if !utils.FileExists(metaPath) {
		// No metadata file - can only verify file exists
		return true, "installed (no version info)"
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

// ReadPromptContent reads the content of an installed asset
func (o *Operations) ReadPromptContent(targetBase string, assetName string) (*PromptContent, error) {
	assetPath := o.GetAssetPath(targetBase, assetName)

	if !utils.FileExists(assetPath) {
		return nil, fmt.Errorf("asset not found: %s", assetName)
	}

	// Read the asset content
	contentBytes, err := os.ReadFile(assetPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read asset content: %w", err)
	}

	result := &PromptContent{
		Content:  string(contentBytes),
		FilePath: assetPath,
	}

	// Try to read metadata for description and version
	metaPath := o.getMetadataPath(assetPath)
	if meta, err := metadata.ParseFile(metaPath); err == nil {
		result.Description = meta.Asset.Description
		result.Version = meta.Asset.Version
	}

	return result, nil
}

// writeMetadataFile writes the metadata file alongside the asset for version tracking
func (o *Operations) writeMetadataFile(zipData []byte, installPath string) error {
	metadataBytes, err := utils.ReadZipFile(zipData, "metadata.toml")
	if err != nil {
		// metadata.toml doesn't exist in zip, that's okay (backwards compatibility)
		return nil
	}

	metaPath := o.getMetadataPath(installPath)
	if err := os.WriteFile(metaPath, metadataBytes, 0644); err != nil {
		return fmt.Errorf("failed to write metadata file: %w", err)
	}

	return nil
}

// removeMetadataFile removes the metadata file if it exists
func (o *Operations) removeMetadataFile(installPath string) {
	metaPath := o.getMetadataPath(installPath)
	if utils.FileExists(metaPath) {
		os.Remove(metaPath) // Ignore errors, metadata is optional
	}
}

// getMetadataPath returns the path to the companion metadata file
func (o *Operations) getMetadataPath(installPath string) string {
	return strings.TrimSuffix(installPath, ".md") + "-metadata.toml"
}
