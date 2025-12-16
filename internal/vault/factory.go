package vault

import (
	"fmt"
)

// Config represents the minimal configuration needed to create a vault
// This avoids circular dependency with the config package
type Config interface {
	GetType() string
	GetServerURL() string
	GetAuthToken() string
	GetRepositoryURL() string
}

// NewFromConfig creates a vault instance from configuration
// This factory function eliminates repetitive switch statements across commands
func NewFromConfig(cfg Config) (Vault, error) {
	switch cfg.GetType() {
	case "sleuth":
		return NewSleuthVault(cfg.GetServerURL(), cfg.GetAuthToken()), nil
	case "git":
		return NewGitVault(cfg.GetRepositoryURL())
	case "path":
		return NewPathVault(cfg.GetRepositoryURL())
	default:
		return nil, fmt.Errorf("unsupported vault type: %s", cfg.GetType())
	}
}
