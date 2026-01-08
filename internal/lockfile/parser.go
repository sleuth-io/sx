package lockfile

import (
	"bytes"
	"fmt"
	"os"

	"github.com/BurntSushi/toml"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/buildinfo"
)

// lockFileCompat is used for parsing old-style lock files with "artifacts" and "repositories" fields
type lockFileCompat struct {
	LockVersion string        `toml:"lock-version"`
	Version     string        `toml:"version"`
	CreatedBy   string        `toml:"created-by"`
	Artifacts   []assetCompat `toml:"artifacts"` // Old name for backwards compatibility
}

// assetCompat is used for parsing old-style assets with "repositories" field
type assetCompat struct {
	Name         string       `toml:"name"`
	Version      string       `toml:"version"`
	Type         asset.Type   `toml:"type"`
	Clients      []string     `toml:"clients,omitempty"`
	Dependencies []Dependency `toml:"dependencies,omitempty"`
	SourceHTTP   *SourceHTTP  `toml:"source-http,omitempty"`
	SourcePath   *SourcePath  `toml:"source-path,omitempty"`
	SourceGit    *SourceGit   `toml:"source-git,omitempty"`
	Repositories []Scope      `toml:"repositories,omitempty"` // Old name for backwards compatibility
}

// Parse parses a lock file from bytes
// Supports both new "assets"/"scopes" and old "artifacts"/"repositories" field names
func Parse(data []byte) (*LockFile, error) {
	var lockFile LockFile

	if err := toml.Unmarshal(data, &lockFile); err != nil {
		return nil, fmt.Errorf("failed to parse lock file: %w", err)
	}

	// Check if we got data from new [assets] section
	if len(lockFile.Assets) == 0 {
		// Try parsing with old [[artifacts]] section name
		var compat lockFileCompat
		if err := toml.Unmarshal(data, &compat); err == nil && len(compat.Artifacts) > 0 {
			lockFile.LockVersion = compat.LockVersion
			lockFile.Version = compat.Version
			lockFile.CreatedBy = compat.CreatedBy
			lockFile.Assets = make([]Asset, len(compat.Artifacts))
			for i, art := range compat.Artifacts {
				lockFile.Assets[i] = Asset{
					Name:         art.Name,
					Version:      art.Version,
					Type:         art.Type,
					Clients:      art.Clients,
					Dependencies: art.Dependencies,
					SourceHTTP:   art.SourceHTTP,
					SourcePath:   art.SourcePath,
					SourceGit:    art.SourceGit,
					Scopes:       art.Repositories,
				}
			}
		}
	}

	return &lockFile, nil
}

// ParseFile parses a lock file from a file path
func ParseFile(filePath string) (*LockFile, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read lock file: %w", err)
	}

	return Parse(data)
}

// Marshal converts a lock file to TOML bytes
func Marshal(lockFile *LockFile) ([]byte, error) {
	// Use bytes.Buffer to marshal to
	buf := new(bytes.Buffer)
	encoder := toml.NewEncoder(buf)

	if err := encoder.Encode(lockFile); err != nil {
		return nil, fmt.Errorf("failed to marshal lock file: %w", err)
	}

	return buf.Bytes(), nil
}

// Write writes a lock file to a file path
func Write(lockFile *LockFile, filePath string) error {
	data, err := Marshal(lockFile)
	if err != nil {
		return err
	}

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write lock file: %w", err)
	}

	return nil
}

// FindAsset finds an asset by name in a lock file
// Returns the asset and true if found, nil and false otherwise
func FindAsset(lockFilePath, name string) (*Asset, bool) {
	lockFile, err := ParseFile(lockFilePath)
	if err != nil {
		// Lock file doesn't exist or can't be read
		return nil, false
	}

	for _, ast := range lockFile.Assets {
		if ast.Name == name {
			return &ast, true
		}
	}

	return nil, false
}

// AddOrUpdateAsset adds or updates an asset in the lock file
// Replaces any existing asset with the same name@version
func AddOrUpdateAsset(lockFilePath string, ast *Asset) error {
	// Load existing lock file or create new one
	var lockFile *LockFile
	if _, err := os.Stat(lockFilePath); err == nil {
		lockFile, err = ParseFile(lockFilePath)
		if err != nil {
			return fmt.Errorf("failed to parse lock file: %w", err)
		}
	} else {
		lockFile = &LockFile{
			LockVersion: "1.0",
			Version:     "1",
			CreatedBy:   buildinfo.GetCreatedBy(),
			Assets:      []Asset{},
		}
	}

	// Remove existing asset with same name@version
	var filteredAssets []Asset
	for _, existing := range lockFile.Assets {
		if existing.Name != ast.Name || existing.Version != ast.Version {
			filteredAssets = append(filteredAssets, existing)
		}
	}
	lockFile.Assets = filteredAssets

	// Add the asset
	lockFile.Assets = append(lockFile.Assets, *ast)

	// Write lock file
	return Write(lockFile, lockFilePath)
}

// RemoveAsset removes an asset and all its installations from a lock file
func RemoveAsset(lockFilePath string, name, version string) error {
	lockFile, err := ParseFile(lockFilePath)
	if err != nil {
		return fmt.Errorf("failed to parse lock file: %w", err)
	}

	// Filter out the asset
	var newAssets []Asset
	for _, ast := range lockFile.Assets {
		if ast.Name != name || ast.Version != version {
			newAssets = append(newAssets, ast)
		}
	}

	lockFile.Assets = newAssets

	return Write(lockFile, lockFilePath)
}
