package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

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

	// Identity is the email used to resolve team and user scopes for this
	// profile. When empty, sx falls back to `git config user.email`. Set
	// per-profile so a single machine can use different identities for
	// different vaults (e.g. work vs personal email).
	Identity string `json:"identity,omitempty"`
}

// MultiProfileConfig represents the full configuration file with multiple profiles
type MultiProfileConfig struct {
	// DefaultProfile is the profile that owns "write" actions when no
	// --profile flag is given (sx add, sx role, sx team ...). It is also
	// the conflict tiebreaker when multiple active profiles publish an
	// asset with the same name. Always a member of ActiveProfiles.
	DefaultProfile string `json:"defaultProfile"`

	// ActiveProfiles is the set of profiles considered "on" for read
	// operations (sx install, sx list). When more than one is active,
	// install merges applicable assets from every active profile's vault.
	// Order is precedence for conflict resolution when DefaultProfile
	// isn't in the active set.
	ActiveProfiles []string `json:"activeProfiles,omitempty"`

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
	// Backfill ActiveProfiles from DefaultProfile for configs written by
	// older sx versions that only tracked a single default.
	ensureActiveProfiles(&mpc)
	return &mpc, nil
}

// ensureActiveProfiles guarantees that ActiveProfiles is populated and
// consistent with the available profiles. When upgrading from a config
// written before ActiveProfiles existed, the default profile becomes the
// sole active profile.
func ensureActiveProfiles(mpc *MultiProfileConfig) {
	// Drop stale entries that point at deleted profiles.
	if len(mpc.ActiveProfiles) > 0 {
		filtered := mpc.ActiveProfiles[:0]
		seen := make(map[string]bool, len(mpc.ActiveProfiles))
		for _, name := range mpc.ActiveProfiles {
			if _, ok := mpc.Profiles[name]; !ok {
				continue
			}
			if seen[name] {
				continue
			}
			seen[name] = true
			filtered = append(filtered, name)
		}
		mpc.ActiveProfiles = filtered
	}
	// Legacy migration: no ActiveProfiles → default becomes the only active.
	if len(mpc.ActiveProfiles) == 0 {
		if mpc.DefaultProfile != "" {
			if _, ok := mpc.Profiles[mpc.DefaultProfile]; ok {
				mpc.ActiveProfiles = []string{mpc.DefaultProfile}
				return
			}
		}
		// Last resort: if DefaultProfile is missing, pick deterministic first.
		names := make([]string, 0, len(mpc.Profiles))
		for name := range mpc.Profiles {
			names = append(names, name)
		}
		sort.Strings(names)
		if len(names) > 0 {
			mpc.ActiveProfiles = []string{names[0]}
			if mpc.DefaultProfile == "" {
				mpc.DefaultProfile = names[0]
			}
		}
	}
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
		ActiveProfiles: []string{DefaultProfileName},
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
	ActiveProfiles []string            `json:"activeProfiles,omitempty"`
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
		ActiveProfiles:       mpc.ActiveProfiles,
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

// GetActiveProfileName returns the name of the profile that owns single-
// profile actions (mutations, --profile flag overrides, --profile=NAME).
// For the read-side set used by sx install, see GetActiveProfileNames.
func GetActiveProfileName(mpc *MultiProfileConfig) string {
	// Priority: override > env var > default profile
	if activeProfileOverride != "" {
		return firstName(activeProfileOverride)
	}
	if envProfile := os.Getenv("SX_PROFILE"); envProfile != "" {
		return firstName(envProfile)
	}
	if mpc.DefaultProfile != "" {
		return mpc.DefaultProfile
	}
	return DefaultProfileName
}

// GetActiveProfileNames returns every profile that should be loaded by
// read-side commands (sx install, sx list). When --profile or SX_PROFILE
// is set, the override list wins and its order is preserved (treating
// the user's explicit input as authoritative precedence). Otherwise
// ActiveProfiles is returned, with the default profile bubbled to the
// front so conflict resolution favors it.
func GetActiveProfileNames(mpc *MultiProfileConfig) []string {
	var raw []string
	explicitOverride := false
	switch {
	case activeProfileOverride != "":
		raw = splitNames(activeProfileOverride)
		explicitOverride = true
	case os.Getenv("SX_PROFILE") != "":
		raw = splitNames(os.Getenv("SX_PROFILE"))
		explicitOverride = true
	case len(mpc.ActiveProfiles) > 0:
		raw = append(raw, mpc.ActiveProfiles...)
	case mpc.DefaultProfile != "":
		raw = []string{mpc.DefaultProfile}
	default:
		raw = []string{DefaultProfileName}
	}

	seen := make(map[string]bool, len(raw))
	out := make([]string, 0, len(raw))
	for _, name := range raw {
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	// Bubble the default profile to the front for the persisted active
	// set; explicit user overrides keep their input order.
	if !explicitOverride && mpc.DefaultProfile != "" && seen[mpc.DefaultProfile] && len(out) > 1 && out[0] != mpc.DefaultProfile {
		ordered := make([]string, 0, len(out))
		ordered = append(ordered, mpc.DefaultProfile)
		for _, name := range out {
			if name != mpc.DefaultProfile {
				ordered = append(ordered, name)
			}
		}
		out = ordered
	}
	return out
}

// firstName returns the first comma-separated entry, trimmed.
func firstName(s string) string {
	parts := splitNames(s)
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

// splitNames parses a comma-separated profile list, trimming whitespace
// and skipping empties.
func splitNames(s string) []string {
	parts := make([]string, 0)
	for raw := range strings.SplitSeq(s, ",") {
		if trimmed := strings.TrimSpace(raw); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	return parts
}

// IsProfileActive reports whether the named profile is in ActiveProfiles.
func (mpc *MultiProfileConfig) IsProfileActive(name string) bool {
	return slices.Contains(mpc.ActiveProfiles, name)
}

// Activate adds name to ActiveProfiles. Returns an error if the profile
// does not exist. A no-op when already active.
func (mpc *MultiProfileConfig) Activate(name string) error {
	if _, ok := mpc.Profiles[name]; !ok {
		return fmt.Errorf("profile not found: %s", name)
	}
	if mpc.IsProfileActive(name) {
		return nil
	}
	mpc.ActiveProfiles = append(mpc.ActiveProfiles, name)
	return nil
}

// Deactivate removes name from ActiveProfiles. Refuses to remove the
// last active profile. If the deactivated profile was also the default,
// the first remaining active profile takes over as the new default so
// the invariant "DefaultProfile is always active" still holds.
func (mpc *MultiProfileConfig) Deactivate(name string) error {
	if !mpc.IsProfileActive(name) {
		return fmt.Errorf("profile not active: %s", name)
	}
	if len(mpc.ActiveProfiles) <= 1 {
		return errors.New("cannot deactivate the last active profile")
	}
	filtered := mpc.ActiveProfiles[:0]
	for _, n := range mpc.ActiveProfiles {
		if n != name {
			filtered = append(filtered, n)
		}
	}
	mpc.ActiveProfiles = filtered
	if mpc.DefaultProfile == name {
		mpc.DefaultProfile = mpc.ActiveProfiles[0]
	}
	return nil
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

	// Drop from active set, refusing to leave the set empty.
	filtered := mpc.ActiveProfiles[:0]
	for _, n := range mpc.ActiveProfiles {
		if n != name {
			filtered = append(filtered, n)
		}
	}
	mpc.ActiveProfiles = filtered

	// If we deleted the default profile, pick a new default.
	if mpc.DefaultProfile == name {
		names := mpc.ListProfiles()
		if len(names) > 0 {
			mpc.DefaultProfile = names[0]
		} else {
			mpc.DefaultProfile = ""
		}
	}

	// Ensure ActiveProfiles is non-empty when profiles remain.
	if len(mpc.ActiveProfiles) == 0 && mpc.DefaultProfile != "" {
		mpc.ActiveProfiles = []string{mpc.DefaultProfile}
	}
	return nil
}

// SetDefaultProfile sets the default profile, activating it if needed.
func (mpc *MultiProfileConfig) SetDefaultProfile(name string) error {
	if _, ok := mpc.Profiles[name]; !ok {
		return fmt.Errorf("profile not found: %s", name)
	}
	mpc.DefaultProfile = name
	if !mpc.IsProfileActive(name) {
		mpc.ActiveProfiles = append(mpc.ActiveProfiles, name)
	}
	return nil
}

// ToConfig converts a Profile to a Config with the given client settings
func (p *Profile) ToConfig(forceEnabled, forceDisabled []string) *Config {
	return &Config{
		Type:                 p.Type,
		ServerURL:            p.ServerURL,
		AuthToken:            p.AuthToken,
		RepositoryURL:        p.RepositoryURL,
		Identity:             p.Identity,
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
		Identity:      cfg.Identity,
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
