package handlers

import (
	"os"
	"path/filepath"
	"strings"
)

// IsVSCodeGeminiInstalled checks if the Gemini Code Assist VS Code extension is installed
func IsVSCodeGeminiInstalled() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}

	extensionsDir := filepath.Join(home, VSCodeExtensionsDir)
	entries, err := os.ReadDir(extensionsDir)
	if err != nil {
		return false
	}

	// Check for extension folder (case-insensitive, may include version suffix)
	// e.g., "google.geminicodeassist-1.0.0" or "Google.geminicodeassist-1.0.0"
	for _, entry := range entries {
		if entry.IsDir() {
			name := strings.ToLower(entry.Name())
			if strings.HasPrefix(name, VSCodeGeminiExtensionPrefix) {
				return true
			}
		}
	}

	return false
}
