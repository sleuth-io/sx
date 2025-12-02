package lockfile

import (
	"bytes"
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
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
