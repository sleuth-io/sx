package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sleuth-io/sx/internal/utils"
)

func TestLoadMigratesOldFormat(t *testing.T) {
	// Create a temporary home directory for testing
	tmpHome := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", originalHome)

	// Create old single-config format file in the new location
	configFile, err := utils.GetConfigFile()
	if err != nil {
		t.Fatalf("Failed to get config file: %v", err)
	}

	if err := os.MkdirAll(filepath.Dir(configFile), 0755); err != nil {
		t.Fatalf("Failed to create config dir: %v", err)
	}

	oldConfig := map[string]any{
		"type":          "git",
		"repositoryUrl": "git@github.com:test/repo",
	}

	data, err := json.MarshalIndent(oldConfig, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal config: %v", err)
	}

	if err := os.WriteFile(configFile, data, 0600); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	// Load should migrate the old format
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

func TestLoadMultiProfileFormat(t *testing.T) {
	// Create a temporary home directory for testing
	tmpHome := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", originalHome)

	// Create multi-profile format config file
	configFile, err := utils.GetConfigFile()
	if err != nil {
		t.Fatalf("Failed to get config file: %v", err)
	}

	if err := os.MkdirAll(filepath.Dir(configFile), 0755); err != nil {
		t.Fatalf("Failed to create config dir: %v", err)
	}

	mpc := &MultiProfileConfig{
		DefaultProfile: "production",
		Profiles: map[string]*Profile{
			"default": {
				Type:          RepositoryTypeGit,
				RepositoryURL: "git@github.com:default/repo",
			},
			"production": {
				Type:          RepositoryTypeSleuth,
				RepositoryURL: "https://app.skills.new",
				AuthToken:     "test-token",
			},
		},
	}

	data, err := json.MarshalIndent(mpc, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal config: %v", err)
	}

	if err := os.WriteFile(configFile, data, 0600); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	// Load should return the active profile (production)
	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if loaded.Type != RepositoryTypeSleuth {
		t.Errorf("Expected type sleuth, got %s", loaded.Type)
	}

	if loaded.RepositoryURL != "https://app.skills.new" {
		t.Errorf("Expected repo URL https://app.skills.new, got %s", loaded.RepositoryURL)
	}
}

func TestLoadWithProfileOverride(t *testing.T) {
	// Create a temporary home directory for testing
	tmpHome := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", originalHome)

	// Create multi-profile format config file
	configFile, err := utils.GetConfigFile()
	if err != nil {
		t.Fatalf("Failed to get config file: %v", err)
	}

	if err := os.MkdirAll(filepath.Dir(configFile), 0755); err != nil {
		t.Fatalf("Failed to create config dir: %v", err)
	}

	mpc := &MultiProfileConfig{
		DefaultProfile: "production",
		Profiles: map[string]*Profile{
			"staging": {
				Type:          RepositoryTypeGit,
				RepositoryURL: "git@github.com:staging/repo",
			},
			"production": {
				Type:          RepositoryTypeSleuth,
				RepositoryURL: "https://app.skills.new",
			},
		},
	}

	data, err := json.MarshalIndent(mpc, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal config: %v", err)
	}

	if err := os.WriteFile(configFile, data, 0600); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	// Override the active profile
	SetActiveProfile("staging")
	defer SetActiveProfile("") // Clean up

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if loaded.Type != RepositoryTypeGit {
		t.Errorf("Expected type git (staging profile), got %s", loaded.Type)
	}

	if loaded.RepositoryURL != "git@github.com:staging/repo" {
		t.Errorf("Expected staging repo URL, got %s", loaded.RepositoryURL)
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

func TestSaveToProfile(t *testing.T) {
	// Create a temporary home directory for testing
	tmpHome := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", originalHome)

	// Save a new config
	cfg := &Config{
		Type:          RepositoryTypeGit,
		RepositoryURL: "git@github.com:test/repo",
	}

	if err := SaveToProfile(cfg, "myprofile"); err != nil {
		t.Fatalf("SaveToProfile() failed: %v", err)
	}

	// Load and verify
	mpc, err := LoadMultiProfile()
	if err != nil {
		t.Fatalf("LoadMultiProfile() failed: %v", err)
	}

	profile, ok := mpc.GetProfile("myprofile")
	if !ok {
		t.Fatal("Expected myprofile to exist")
	}

	if profile.RepositoryURL != "git@github.com:test/repo" {
		t.Errorf("Expected repo URL git@github.com:test/repo, got %s", profile.RepositoryURL)
	}
}

func TestProfileListAndSwitch(t *testing.T) {
	// Create a temporary home directory for testing
	tmpHome := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", originalHome)

	// Create multi-profile config
	mpc := &MultiProfileConfig{
		DefaultProfile: "default",
		Profiles: map[string]*Profile{
			"default": {
				Type:          RepositoryTypeGit,
				RepositoryURL: "git@github.com:default/repo",
			},
			"staging": {
				Type:          RepositoryTypeGit,
				RepositoryURL: "git@github.com:staging/repo",
			},
		},
	}

	if err := SaveMultiProfile(mpc); err != nil {
		t.Fatalf("SaveMultiProfile() failed: %v", err)
	}

	// List profiles
	profiles := mpc.ListProfiles()
	if len(profiles) != 2 {
		t.Errorf("Expected 2 profiles, got %d", len(profiles))
	}

	// Switch to staging
	if err := mpc.SetDefaultProfile("staging"); err != nil {
		t.Fatalf("SetDefaultProfile() failed: %v", err)
	}

	if mpc.DefaultProfile != "staging" {
		t.Errorf("Expected default profile to be staging, got %s", mpc.DefaultProfile)
	}

	// Try to switch to non-existent profile
	if err := mpc.SetDefaultProfile("nonexistent"); err == nil {
		t.Error("Expected error when switching to non-existent profile")
	}
}
