package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ExpandTilde expands a tilde (~) at the beginning of a path to the user's home directory
func ExpandTilde(path string) (string, error) {
	if !strings.HasPrefix(path, "~") {
		return path, nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home directory: %w", err)
	}

	if path == "~" {
		return homeDir, nil
	}

	if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, "~\\") {
		return filepath.Join(homeDir, path[2:]), nil
	}

	return path, nil
}

// NormalizePath normalizes a file path, expanding tilde and cleaning it
func NormalizePath(path string) (string, error) {
	expanded, err := ExpandTilde(path)
	if err != nil {
		return "", err
	}
	return filepath.Clean(expanded), nil
}

// EnsureDir ensures that a directory exists, creating it if necessary
func EnsureDir(path string) error {
	return os.MkdirAll(path, 0755)
}

// GetClaudeDir returns the path to the .claude directory
func GetClaudeDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home directory: %w", err)
	}
	return filepath.Join(homeDir, ".claude"), nil
}

// GetConfigDir returns the path to the skills config directory
func GetConfigDir() (string, error) {
	claudeDir, err := GetClaudeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(claudeDir, "plugins", "skills"), nil
}

// GetConfigFile returns the path to the config.json file
func GetConfigFile() (string, error) {
	configDir, err := GetConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "config.json"), nil
}

// FileExists checks if a file exists
func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// IsDirectory checks if a path is a directory
func IsDirectory(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}
