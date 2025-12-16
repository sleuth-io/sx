package dirasset

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sleuth-io/skills/internal/asset"
	"github.com/sleuth-io/skills/internal/metadata"
	"github.com/sleuth-io/skills/internal/utils"
)

// Operations provides common operations for directory-based artifact installation.
// This handles artifacts that are extracted into their own directory with a metadata.toml file,
// such as skills, commands, and agents.
type Operations struct {
	// subdir is the subdirectory under targetBase where artifacts are stored (e.g., "skills", "commands")
	subdir string
	// expectedType is the artifact type expected when scanning (optional, nil means accept any)
	expectedType *asset.Type
}

// NewOperations creates a new Operations instance for a specific artifact subdirectory
func NewOperations(subdir string, expectedType *asset.Type) *Operations {
	return &Operations{
		subdir:       subdir,
		expectedType: expectedType,
	}
}

// Install extracts an artifact zip to {targetBase}/{subdir}/{name}/
func (o *Operations) Install(ctx context.Context, zipData []byte, targetBase string, artifactName string) error {
	artifactDir := filepath.Join(targetBase, o.subdir, artifactName)

	// Remove existing installation if present
	if utils.IsDirectory(artifactDir) {
		if err := os.RemoveAll(artifactDir); err != nil {
			return fmt.Errorf("failed to remove existing installation: %w", err)
		}
	}

	// Create installation directory
	if err := utils.EnsureDir(artifactDir); err != nil {
		return fmt.Errorf("failed to create installation directory: %w", err)
	}

	// Extract entire zip to artifact directory
	if err := utils.ExtractZip(zipData, artifactDir); err != nil {
		return fmt.Errorf("failed to extract artifact: %w", err)
	}

	return nil
}

// Remove removes an artifact from {targetBase}/{subdir}/{name}/
func (o *Operations) Remove(ctx context.Context, targetBase string, artifactName string) error {
	artifactDir := filepath.Join(targetBase, o.subdir, artifactName)

	if !utils.IsDirectory(artifactDir) {
		// Already removed or never installed
		return nil
	}

	if err := os.RemoveAll(artifactDir); err != nil {
		return fmt.Errorf("failed to remove artifact: %w", err)
	}

	return nil
}

// ScanInstalled scans for installed artifacts in {targetBase}/{subdir}/
func (o *Operations) ScanInstalled(targetBase string) ([]InstalledArtifactInfo, error) {
	var artifacts []InstalledArtifactInfo

	artifactsPath := filepath.Join(targetBase, o.subdir)
	if !utils.IsDirectory(artifactsPath) {
		return artifacts, nil
	}

	dirs, err := os.ReadDir(artifactsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s directory: %w", o.subdir, err)
	}

	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}

		metaPath := filepath.Join(artifactsPath, dir.Name(), "metadata.toml")
		meta, err := metadata.ParseFile(metaPath)
		if err != nil {
			continue // Skip if can't parse
		}

		// Filter by expected type if specified
		if o.expectedType != nil && meta.Artifact.Type != *o.expectedType {
			continue
		}

		artifacts = append(artifacts, InstalledArtifactInfo{
			Name:        meta.Artifact.Name,
			Description: meta.Artifact.Description,
			Version:     meta.Artifact.Version,
			Type:        meta.Artifact.Type,
			InstallPath: filepath.Join(o.subdir, dir.Name()),
		})
	}

	return artifacts, nil
}

// GetArtifactDir returns the full path to an artifact's directory
func (o *Operations) GetArtifactDir(targetBase string, artifactName string) string {
	return filepath.Join(targetBase, o.subdir, artifactName)
}

// VerifyInstalled checks if an artifact is installed with the expected version
func (o *Operations) VerifyInstalled(targetBase string, artifactName string, expectedVersion string) (bool, string) {
	artifactDir := o.GetArtifactDir(targetBase, artifactName)

	if !utils.IsDirectory(artifactDir) {
		return false, "directory not found"
	}

	metaPath := filepath.Join(artifactDir, "metadata.toml")
	if !utils.FileExists(metaPath) {
		return false, "metadata.toml not found"
	}

	meta, err := metadata.ParseFile(metaPath)
	if err != nil {
		return false, "failed to parse metadata: " + err.Error()
	}

	if meta.Artifact.Version != expectedVersion {
		return false, fmt.Sprintf("version mismatch: installed %s, expected %s", meta.Artifact.Version, expectedVersion)
	}

	return true, "installed"
}

// PromptFileGetter is a function that extracts the prompt file path from metadata.
// Returns empty string if not specified, in which case the default will be used.
type PromptFileGetter func(meta *metadata.Metadata) string

// PromptContent contains the result of reading an artifact's prompt file
type PromptContent struct {
	Content     string // The prompt file contents
	BaseDir     string // Directory where the artifact is installed
	Description string // Artifact description from metadata
	Version     string // Artifact version from metadata
}

// ReadPromptContent reads the prompt/content file for an artifact.
// promptFileGetter extracts the prompt file from metadata; if nil or returns empty, defaultPromptFile is used.
func (o *Operations) ReadPromptContent(targetBase string, artifactName string, defaultPromptFile string, promptFileGetter PromptFileGetter) (*PromptContent, error) {
	artifactDir := o.GetArtifactDir(targetBase, artifactName)

	// Check if artifact directory exists
	if _, err := os.Stat(artifactDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("artifact not found: %s", artifactName)
	}

	// Read metadata
	metaPath := filepath.Join(artifactDir, "metadata.toml")
	meta, err := metadata.ParseFile(metaPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read artifact metadata: %w", err)
	}

	// Determine prompt file
	promptFile := defaultPromptFile
	if promptFileGetter != nil {
		if pf := promptFileGetter(meta); pf != "" {
			promptFile = pf
		}
	}

	// Read the prompt file content
	promptPath := filepath.Join(artifactDir, promptFile)
	contentBytes, err := os.ReadFile(promptPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read prompt content: %w", err)
	}

	return &PromptContent{
		Content:     string(contentBytes),
		BaseDir:     artifactDir,
		Description: meta.Artifact.Description,
		Version:     meta.Artifact.Version,
	}, nil
}
