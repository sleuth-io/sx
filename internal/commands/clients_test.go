package commands

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/sleuth-io/sx/internal/clients"
	"github.com/sleuth-io/sx/internal/config"
)

// setupTestConfig creates a test config file with the given ForceEnabled and ForceDisabled clients
func setupTestConfig(t *testing.T, homeDir string, forceEnabled, forceDisabled []string) {
	t.Helper()

	configDir := filepath.Join(homeDir, ".config", "sx")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("Failed to create config dir: %v", err)
	}

	mpc := &config.MultiProfileConfig{
		DefaultProfile: "default",
		Profiles: map[string]*config.Profile{
			"default": {
				Type:          config.RepositoryTypeGit,
				RepositoryURL: "git@github.com:test/repo",
			},
		},
		ForceEnabledClients:  forceEnabled,
		ForceDisabledClients: forceDisabled,
	}

	data, err := json.MarshalIndent(mpc, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal config: %v", err)
	}

	configFile := filepath.Join(configDir, "config.json")
	if err := os.WriteFile(configFile, data, 0600); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}
}

// loadTestConfig loads and returns the config from the test environment
func loadTestConfig(t *testing.T) *config.MultiProfileConfig {
	t.Helper()

	mpc, err := config.LoadMultiProfile()
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}
	return mpc
}

// TestClientsEnableCommand tests the 'sx clients enable' command
func TestClientsEnableCommand(t *testing.T) {
	tests := []struct {
		name                   string
		initialForceEnabled    []string
		initialForceDisabled   []string
		clientToEnable         string
		expectedForceEnabled   []string
		expectedForceDisabled  []string
		expectedOutputContains string
		expectError            bool
	}{
		{
			name:                   "enable client removes from disabled and adds to enabled",
			initialForceEnabled:    nil,
			initialForceDisabled:   []string{"claude-code", "cursor"},
			clientToEnable:         "claude-code",
			expectedForceEnabled:   []string{"claude-code"},
			expectedForceDisabled:  []string{"cursor"},
			expectedOutputContains: "Enabled claude-code",
		},
		{
			name:                   "enable already enabled client is no-op",
			initialForceEnabled:    []string{"claude-code"},
			initialForceDisabled:   nil,
			clientToEnable:         "claude-code",
			expectedForceEnabled:   []string{"claude-code"},
			expectedForceDisabled:  nil,
			expectedOutputContains: "already enabled",
		},
		{
			name:                   "enable client not in any list adds to enabled",
			initialForceEnabled:    nil,
			initialForceDisabled:   nil,
			clientToEnable:         "cursor",
			expectedForceEnabled:   []string{"cursor"},
			expectedForceDisabled:  nil,
			expectedOutputContains: "Enabled cursor",
		},
		{
			name:           "enable unknown client fails",
			clientToEnable: "unknown-client",
			expectError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup test environment
			env := NewTestEnv(t)
			setupTestConfig(t, env.HomeDir, tt.initialForceEnabled, tt.initialForceDisabled)

			// Create and execute command
			cmd := newClientsEnableCommand()
			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)
			cmd.SetArgs([]string{tt.clientToEnable})

			err := cmd.Execute()

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("Command failed: %v", err)
			}

			// Check output
			output := stdout.String()
			if tt.expectedOutputContains != "" && !strings.Contains(output, tt.expectedOutputContains) {
				t.Errorf("Output %q does not contain %q", output, tt.expectedOutputContains)
			}

			// Verify config was updated correctly
			mpc := loadTestConfig(t)

			// Check ForceEnabledClients
			if len(tt.expectedForceEnabled) != len(mpc.ForceEnabledClients) {
				t.Errorf("ForceEnabledClients = %v, want %v", mpc.ForceEnabledClients, tt.expectedForceEnabled)
			} else {
				for _, expected := range tt.expectedForceEnabled {
					if !slices.Contains(mpc.ForceEnabledClients, expected) {
						t.Errorf("ForceEnabledClients should contain %q, got %v", expected, mpc.ForceEnabledClients)
					}
				}
			}

			// Check ForceDisabledClients
			if len(tt.expectedForceDisabled) != len(mpc.ForceDisabledClients) {
				t.Errorf("ForceDisabledClients = %v, want %v", mpc.ForceDisabledClients, tt.expectedForceDisabled)
			}
		})
	}
}

