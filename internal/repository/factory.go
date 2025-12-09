package repository

import (
	"fmt"
)

// Config represents the minimal configuration needed to create a repository
// This avoids circular dependency with the config package
type Config interface {
	GetType() string
	GetServerURL() string
	GetAuthToken() string
	GetRepositoryURL() string
}

// NewFromConfig creates a repository instance from configuration
// This factory function eliminates repetitive switch statements across commands
func NewFromConfig(cfg Config) (Repository, error) {
	switch cfg.GetType() {
	case "sleuth":
		return NewSleuthRepository(cfg.GetServerURL(), cfg.GetAuthToken()), nil
	case "git":
		return NewGitRepository(cfg.GetRepositoryURL())
	case "path":
		return NewPathRepository(cfg.GetRepositoryURL())
	default:
		return nil, fmt.Errorf("unsupported repository type: %s", cfg.GetType())
	}
}
