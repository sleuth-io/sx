package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
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

// Test IsClientEnabled with ForceDisabledClients
func TestIsClientEnabled(t *testing.T) {
	tests := []struct {
		name                  string
		forceEnabled          []string
		forceDisabled         []string
		clientID              string
		expectedEnabled       bool
		expectedForceEnabled  bool
		expectedForceDisabled bool
	}{
		{
			name:                  "client with no config is enabled",
			forceEnabled:          nil,
			forceDisabled:         nil,
			clientID:              "claude-code",
			expectedEnabled:       true,
			expectedForceEnabled:  false,
			expectedForceDisabled: false,
		},
		{
			name:                  "force-disabled client is disabled",
			forceEnabled:          nil,
			forceDisabled:         []string{"claude-code"},
			clientID:              "claude-code",
			expectedEnabled:       false,
			expectedForceEnabled:  false,
			expectedForceDisabled: true,
		},
		{
			name:                  "force-enabled client is enabled",
			forceEnabled:          []string{"claude-code"},
			forceDisabled:         nil,
			clientID:              "claude-code",
			expectedEnabled:       true,
			expectedForceEnabled:  true,
			expectedForceDisabled: false,
		},
		{
			name:                  "force-disabled takes precedence over force-enabled",
			forceEnabled:          []string{"claude-code"},
			forceDisabled:         []string{"claude-code"},
			clientID:              "claude-code",
			expectedEnabled:       false,
			expectedForceEnabled:  true,
			expectedForceDisabled: true,
		},
		{
			name:                  "other client is enabled when one is disabled",
			forceEnabled:          nil,
			forceDisabled:         []string{"cursor"},
			clientID:              "claude-code",
			expectedEnabled:       true,
			expectedForceEnabled:  false,
			expectedForceDisabled: false,
		},
		{
			name:                  "multiple clients can be disabled",
			forceEnabled:          nil,
			forceDisabled:         []string{"claude-code", "cursor"},
			clientID:              "cursor",
			expectedEnabled:       false,
			expectedForceEnabled:  false,
			expectedForceDisabled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Type:                 RepositoryTypeGit,
				RepositoryURL:        "git@github.com:test/repo",
				ForceEnabledClients:  tt.forceEnabled,
				ForceDisabledClients: tt.forceDisabled,
			}

			if got := cfg.IsClientEnabled(tt.clientID); got != tt.expectedEnabled {
				t.Errorf("IsClientEnabled(%q) = %v, want %v", tt.clientID, got, tt.expectedEnabled)
			}
			if got := cfg.IsClientForceEnabled(tt.clientID); got != tt.expectedForceEnabled {
				t.Errorf("IsClientForceEnabled(%q) = %v, want %v", tt.clientID, got, tt.expectedForceEnabled)
			}
			if got := cfg.IsClientForceDisabled(tt.clientID); got != tt.expectedForceDisabled {
				t.Errorf("IsClientForceDisabled(%q) = %v, want %v", tt.clientID, got, tt.expectedForceDisabled)
			}
		})
	}
}