// TestClientsDisableCommand tests the 'sx clients disable' command
func TestClientsDisableCommand(t *testing.T) {
	tests := []struct {
		name                   string
		initialForceEnabled    []string
		initialForceDisabled   []string
		clientToDisable        string
		expectedForceEnabled   []string
		expectedForceDisabled  []string
		expectedOutputContains string
		expectError            bool
	}{
		{
			name:                   "disable client removes from enabled and adds to disabled",
			initialForceEnabled:    []string{"claude-code", "cursor"},
			initialForceDisabled:   nil,
			clientToDisable:        "claude-code",
			expectedForceEnabled:   []string{"cursor"},
			expectedForceDisabled:  []string{"claude-code"},
			expectedOutputContains: "Disabled claude-code",
		},
		{
			name:                   "disable already disabled client is no-op",
			initialForceEnabled:    nil,
			initialForceDisabled:   []string{"claude-code"},
			clientToDisable:        "claude-code",
			expectedForceEnabled:   nil,
			expectedForceDisabled:  []string{"claude-code"},
			expectedOutputContains: "already disabled",
		},
		{
			name:                   "disable client not in any list adds to disabled",
			initialForceEnabled:    nil,
			initialForceDisabled:   nil,
			clientToDisable:        "cursor",
			expectedForceEnabled:   nil,
			expectedForceDisabled:  []string{"cursor"},
			expectedOutputContains: "Disabled cursor",
		},
		{
			name:            "disable unknown client fails",
			clientToDisable: "unknown-client",
			expectError:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup test environment
			env := NewTestEnv(t)
			setupTestConfig(t, env.HomeDir, tt.initialForceEnabled, tt.initialForceDisabled)

			// Create and execute command
			cmd := newClientsDisableCommand()
			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)
			cmd.SetArgs([]string{tt.clientToDisable})

			err := cmd.Execute()

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("Command failed: %v", err)
			}

			// Check output
			output := stdout.String()
			if tt.expectedOutputContains != "" && !strings.Contains(output, tt.expectedOutputContains) {
				t.Errorf("Output %q does not contain %q", output, tt.expectedOutputContains)
			}

			// Verify config was updated correctly
			mpc := loadTestConfig(t)

			// Check ForceEnabledClients
			if len(tt.expectedForceEnabled) != len(mpc.ForceEnabledClients) {
				t.Errorf("ForceEnabledClients = %v, want %v", mpc.ForceEnabledClients, tt.expectedForceEnabled)
			}

			// Check ForceDisabledClients
			if len(tt.expectedForceDisabled) != len(mpc.ForceDisabledClients) {
				t.Errorf("ForceDisabledClients = %v, want %v", mpc.ForceDisabledClients, tt.expectedForceDisabled)
			} else {
				for _, expected := range tt.expectedForceDisabled {
					if !slices.Contains(mpc.ForceDisabledClients, expected) {
						t.Errorf("ForceDisabledClients should contain %q, got %v", expected, mpc.ForceDisabledClients)
					}
				}
			}
		})
	}
}

