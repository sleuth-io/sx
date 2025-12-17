package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

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

// getLegacyConfigFile returns the old config file path for backwards compatibility
func getLegacyConfigFile() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homeDir, ".claude", "plugins", "skills", "config.json"), nil
}

// Load loads the configuration from the config file
// Falls back to the old location (~/.claude/plugins/skills/config.json) for backwards compatibility
func Load() (*Config, error) {
	configFile, err := utils.GetConfigFile()
	if err != nil {
		return nil, fmt.Errorf("failed to get config file path: %w", err)
	}

	// Try new location first
	if utils.FileExists(configFile) {
		data, err := os.ReadFile(configFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}

		var cfg Config
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("failed to parse config file: %w", err)
		}

		return &cfg, nil
	}

	// Fallback to legacy location
	legacyConfigFile, err := getLegacyConfigFile()
	if err == nil && utils.FileExists(legacyConfigFile) {
		data, err := os.ReadFile(legacyConfigFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read legacy config file: %w", err)
		}

		var cfg Config
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("failed to parse legacy config file: %w", err)
		}

		return &cfg, nil
	}

	return nil, fmt.Errorf("configuration not found. Run 'sx init' first")
}

// Save saves the configuration to the config file
func Save(cfg *Config) error {
	configFile, err := utils.GetConfigFile()
	if err != nil {
		return fmt.Errorf("failed to get config file path: %w", err)
	}

	// Ensure config directory exists
	configDir := filepath.Dir(configFile)
	if err := utils.EnsureDir(configDir); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Marshal config to JSON with indentation
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Write to file with secure permissions
	if err := os.WriteFile(configFile, data, 0600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
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
			return fmt.Errorf("repositoryUrl is required for sleuth repository type")
		}
		if c.AuthToken == "" {
			return fmt.Errorf("authToken is required for sleuth repository type")
		}
	case RepositoryTypeGit:
		if c.RepositoryURL == "" {
			return fmt.Errorf("repositoryUrl is required for git repository type")
		}
	case RepositoryTypePath:
		if c.RepositoryURL == "" {
			return fmt.Errorf("repositoryUrl is required for path repository type")
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
	for _, id := range c.EnabledClients {
		if id == clientID {
			return true
		}
	}
	return false
}

// GetEnabledClients returns the list of enabled client IDs.
// Returns nil if not explicitly configured (meaning use all detected).
func (c *Config) GetEnabledClients() []string {
	return c.EnabledClients
}
