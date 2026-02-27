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

// GetClaudeDir returns the path to the .claude directory for asset installation
// This is where global assets are installed
func GetClaudeDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home directory: %w", err)
	}
	return filepath.Join(homeDir, ".claude"), nil
}

// GetConfigDir returns the path to the sx config directory
// Uses platform-specific config directories:
// - Linux: ~/.config/sx (or $XDG_CONFIG_HOME/sx)
// - macOS: ~/Library/Application Support/sx
// - Windows: %AppData%/sx
func GetConfigDir() (string, error) {
	// Check for environment override (support both new and legacy)
	if configDir := os.Getenv("SX_CONFIG_DIR"); configDir != "" {
		return configDir, nil
	}
	if configDir := os.Getenv("SKILLS_CONFIG_DIR"); configDir != "" {
		return configDir, nil
	}

	// Use os.UserConfigDir() for platform-specific config directory
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("failed to determine config directory: %w", err)
	}

	return filepath.Join(configDir, "sx"), nil
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

// ResolveCommand resolves a command for a packaged MCP server.
// Bare command names (e.g. "node", "uv", "python") are left as-is to be resolved via PATH.
// Relative paths containing a directory separator (e.g. "./bin/server") are made absolute
// relative to installPath.
func ResolveCommand(command string, installPath string) string {
	if filepath.IsAbs(command) {
		return command
	}
	// Bare command name (no directory separator) - resolve via PATH
	if filepath.Base(command) == command {
		return command
	}
	// Relative path with directory component - make absolute
	return filepath.Join(installPath, command)
}

// ResolveArgs resolves args for a packaged MCP server.
// Each arg is converted to an absolute path only if it corresponds to an actual file
// or directory within installPath. Subcommands (e.g. "run" for "uv run"), flags,
// and other non-file args are left as-is.
func ResolveArgs(args []string, installPath string) []string {
	resolved := make([]string, len(args))
	for i, arg := range args {
		if filepath.IsAbs(arg) || strings.HasPrefix(arg, "-") {
			resolved[i] = arg
		} else {
			candidate := filepath.Join(installPath, arg)
			if _, err := os.Stat(candidate); err == nil {
				resolved[i] = candidate
			} else {
				resolved[i] = arg
			}
		}
	}
	return resolved
}

// ResolveCommandAndArgs resolves a command and its args for a packaged MCP server,
// returning them as []any suitable for JSON serialization into client configs.
func ResolveCommandAndArgs(command string, args []string, installPath string) (string, []any) {
	resolvedCmd := ResolveCommand(command, installPath)
	resolvedArgs := ResolveArgs(args, installPath)
	anyArgs := make([]any, len(resolvedArgs))
	for i, arg := range resolvedArgs {
		anyArgs[i] = arg
	}
	return resolvedCmd, anyArgs
}

// StringsToAny converts a []string to []any.
func StringsToAny(s []string) []any {
	result := make([]any, len(s))
	for i, v := range s {
		result[i] = v
	}
	return result
}