// TestClientsResetCommand tests the 'sx clients reset' command
func TestClientsResetCommand(t *testing.T) {
	tests := []struct {
		name                   string
		initialForceEnabled    []string
		initialForceDisabled   []string
		initialEnabledClients  []string // deprecated field
		expectedOutputContains string
	}{
		{
			name:                   "reset clears force enabled and disabled",
			initialForceEnabled:    []string{"claude-code"},
			initialForceDisabled:   []string{"cursor"},
			expectedOutputContains: "Reset to default",
		},
		{
			name:                   "reset when already default",
			initialForceEnabled:    nil,
			initialForceDisabled:   nil,
			expectedOutputContains: "Already using default",
		},
		{
			name:                   "reset clears deprecated EnabledClients too",
			initialEnabledClients:  []string{"claude-code"},
			expectedOutputContains: "Reset to default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup test environment
			env := NewTestEnv(t)

			// Create config with test values
			configDir := filepath.Join(env.HomeDir, ".config", "sx")
			if err := os.MkdirAll(configDir, 0755); err != nil {
				t.Fatalf("Failed to create config dir: %v", err)
			}

			// For the deprecated EnabledClients test, we need to write the file directly
			// because SaveMultiProfile intentionally doesn't save the deprecated field
			if len(tt.initialEnabledClients) > 0 {
				// Write config with deprecated field directly as JSON
				configData := map[string]any{
					"defaultProfile": "default",
					"profiles": map[string]any{
						"default": map[string]any{
							"type":          "git",
							"repositoryUrl": "git@github.com:test/repo",
						},
					},
					"enabledClients": tt.initialEnabledClients,
				}
				data, _ := json.MarshalIndent(configData, "", "  ")
				configFile := filepath.Join(configDir, "config.json")
				if err := os.WriteFile(configFile, data, 0600); err != nil {
					t.Fatalf("Failed to write config: %v", err)
				}
			} else {
				mpc := &config.MultiProfileConfig{
					DefaultProfile: "default",
					Profiles: map[string]*config.Profile{
						"default": {
							Type:          config.RepositoryTypeGit,
							RepositoryURL: "git@github.com:test/repo",
						},
					},
					ForceEnabledClients:  tt.initialForceEnabled,
					ForceDisabledClients: tt.initialForceDisabled,
				}

				if err := config.SaveMultiProfile(mpc); err != nil {
					t.Fatalf("Failed to save config: %v", err)
				}
			}

			// Create and execute command
			cmd := newClientsResetCommand()
			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)

			if err := cmd.Execute(); err != nil {
				t.Fatalf("Command failed: %v", err)
			}

			// Check output
			output := stdout.String()
			if tt.expectedOutputContains != "" && !strings.Contains(output, tt.expectedOutputContains) {
				t.Errorf("Output %q does not contain %q", output, tt.expectedOutputContains)
			}

			// Verify config was reset
			loaded := loadTestConfig(t)
			if len(loaded.ForceEnabledClients) != 0 {
				t.Errorf("ForceEnabledClients should be empty after reset, got %v", loaded.ForceEnabledClients)
			}
			if len(loaded.ForceDisabledClients) != 0 {
				t.Errorf("ForceDisabledClients should be empty after reset, got %v", loaded.ForceDisabledClients)
			}
			if len(loaded.EnabledClients) != 0 {
				t.Errorf("EnabledClients should be empty after reset, got %v", loaded.EnabledClients)
			}
		})
	}
}

