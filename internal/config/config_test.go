package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sleuth-io/skills/internal/utils"
)

func TestLoadFallbackToLegacy(t *testing.T) {
	// Create a temporary home directory for testing
	tmpHome := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", originalHome)

	// Create legacy config file
	legacyDir := filepath.Join(tmpHome, ".claude", "plugins", "skills")
	if err := os.MkdirAll(legacyDir, 0755); err != nil {
		t.Fatalf("Failed to create legacy dir: %v", err)
	}

	legacyConfig := &Config{
		Type:          RepositoryTypeGit,
		RepositoryURL: "git@github.com:test/repo",
	}

	data, err := json.MarshalIndent(legacyConfig, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal config: %v", err)
	}

	legacyConfigPath := filepath.Join(legacyDir, "config.json")
	if err := os.WriteFile(legacyConfigPath, data, 0600); err != nil {
		t.Fatalf("Failed to write legacy config: %v", err)
	}

	// Load should find the legacy config
	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if loaded.Type != RepositoryTypeGit {
		t.Errorf("Expected type git, got %s", loaded.Type)
	}

	if loaded.RepositoryURL != "git@github.com:test/repo" {
		t.Errorf("Expected repo URL git@github.com:test/repo, got %s", loaded.RepositoryURL)
	}
}

func TestLoadPreferNewLocation(t *testing.T) {
	// Create a temporary home directory for testing
	tmpHome := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", originalHome)

	// Create BOTH new and legacy config files
	legacyDir := filepath.Join(tmpHome, ".claude", "plugins", "skills")
	if err := os.MkdirAll(legacyDir, 0755); err != nil {
		t.Fatalf("Failed to create legacy dir: %v", err)
	}

	legacyConfig := &Config{
		Type:          RepositoryTypeGit,
		RepositoryURL: "git@github.com:legacy/repo",
	}
	legacyData, _ := json.MarshalIndent(legacyConfig, "", "  ")
	legacyConfigPath := filepath.Join(legacyDir, "config.json")
	if err := os.WriteFile(legacyConfigPath, legacyData, 0600); err != nil {
		t.Fatalf("Failed to write legacy config: %v", err)
	}

	// Create new config file
	newConfigFile, err := utils.GetConfigFile()
	if err != nil {
		t.Fatalf("Failed to get config file: %v", err)
	}

	if err := os.MkdirAll(filepath.Dir(newConfigFile), 0755); err != nil {
		t.Fatalf("Failed to create new config dir: %v", err)
	}

	newConfig := &Config{
		Type:          RepositoryTypeGit,
		RepositoryURL: "git@github.com:new/repo",
	}
	newData, _ := json.MarshalIndent(newConfig, "", "  ")
	if err := os.WriteFile(newConfigFile, newData, 0600); err != nil {
		t.Fatalf("Failed to write new config: %v", err)
	}

	// Load should prefer the new location
	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if loaded.RepositoryURL != "git@github.com:new/repo" {
		t.Errorf("Expected new repo URL git@github.com:new/repo (should prefer new location), got %s", loaded.RepositoryURL)
	}
}

func TestLoadNoConfig(t *testing.T) {
	// Create a temporary home directory with no configs
	tmpHome := t.TempDir()
	originalHome := os.Getenv("HOME")
	originalXDG := os.Getenv("XDG_CONFIG_HOME")
	originalConfigDir := os.Getenv("SKILLS_CONFIG_DIR")

	os.Setenv("HOME", tmpHome)
	os.Setenv("XDG_CONFIG_HOME", "")
	os.Setenv("SKILLS_CONFIG_DIR", "")

	defer func() {
		os.Setenv("HOME", originalHome)
		os.Setenv("XDG_CONFIG_HOME", originalXDG)
		os.Setenv("SKILLS_CONFIG_DIR", originalConfigDir)
	}()

	// Load should fail
	_, err := Load()
	if err == nil {
		t.Error("Expected error when no config exists, got nil")
	}

	if err != nil && err.Error() != "configuration not found. Run 'sx init' first" {
		t.Errorf("Expected 'configuration not found' error, got: %v", err)
	}
}
