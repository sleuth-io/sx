package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/sleuth-io/sx/internal/utils"
)

// DefaultProfileName is the name used for the default profile
const DefaultProfileName = "default"

// Profile represents a single configuration profile
type Profile struct {
	// Type of repository: "sleuth", "git", or "path"
	Type RepositoryType `json:"type"`

	// ServerURL is the Sleuth server URL (only for type=sleuth)
	ServerURL string `json:"serverUrl,omitempty"`

	// AuthToken is the OAuth token for Sleuth server (only for type=sleuth)
	AuthToken string `json:"authToken,omitempty"`

	// RepositoryURL is the repository URL
	// - For git: git repository URL (https://github.com/org/repo.git)
	// - For path: file:// URL pointing to local directory (file:///path/to/repo)
	RepositoryURL string `json:"repositoryUrl,omitempty"`
}

// MultiProfileConfig represents the full configuration file with multiple profiles
type MultiProfileConfig struct {
	// DefaultProfile is the name of the currently active profile
	DefaultProfile string `json:"defaultProfile"`

	// Profiles is a map of profile name to profile configuration
	Profiles map[string]*Profile `json:"profiles"`

	// EnabledClients is DEPRECATED - use ForceEnabledClients/ForceDisabledClients instead.
	// Kept for migration purposes only.
	EnabledClients []string `json:"enabledClients,omitempty"`

	// ForceEnabledClients is the list of client IDs that should always be enabled,
	// even if not detected. This is global across all profiles.
	ForceEnabledClients []string `json:"forceEnabledClients,omitempty"`

	// ForceDisabledClients is the list of client IDs that should always be disabled,
	// even if detected. This is global across all profiles.
	ForceDisabledClients []string `json:"forceDisabledClients,omitempty"`

	// BootstrapOptions stores user consent for bootstrap items (hooks, MCP servers).
	// Keyed by option key, nil/missing = yes (backwards compatible).
	BootstrapOptions map[string]*bool `json:"bootstrapOptions,omitempty"`
}

// GetBootstrapOption returns whether a bootstrap option is enabled.
// Returns true if the option is missing/nil (backwards compatible - existing users get everything).
func (mpc *MultiProfileConfig) GetBootstrapOption(key string) bool {
	if mpc.BootstrapOptions == nil {
		return true // nil = yes
	}
	if val, ok := mpc.BootstrapOptions[key]; ok && val != nil {
		return *val
	}
	return true // missing/nil = yes
}

// SetBootstrapOption sets a bootstrap option value.
func (mpc *MultiProfileConfig) SetBootstrapOption(key string, enabled bool) {
	if mpc.BootstrapOptions == nil {
		mpc.BootstrapOptions = make(map[string]*bool)
	}
	mpc.BootstrapOptions[key] = &enabled
}

// activeProfileOverride is set via SetActiveProfile to override the default profile
var activeProfileOverride string

// SetActiveProfile sets the active profile for the current session
// This is typically set from a --profile flag or SX_PROFILE env var
func SetActiveProfile(name string) {
	activeProfileOverride = name
}

// GetActiveProfileOverride returns the current profile override, if any
func GetActiveProfileOverride() string {
	return activeProfileOverride
}