// TestGatherClientInfo tests the gatherClientInfo function
func TestGatherClientInfo(t *testing.T) {
	tests := []struct {
		name            string
		forceEnabled    []string
		forceDisabled   []string
		setupClients    func(env *TestEnv) // optional setup for client detection
		checkClientInfo func(t *testing.T, infos []ClientInfo)
	}{
		{
			name:          "force disabled client shows as disabled",
			forceDisabled: []string{"claude-code"},
			checkClientInfo: func(t *testing.T, infos []ClientInfo) {
				for _, info := range infos {
					if info.ID == "claude-code" {
						if !info.ForceDisabled {
							t.Errorf("claude-code should be ForceDisabled")
						}
						if info.ForceEnabled {
							t.Errorf("claude-code should not be ForceEnabled")
						}
					}
				}
			},
		},
		{
			name:         "force enabled client shows as enabled",
			forceEnabled: []string{"github-copilot"},
			checkClientInfo: func(t *testing.T, infos []ClientInfo) {
				for _, info := range infos {
					if info.ID == "github-copilot" {
						if !info.ForceEnabled {
							t.Errorf("github-copilot should be ForceEnabled")
						}
						if info.ForceDisabled {
							t.Errorf("github-copilot should not be ForceDisabled")
						}
					}
				}
			},
		},
		{
			name: "no config means no force flags",
			checkClientInfo: func(t *testing.T, infos []ClientInfo) {
				for _, info := range infos {
					if info.ForceEnabled {
						t.Errorf("%s should not be ForceEnabled with no config", info.ID)
					}
					if info.ForceDisabled {
						t.Errorf("%s should not be ForceDisabled with no config", info.ID)
					}
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup test environment
			env := NewTestEnv(t)
			setupTestConfig(t, env.HomeDir, tt.forceEnabled, tt.forceDisabled)

			if tt.setupClients != nil {
				tt.setupClients(env)
			}

			// Get client info
			infos := gatherClientInfo()

			// Verify
			if tt.checkClientInfo != nil {
				tt.checkClientInfo(t, infos)
			}
		})
	}
}

// TestClientsSorting tests that clients are sorted by installed status then name
func TestClientsSorting(t *testing.T) {
	env := NewTestEnv(t)
	setupTestConfig(t, env.HomeDir, nil, nil)

	// Gather info - clients should be sorted
	infos := gatherClientInfo()

	// Verify sorting: installed first, then by name
	notInstalledSeen := false
	lastInstalledName := ""
	lastNotInstalledName := ""

	for _, info := range infos {
		if info.Installed {
			if notInstalledSeen {
				t.Error("Not-installed client appeared before installed client")
			}
			if lastInstalledName != "" && info.Name < lastInstalledName {
				t.Errorf("Installed clients not sorted by name: %s came after %s", info.Name, lastInstalledName)
			}
			lastInstalledName = info.Name
		} else {
			notInstalledSeen = true
			if lastNotInstalledName != "" && info.Name < lastNotInstalledName {
				t.Errorf("Not-installed clients not sorted by name: %s came after %s", info.Name, lastNotInstalledName)
			}
			lastNotInstalledName = info.Name
		}
	}
}

// TestComputeDisabledClients tests the computeDisabledClients function
func TestComputeDisabledClients(t *testing.T) {
	// Note: This test needs detected clients, which requires setting up
	// the detection markers in the test environment

	tests := []struct {
		name             string
		setupDetected    func(env *TestEnv) // Setup which clients are "detected"
		selectedClients  []string
		expectedDisabled []string
	}{
		{
			name: "nil selection means all enabled",
			setupDetected: func(env *TestEnv) {
				// Claude and Copilot are detected (already setup in NewTestEnv)
			},
			selectedClients:  nil,
			expectedDisabled: nil, // nil selection = all enabled, no disabled list
		},
		{
			name: "empty selection means all enabled",
			setupDetected: func(env *TestEnv) {
				// Claude and Copilot are detected
			},
			selectedClients:  []string{},
			expectedDisabled: nil, // empty selection = all enabled, no disabled list
		},
		{
			name: "no detected clients returns nil",
			setupDetected: func(env *TestEnv) {
				// Remove all detection markers
				os.RemoveAll(filepath.Join(env.HomeDir, ".claude"))
				os.RemoveAll(filepath.Join(env.HomeDir, ".copilot"))
				os.RemoveAll(filepath.Join(env.HomeDir, ".gemini"))
			},
			selectedClients:  []string{"claude-code"},
			expectedDisabled: nil,
		},
		{
			name: "all detected clients selected returns nil",
			setupDetected: func(env *TestEnv) {
				// Claude, Copilot, and Gemini are detected (already setup in NewTestEnv)
			},
			selectedClients:  []string{"claude-code", "github-copilot", "gemini"},
			expectedDisabled: nil,
		},
		{
			name: "one detected client not selected is disabled",
			setupDetected: func(env *TestEnv) {
				// Claude, Copilot, and Gemini are detected
			},
			selectedClients:  []string{"claude-code"},
			expectedDisabled: []string{"github-copilot", "gemini"},
		},
		{
			name: "undetected client in selection is ignored",
			setupDetected: func(env *TestEnv) {
				// Only Claude detected
				os.RemoveAll(filepath.Join(env.HomeDir, ".copilot"))
				os.RemoveAll(filepath.Join(env.HomeDir, ".gemini"))
			},
			selectedClients:  []string{"claude-code", "github-copilot"}, // copilot not detected
			expectedDisabled: nil,                                       // copilot not disabled because not detected
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := NewTestEnv(t)

			if tt.setupDetected != nil {
				tt.setupDetected(env)
			}

			disabled := computeDisabledClients(tt.selectedClients)

			// Check result length
			if len(disabled) != len(tt.expectedDisabled) {
				t.Errorf("computeDisabledClients() = %v, want %v", disabled, tt.expectedDisabled)
				return
			}

			// Check each expected disabled client
			for _, expected := range tt.expectedDisabled {
				if !slices.Contains(disabled, expected) {
					t.Errorf("computeDisabledClients() should contain %q, got %v", expected, disabled)
				}
			}
		})
	}
}

