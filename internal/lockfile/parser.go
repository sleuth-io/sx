package lockfile

import (
	"bytes"
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
	"github.com/sleuth-io/skills/internal/buildinfo"
)

// Parse parses a lock file from bytes
func Parse(data []byte) (*LockFile, error) {
	var lockFile LockFile

	if err := toml.Unmarshal(data, &lockFile); err != nil {
		return nil, fmt.Errorf("failed to parse lock file: %w", err)
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

// FindArtifact finds an artifact by name in a lock file
// Returns the artifact and true if found, nil and false otherwise
func FindArtifact(lockFilePath, name string) (*Artifact, bool) {
	lockFile, err := ParseFile(lockFilePath)
	if err != nil {
		// Lock file doesn't exist or can't be read
		return nil, false
	}

	for _, artifact := range lockFile.Artifacts {
		if artifact.Name == name {
			return &artifact, true
		}
	}

	return nil, false
}

// AddOrUpdateArtifact adds or updates an artifact in the lock file
// Replaces any existing artifact with the same name@version
func AddOrUpdateArtifact(lockFilePath string, artifact *Artifact) error {
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
			Artifacts:   []Artifact{},
		}
	}

	// Remove existing artifact with same name@version
	var filteredArtifacts []Artifact
	for _, existing := range lockFile.Artifacts {
		if existing.Name != artifact.Name || existing.Version != artifact.Version {
			filteredArtifacts = append(filteredArtifacts, existing)
		}
	}
	lockFile.Artifacts = filteredArtifacts

	// Add the artifact
	lockFile.Artifacts = append(lockFile.Artifacts, *artifact)

	// Write lock file
	return Write(lockFile, lockFilePath)
}

// RemoveArtifact removes an artifact and all its installations from a lock file
func RemoveArtifact(lockFilePath string, name, version string) error {
	lockFile, err := ParseFile(lockFilePath)
	if err != nil {
		return fmt.Errorf("failed to parse lock file: %w", err)
	}

	// Filter out the artifact
	var newArtifacts []Artifact
	for _, artifact := range lockFile.Artifacts {
		if artifact.Name != name || artifact.Version != version {
			newArtifacts = append(newArtifacts, artifact)
		}
	}

	lockFile.Artifacts = newArtifacts

	return Write(lockFile, lockFilePath)
}