// Test HasExplicitClientConfig
func TestHasExplicitClientConfig(t *testing.T) {
	tests := []struct {
		name          string
		forceEnabled  []string
		forceDisabled []string
		expected      bool
	}{
		{
			name:          "no config returns false",
			forceEnabled:  nil,
			forceDisabled: nil,
			expected:      false,
		},
		{
			name:          "empty slices returns false",
			forceEnabled:  []string{},
			forceDisabled: []string{},
			expected:      false,
		},
		{
			name:          "force enabled returns true",
			forceEnabled:  []string{"claude-code"},
			forceDisabled: nil,
			expected:      true,
		},
		{
			name:          "force disabled returns true",
			forceEnabled:  nil,
			forceDisabled: []string{"cursor"},
			expected:      true,
		},
		{
			name:          "both configured returns true",
			forceEnabled:  []string{"claude-code"},
			forceDisabled: []string{"cursor"},
			expected:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Type:                 RepositoryTypeGit,
				RepositoryURL:        "git@github.com:test/repo",
				ForceEnabledClients:  tt.forceEnabled,
				ForceDisabledClients: tt.forceDisabled,
			}

			if got := cfg.HasExplicitClientConfig(); got != tt.expected {
				t.Errorf("HasExplicitClientConfig() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// Test MigrateEnabledClients
func TestMigrateEnabledClients(t *testing.T) {
	// Create a temporary home directory for testing
	tmpHome := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", originalHome)

	allClientIDs := []string{"claude-code", "cursor", "github-copilot"}

	tests := []struct {
		name                      string
		enabledClients            []string
		expectedMigrated          bool
		expectedForceDisabled     []string
		expectedEnabledClientsNil bool
	}{
		{
			name:                      "no migration needed when EnabledClients is empty",
			enabledClients:            nil,
			expectedMigrated:          false,
			expectedForceDisabled:     nil,
			expectedEnabledClientsNil: true,
		},
		{
			name:                      "all clients enabled means no force disabled",
			enabledClients:            []string{"claude-code", "cursor", "github-copilot"},
			expectedMigrated:          true,
			expectedForceDisabled:     nil,
			expectedEnabledClientsNil: true,
		},
		{
			name:                      "one client enabled disables others",
			enabledClients:            []string{"claude-code"},
			expectedMigrated:          true,
			expectedForceDisabled:     []string{"cursor", "github-copilot"},
			expectedEnabledClientsNil: true,
		},
		{
			name:                      "two clients enabled disables third",
			enabledClients:            []string{"claude-code", "cursor"},
			expectedMigrated:          true,
			expectedForceDisabled:     []string{"github-copilot"},
			expectedEnabledClientsNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fresh config dir for each test
			configFile, _ := utils.GetConfigFile()
			os.MkdirAll(filepath.Dir(configFile), 0755)

			// Create config with old-style EnabledClients
			mpc := &MultiProfileConfig{
				DefaultProfile: "default",
				Profiles: map[string]*Profile{
					"default": {
						Type:          RepositoryTypeGit,
						RepositoryURL: "git@github.com:test/repo",
					},
				},
				EnabledClients: tt.enabledClients,
			}

			migrated, err := mpc.MigrateEnabledClients(allClientIDs)
			if err != nil {
				t.Fatalf("MigrateEnabledClients() error = %v", err)
			}

			if migrated != tt.expectedMigrated {
				t.Errorf("MigrateEnabledClients() migrated = %v, want %v", migrated, tt.expectedMigrated)
			}

			// Check that EnabledClients is cleared after migration
			if tt.expectedEnabledClientsNil && len(mpc.EnabledClients) > 0 {
				t.Errorf("EnabledClients should be nil after migration, got %v", mpc.EnabledClients)
			}

			// Check ForceDisabledClients
			if len(tt.expectedForceDisabled) == 0 && len(mpc.ForceDisabledClients) > 0 {
				t.Errorf("ForceDisabledClients should be empty, got %v", mpc.ForceDisabledClients)
			}
			for _, expected := range tt.expectedForceDisabled {
				if !slices.Contains(mpc.ForceDisabledClients, expected) {
					t.Errorf("ForceDisabledClients should contain %q, got %v", expected, mpc.ForceDisabledClients)
				}
			}

			// Clean up for next test
			os.RemoveAll(filepath.Dir(configFile))
		})
	}
}

// Test NeedsMigration
func TestNeedsMigration(t *testing.T) {
	tests := []struct {
		name           string
		enabledClients []string
		expected       bool
	}{
		{
			name:           "nil EnabledClients does not need migration",
			enabledClients: nil,
			expected:       false,
		},
		{
			name:           "empty EnabledClients does not need migration",
			enabledClients: []string{},
			expected:       false,
		},
		{
			name:           "non-empty EnabledClients needs migration",
			enabledClients: []string{"claude-code"},
			expected:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mpc := &MultiProfileConfig{
				DefaultProfile: "default",
				Profiles:       map[string]*Profile{},
				EnabledClients: tt.enabledClients,
			}

			if got := mpc.NeedsMigration(); got != tt.expected {
				t.Errorf("NeedsMigration() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// Test ToConfig properly transfers client settings
func TestProfileToConfig(t *testing.T) {
	profile := &Profile{
		Type:          RepositoryTypeGit,
		RepositoryURL: "git@github.com:test/repo",
	}

	forceEnabled := []string{"claude-code"}
	forceDisabled := []string{"cursor", "github-copilot"}

	cfg := profile.ToConfig(forceEnabled, forceDisabled)

	if cfg.Type != RepositoryTypeGit {
		t.Errorf("Type = %v, want %v", cfg.Type, RepositoryTypeGit)
	}
	if cfg.RepositoryURL != "git@github.com:test/repo" {
		t.Errorf("RepositoryURL = %v, want git@github.com:test/repo", cfg.RepositoryURL)
	}
	if len(cfg.ForceEnabledClients) != 1 || cfg.ForceEnabledClients[0] != "claude-code" {
		t.Errorf("ForceEnabledClients = %v, want [claude-code]", cfg.ForceEnabledClients)
	}
	if len(cfg.ForceDisabledClients) != 2 {
		t.Errorf("ForceDisabledClients = %v, want [cursor github-copilot]", cfg.ForceDisabledClients)
	}
}

// Test SaveMultiProfile persists client settings correctly
func TestSaveMultiProfileWithClientSettings(t *testing.T) {
	tmpHome := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", originalHome)

	mpc := &MultiProfileConfig{
		DefaultProfile: "default",
		Profiles: map[string]*Profile{
			"default": {
				Type:          RepositoryTypeGit,
				RepositoryURL: "git@github.com:test/repo",
			},
		},
		ForceEnabledClients:  []string{"claude-code"},
		ForceDisabledClients: []string{"cursor"},
	}

	if err := SaveMultiProfile(mpc); err != nil {
		t.Fatalf("SaveMultiProfile() failed: %v", err)
	}

	// Load and verify
	loaded, err := LoadMultiProfile()
	if err != nil {
		t.Fatalf("LoadMultiProfile() failed: %v", err)
	}

	if len(loaded.ForceEnabledClients) != 1 || loaded.ForceEnabledClients[0] != "claude-code" {
		t.Errorf("ForceEnabledClients = %v, want [claude-code]", loaded.ForceEnabledClients)
	}
	if len(loaded.ForceDisabledClients) != 1 || loaded.ForceDisabledClients[0] != "cursor" {
		t.Errorf("ForceDisabledClients = %v, want [cursor]", loaded.ForceDisabledClients)
	}
}

// Test that Load includes client settings
func TestLoadIncludesClientSettings(t *testing.T) {
	tmpHome := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", originalHome)

	mpc := &MultiProfileConfig{
		DefaultProfile: "default",
		Profiles: map[string]*Profile{
			"default": {
				Type:          RepositoryTypeGit,
				RepositoryURL: "git@github.com:test/repo",
			},
		},
		ForceEnabledClients:  []string{"github-copilot"},
		ForceDisabledClients: []string{"cursor"},
	}

	if err := SaveMultiProfile(mpc); err != nil {
		t.Fatalf("SaveMultiProfile() failed: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if !cfg.IsClientForceEnabled("github-copilot") {
		t.Error("Expected github-copilot to be force-enabled")
	}
	if !cfg.IsClientForceDisabled("cursor") {
		t.Error("Expected cursor to be force-disabled")
	}
	if cfg.IsClientEnabled("cursor") {
		t.Error("Expected cursor to be disabled")
	}
	if !cfg.IsClientEnabled("claude-code") {
		t.Error("Expected claude-code to be enabled (default)")
	}
}
