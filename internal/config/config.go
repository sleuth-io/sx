package config

import (
	"errors"
	"fmt"
	"os"
	"slices"

	"github.com/sleuth-io/sx/internal/utils"
)

// RepositoryType represents the type of repository (sleuth, git, or path)
type RepositoryType string

const (
	RepositoryTypeSleuth RepositoryType = "sleuth"
	RepositoryTypeGit    RepositoryType = "git"
	RepositoryTypePath   RepositoryType = "path"
)

// Config represents the configuration for the skills CLI
type Config struct {
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

	// EnabledClients is the list of client IDs that assets should be installed to.
	// An empty/nil slice means "all detected clients" (backwards compatible default).
	EnabledClients []string `json:"enabledClients,omitempty"`
}

// Load loads the configuration from the config file
// Uses the multi-profile system and returns the active profile as a Config
func Load() (*Config, error) {
	mpc, err := LoadMultiProfile()
	if err != nil {
		return nil, err
	}

	profileName := GetActiveProfileName(mpc)
	profile, ok := mpc.GetProfile(profileName)
	if !ok {
		// If the requested profile doesn't exist, try the default
		if profileName != mpc.DefaultProfile && mpc.DefaultProfile != "" {
			profile, ok = mpc.GetProfile(mpc.DefaultProfile)
		}
		if !ok {
			return nil, fmt.Errorf("profile not found: %s", profileName)
		}
	}

	return profile.ToConfig(mpc.EnabledClients), nil
}

// Save saves the configuration to the config file
// This updates the active profile while preserving other profiles
func Save(cfg *Config) error {
	return SaveToProfile(cfg, "")
}

// SaveToProfile saves the configuration to a specific profile
// If profileName is empty, uses the active profile
func SaveToProfile(cfg *Config, profileName string) error {
	// Try to load existing multi-profile config
	mpc, err := LoadMultiProfile()
	if err != nil {
		// No existing config, create new multi-profile config
		mpc = &MultiProfileConfig{
			DefaultProfile: DefaultProfileName,
			Profiles:       make(map[string]*Profile),
		}
	}

	// Determine which profile to save to
	if profileName == "" {
		profileName = GetActiveProfileName(mpc)
	}

	// Update the profile
	mpc.SetProfile(profileName, ProfileFromConfig(cfg))

	// Update enabled clients (global setting)
	mpc.EnabledClients = cfg.EnabledClients

	// If this is the first profile, make it the default
	if mpc.DefaultProfile == "" {
		mpc.DefaultProfile = profileName
	}

	return SaveMultiProfile(mpc)
}

// Exists checks if a configuration file exists
func Exists() bool {
	configFile, err := utils.GetConfigFile()
	if err != nil {
		return false
	}
	return utils.FileExists(configFile)
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.Type != RepositoryTypeSleuth && c.Type != RepositoryTypeGit && c.Type != RepositoryTypePath {
		return fmt.Errorf("invalid repository type: %s (must be 'sleuth', 'git', or 'path')", c.Type)
	}

	switch c.Type {
	case RepositoryTypeSleuth:
		if c.RepositoryURL == "" && c.ServerURL == "" {
			return errors.New("repositoryUrl is required for sleuth repository type")
		}
		if c.AuthToken == "" {
			return errors.New("authToken is required for sleuth repository type")
		}
	case RepositoryTypeGit:
		if c.RepositoryURL == "" {
			return errors.New("repositoryUrl is required for git repository type")
		}
	case RepositoryTypePath:
		if c.RepositoryURL == "" {
			return errors.New("repositoryUrl is required for path repository type")
		}
	}

	return nil
}

// GetType returns the repository type
func (c *Config) GetType() string {
	return string(c.Type)
}

// GetServerURL returns the Sleuth server URL, with environment override
// For backwards compatibility, falls back to ServerURL if RepositoryURL is empty
func (c *Config) GetServerURL() string {
	if envURL := os.Getenv("SLEUTH_SERVER_URL"); envURL != "" {
		return envURL
	}
	if c.RepositoryURL != "" {
		return c.RepositoryURL
	}
	return c.ServerURL
}

// GetAuthToken returns the auth token
func (c *Config) GetAuthToken() string {
	return c.AuthToken
}

// GetRepositoryURL returns the repository URL
func (c *Config) GetRepositoryURL() string {
	return c.RepositoryURL
}

// IsSilent checks if silent mode is enabled via environment variable
func IsSilent() bool {
	return os.Getenv("SX_SYNC_SILENT") == "true" || os.Getenv("SKILLS_SYNC_SILENT") == "true"
}

// IsClientEnabled checks if a specific client ID is enabled.
// If EnabledClients is empty/nil, all clients are considered enabled (backwards compatible).
func (c *Config) IsClientEnabled(clientID string) bool {
	if len(c.EnabledClients) == 0 {
		return true
	}
	return slices.Contains(c.EnabledClients, clientID)
}

// GetEnabledClients returns the list of enabled client IDs.
// Returns nil if not explicitly configured (meaning use all detected).
func (c *Config) GetEnabledClients() []string {
	return c.EnabledClients
}