// TestClientInfoHasAllKnownClients verifies gatherClientInfo returns all known clients
func TestClientInfoHasAllKnownClients(t *testing.T) {
	env := NewTestEnv(t)
	setupTestConfig(t, env.HomeDir, nil, nil)

	infos := gatherClientInfo()

	// Should have entries for all known client IDs
	allIDs := clients.AllClientIDs()
	infoIDs := make(map[string]bool)
	for _, info := range infos {
		infoIDs[info.ID] = true
	}

	for _, id := range allIDs {
		if !infoIDs[id] {
			t.Errorf("gatherClientInfo() missing client %q", id)
		}
	}
}

// TestClientsCommandShowsGemini tests that 'sx clients' shows Gemini with installed status and version
func TestClientsCommandShowsGemini(t *testing.T) {
	env := NewTestEnv(t)

	// Create .gemini directory so Gemini is detected as installed
	geminiDir := filepath.Join(env.HomeDir, ".gemini")
	if err := os.MkdirAll(geminiDir, 0755); err != nil {
		t.Fatalf("Failed to create .gemini dir: %v", err)
	}

	setupTestConfig(t, env.HomeDir, nil, nil)

	// Use gatherClientInfo directly since runClients writes to os.Stdout
	infos := gatherClientInfo()

	// Find Gemini in the results
	var geminiInfo *ClientInfo
	for i := range infos {
		if infos[i].ID == "gemini" {
			geminiInfo = &infos[i]
			break
		}
	}

	if geminiInfo == nil {
		t.Fatal("Gemini client not found in gatherClientInfo()")
	}

	// Check that Gemini is detected as installed
	if !geminiInfo.Installed {
		t.Error("Gemini should be detected as installed when .gemini directory exists")
	}

	// Check that Gemini has the correct display name
	if geminiInfo.Name != "Gemini Code Assist" {
		t.Errorf("Gemini Name = %q, want %q", geminiInfo.Name, "Gemini Code Assist")
	}
}

// TestClientsCommandGeminiVersion tests that 'sx clients' shows Gemini version when CLI is available
func TestClientsCommandGeminiVersion(t *testing.T) {
	env := NewTestEnv(t)

	// Create .gemini directory so Gemini is detected as installed
	geminiDir := filepath.Join(env.HomeDir, ".gemini")
	if err := os.MkdirAll(geminiDir, 0755); err != nil {
		t.Fatalf("Failed to create .gemini dir: %v", err)
	}

	setupTestConfig(t, env.HomeDir, nil, nil)

	// Get client info directly to check version
	infos := gatherClientInfo()

	var geminiInfo *ClientInfo
	for i := range infos {
		if infos[i].ID == "gemini" {
			geminiInfo = &infos[i]
			break
		}
	}

	if geminiInfo == nil {
		t.Fatal("Gemini client not found in gatherClientInfo()")
	}

	if !geminiInfo.Installed {
		t.Error("Gemini should be detected as installed")
	}

	// If gemini CLI is available, version should be non-empty
	// This test passes regardless of whether CLI is installed,
	// but verifies the version field is populated when it is
	if geminiInfo.Version != "" {
		t.Logf("Gemini version detected: %s", geminiInfo.Version)
		// Version format should be semver-like (e.g., "0.29.5")
		if !strings.Contains(geminiInfo.Version, ".") {
			t.Errorf("Gemini version %q doesn't look like a valid version", geminiInfo.Version)
		}
	} else {
		t.Log("Gemini CLI not available, skipping version check")
	}
}
