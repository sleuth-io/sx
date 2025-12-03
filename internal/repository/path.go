package repository

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sleuth-io/skills/internal/lockfile"
	"github.com/sleuth-io/skills/internal/utils"
)

// PathSourceHandler handles artifacts with source-path
type PathSourceHandler struct {
	lockFileDir string // Directory containing the lock file for relative path resolution
}

// NewPathSourceHandler creates a new path source handler
func NewPathSourceHandler(lockFileDir string) *PathSourceHandler {
	return &PathSourceHandler{
		lockFileDir: lockFileDir,
	}
}

// Fetch reads an artifact from a local file path
func (p *PathSourceHandler) Fetch(ctx context.Context, artifact *lockfile.Artifact) ([]byte, error) {
	if artifact.SourcePath == nil {
		return nil, fmt.Errorf("artifact does not have source-path")
	}

	source := artifact.SourcePath
	path := source.Path

	// Resolve path based on type
	var resolvedPath string
	var err error

	if filepath.IsAbs(path) {
		// Absolute path - use as-is
		resolvedPath = path
	} else if len(path) > 0 && path[0] == '~' {
		// Tilde path - expand to home directory
		resolvedPath, err = utils.ExpandTilde(path)
		if err != nil {
			return nil, fmt.Errorf("failed to expand tilde in path: %w", err)
		}
	} else {
		// Relative path - resolve from lock file directory
		if p.lockFileDir == "" {
			return nil, fmt.Errorf("relative paths require lock file directory to be set")
		}
		resolvedPath = filepath.Join(p.lockFileDir, path)
	}

	// Clean the path
	resolvedPath = filepath.Clean(resolvedPath)

	// Check if path exists
	info, err := os.Stat(resolvedPath)
	if err != nil {
		return nil, fmt.Errorf("path not found: %s", resolvedPath)
	}

	// If it's a directory, create a zip from it
	if info.IsDir() {
		data, err := utils.CreateZip(resolvedPath)
		if err != nil {
			return nil, fmt.Errorf("failed to create zip from directory: %w", err)
		}
		return data, nil
	}

	// It's a file - read it
	data, err := os.ReadFile(resolvedPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	// Verify it's a valid zip file
	if !utils.IsZipFile(data) {
		return nil, fmt.Errorf("file is not a valid zip archive: %s", resolvedPath)
	}

	return data, nil
}

// ResolvePath resolves a path (absolute, relative, or tilde) to an absolute path
func (p *PathSourceHandler) ResolvePath(path string) (string, error) {
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}

	if len(path) > 0 && path[0] == '~' {
		return utils.ExpandTilde(path)
	}

	if p.lockFileDir == "" {
		return "", fmt.Errorf("relative paths require lock file directory to be set")
	}

	return filepath.Join(p.lockFileDir, path), nil
}