// isMultiProfileConfig checks if the JSON data is a multi-profile config
func isMultiProfileConfig(data []byte) bool {
	var probe struct {
		Profiles map[string]any `json:"profiles"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return false
	}
	return probe.Profiles != nil
}

// loadMultiProfileConfig loads and parses a multi-profile config file
func loadMultiProfileConfig(data []byte) (*MultiProfileConfig, error) {
	var mpc MultiProfileConfig
	if err := json.Unmarshal(data, &mpc); err != nil {
		return nil, fmt.Errorf("failed to parse multi-profile config: %w", err)
	}
	if mpc.Profiles == nil {
		mpc.Profiles = make(map[string]*Profile)
	}
	// Migrate old bootstrap option keys to new consolidated keys
	migrateBootstrapKeys(&mpc)
	return &mpc, nil
}

// migrateBootstrapKeys migrates old client-specific bootstrap keys to the new shared keys.
// Old keys like "cursor_session_hook" and "copilot_session_hook" are migrated to "session_hook".
func migrateBootstrapKeys(mpc *MultiProfileConfig) {
	if mpc.BootstrapOptions == nil {
		return
	}

	// Map of old keys to new keys
	migrations := map[string]string{
		"cursor_session_hook":    "session_hook",
		"copilot_session_hook":   "session_hook",
		"copilot_analytics_hook": "analytics_hook",
	}

	for oldKey, newKey := range migrations {
		if val, ok := mpc.BootstrapOptions[oldKey]; ok {
			// Only migrate if the new key isn't already set
			if _, exists := mpc.BootstrapOptions[newKey]; !exists {
				mpc.BootstrapOptions[newKey] = val
			}
			delete(mpc.BootstrapOptions, oldKey)
		}
	}
}

// migrateOldConfig converts an old single-profile config to multi-profile format
func migrateOldConfig(data []byte) (*MultiProfileConfig, error) {
	var oldCfg struct {
		Type           RepositoryType `json:"type"`
		ServerURL      string         `json:"serverUrl,omitempty"`
		AuthToken      string         `json:"authToken,omitempty"`
		RepositoryURL  string         `json:"repositoryUrl,omitempty"`
		EnabledClients []string       `json:"enabledClients,omitempty"`
	}
	if err := json.Unmarshal(data, &oldCfg); err != nil {
		return nil, fmt.Errorf("failed to parse legacy config: %w", err)
	}

	// Create multi-profile config with the old config as "default" profile
	mpc := &MultiProfileConfig{
		DefaultProfile: DefaultProfileName,
		Profiles: map[string]*Profile{
			DefaultProfileName: {
				Type:          oldCfg.Type,
				ServerURL:     oldCfg.ServerURL,
				AuthToken:     oldCfg.AuthToken,
				RepositoryURL: oldCfg.RepositoryURL,
			},
		},
		EnabledClients: oldCfg.EnabledClients,
	}

	return mpc, nil
}

// LoadMultiProfile loads the full multi-profile configuration
func LoadMultiProfile() (*MultiProfileConfig, error) {
	configFile, err := utils.GetConfigFile()
	if err != nil {
		return nil, fmt.Errorf("failed to get config file path: %w", err)
	}

	if !utils.FileExists(configFile) {
		return nil, errors.New("configuration not found. Run 'sx init' first")
	}

	data, err := os.ReadFile(configFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Check if it's already multi-profile format
	if isMultiProfileConfig(data) {
		return loadMultiProfileConfig(data)
	}

	// Migrate old single-config format to multi-profile format
	return migrateOldConfig(data)
}

// backwardsCompatibleConfig is the on-disk format that includes both
// top-level fields (for old binaries) and profiles (for new binaries)
type backwardsCompatibleConfig struct {
	// Top-level fields for backwards compatibility with old binaries
	Type           RepositoryType `json:"type,omitempty"`
	ServerURL      string         `json:"serverUrl,omitempty"`
	AuthToken      string         `json:"authToken,omitempty"`
	RepositoryURL  string         `json:"repositoryUrl,omitempty"`
	EnabledClients []string       `json:"enabledClients,omitempty"` // DEPRECATED

	// New multi-profile fields
	DefaultProfile string              `json:"defaultProfile"`
	Profiles       map[string]*Profile `json:"profiles"`

	// Client enable/disable settings (global across profiles)
	ForceEnabledClients  []string `json:"forceEnabledClients,omitempty"`
	ForceDisabledClients []string `json:"forceDisabledClients,omitempty"`

	// Bootstrap options (global across profiles)
	BootstrapOptions map[string]*bool `json:"bootstrapOptions,omitempty"`
}

// SaveMultiProfile saves the full multi-profile configuration
func SaveMultiProfile(mpc *MultiProfileConfig) error {
	configFile, err := utils.GetConfigFile()
	if err != nil {
		return fmt.Errorf("failed to get config file path: %w", err)
	}

	// Ensure config directory exists
	configDir := filepath.Dir(configFile)
	if err := utils.EnsureDir(configDir); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Build backwards-compatible config with active profile at top level
	compat := backwardsCompatibleConfig{
		DefaultProfile:       mpc.DefaultProfile,
		Profiles:             mpc.Profiles,
		ForceEnabledClients:  mpc.ForceEnabledClients,
		ForceDisabledClients: mpc.ForceDisabledClients,
		BootstrapOptions:     mpc.BootstrapOptions,
		// Note: EnabledClients intentionally not saved (deprecated)
	}

	// Copy active profile fields to top level for old binaries
	activeProfileName := GetActiveProfileName(mpc)
	if activeProfile, ok := mpc.Profiles[activeProfileName]; ok {
		compat.Type = activeProfile.Type
		compat.ServerURL = activeProfile.ServerURL
		compat.AuthToken = activeProfile.AuthToken
		compat.RepositoryURL = activeProfile.RepositoryURL
	}

	// Marshal config to JSON with indentation
	data, err := json.MarshalIndent(compat, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Write to file with secure permissions
	if err := os.WriteFile(configFile, data, 0600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// GetActiveProfileName returns the name of the profile that should be used
func GetActiveProfileName(mpc *MultiProfileConfig) string {
	// Priority: override > env var > default profile
	if activeProfileOverride != "" {
		return activeProfileOverride
	}
	if envProfile := os.Getenv("SX_PROFILE"); envProfile != "" {
		return envProfile
	}
	if mpc.DefaultProfile != "" {
		return mpc.DefaultProfile
	}
	return DefaultProfileName
}

// ListProfiles returns a sorted list of profile names
func (mpc *MultiProfileConfig) ListProfiles() []string {
	names := make([]string, 0, len(mpc.Profiles))
	for name := range mpc.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// GetProfile returns a profile by name
func (mpc *MultiProfileConfig) GetProfile(name string) (*Profile, bool) {
	p, ok := mpc.Profiles[name]
	return p, ok
}

// SetProfile adds or updates a profile
func (mpc *MultiProfileConfig) SetProfile(name string, profile *Profile) {
	if mpc.Profiles == nil {
		mpc.Profiles = make(map[string]*Profile)
	}
	mpc.Profiles[name] = profile
}

// DeleteProfile removes a profile by name
func (mpc *MultiProfileConfig) DeleteProfile(name string) error {
	if _, ok := mpc.Profiles[name]; !ok {
		return fmt.Errorf("profile not found: %s", name)
	}
	delete(mpc.Profiles, name)

	// If we deleted the default profile, set a new default
	if mpc.DefaultProfile == name {
		names := mpc.ListProfiles()
		if len(names) > 0 {
			mpc.DefaultProfile = names[0]
		} else {
			mpc.DefaultProfile = ""
		}
	}
	return nil
}

// SetDefaultProfile sets the default profile
func (mpc *MultiProfileConfig) SetDefaultProfile(name string) error {
	if _, ok := mpc.Profiles[name]; !ok {
		return fmt.Errorf("profile not found: %s", name)
	}
	mpc.DefaultProfile = name
	return nil
}

// ToConfig converts a Profile to a Config with the given client settings
func (p *Profile) ToConfig(forceEnabled, forceDisabled []string) *Config {
	return &Config{
		Type:                 p.Type,
		ServerURL:            p.ServerURL,
		AuthToken:            p.AuthToken,
		RepositoryURL:        p.RepositoryURL,
		ForceEnabledClients:  forceEnabled,
		ForceDisabledClients: forceDisabled,
	}
}

// ProfileFromConfig creates a Profile from a Config
func ProfileFromConfig(cfg *Config) *Profile {
	return &Profile{
		Type:          cfg.Type,
		ServerURL:     cfg.ServerURL,
		AuthToken:     cfg.AuthToken,
		RepositoryURL: cfg.RepositoryURL,
	}
}

// MigrateEnabledClients migrates the deprecated EnabledClients field to the new
// ForceEnabledClients/ForceDisabledClients fields. Call this with all known client IDs.
// Returns true if migration was performed and config was saved.
func (mpc *MultiProfileConfig) MigrateEnabledClients(allClientIDs []string) (bool, error) {
	if len(mpc.EnabledClients) == 0 {
		return false, nil // Nothing to migrate
	}

	// Clients NOT in EnabledClients were disabled
	enabledSet := make(map[string]bool)
	for _, id := range mpc.EnabledClients {
		enabledSet[id] = true
	}

	for _, id := range allClientIDs {
		if !enabledSet[id] {
			mpc.ForceDisabledClients = append(mpc.ForceDisabledClients, id)
		}
	}

	// Clear deprecated field
	mpc.EnabledClients = nil

	// Save the migrated config
	if err := SaveMultiProfile(mpc); err != nil {
		return false, err
	}

	return true, nil
}

// NeedsMigration returns true if the config has deprecated EnabledClients field
func (mpc *MultiProfileConfig) NeedsMigration() bool {
	return len(mpc.EnabledClients) > 0
}
