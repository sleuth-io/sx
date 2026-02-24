package handlers

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsVSCodeGeminiInstalled(t *testing.T) {
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)

	tempDir := t.TempDir()
	os.Setenv("HOME", tempDir)

	// Initially not installed
	if IsVSCodeGeminiInstalled() {
		t.Error("IsVSCodeGeminiInstalled() = true when no extension exists")
	}

	// Create VS Code extensions directory without Gemini
	extensionsDir := filepath.Join(tempDir, ".vscode/extensions")
	if err := os.MkdirAll(extensionsDir, 0755); err != nil {
		t.Fatalf("Failed to create extensions dir: %v", err)
	}

	// Still not installed
	if IsVSCodeGeminiInstalled() {
		t.Error("IsVSCodeGeminiInstalled() = true when extensions dir is empty")
	}

	// Create non-Gemini extension
	otherExt := filepath.Join(extensionsDir, "ms-python.python-1.0.0")
	if err := os.MkdirAll(otherExt, 0755); err != nil {
		t.Fatalf("Failed to create other extension dir: %v", err)
	}

	// Still not installed
	if IsVSCodeGeminiInstalled() {
		t.Error("IsVSCodeGeminiInstalled() = true when only other extensions exist")
	}

	// Create Gemini extension (lowercase, with version)
	geminiExt := filepath.Join(extensionsDir, "google.geminicodeassist-1.2.3")
	if err := os.MkdirAll(geminiExt, 0755); err != nil {
		t.Fatalf("Failed to create Gemini extension dir: %v", err)
	}

	// Now should be installed
	if !IsVSCodeGeminiInstalled() {
		t.Error("IsVSCodeGeminiInstalled() = false when Gemini extension exists")
	}
}

func TestIsVSCodeGeminiInstalled_CaseInsensitive(t *testing.T) {
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)

	tempDir := t.TempDir()
	os.Setenv("HOME", tempDir)

	// Create VS Code extension with different casing
	extensionsDir := filepath.Join(tempDir, ".vscode/extensions")
	geminiExt := filepath.Join(extensionsDir, "Google.geminicodeassist-1.0.0")
	if err := os.MkdirAll(geminiExt, 0755); err != nil {
		t.Fatalf("Failed to create Gemini extension dir: %v", err)
	}

	// Should be detected (case-insensitive)
	if !IsVSCodeGeminiInstalled() {
		t.Error("IsVSCodeGeminiInstalled() should be case-insensitive")
	}
}
