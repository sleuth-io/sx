package vault

import (
	"fmt"
	"strings"

	"github.com/sleuth-io/sx/internal/git"
)

// Config represents the minimal configuration needed to create a vault
// This avoids circular dependency with the config package
type Config interface {
	GetType() string
	GetServerURL() string
	GetAuthToken() string
	GetRepositoryURL() string
}

type authUsernameConfig interface {
	GetAuthUsername() string
}

// NewFromConfig creates a vault instance from configuration
// This factory function eliminates repetitive switch statements across commands
func NewFromConfig(cfg Config) (Vault, error) {
	switch cfg.GetType() {
	case "sleuth":
		return NewSleuthVault(cfg.GetServerURL(), cfg.GetAuthToken()), nil
	case "git":
		opts := []GitVaultOption(nil)
		if tok := strings.TrimSpace(cfg.GetAuthToken()); tok != "" {
			info := git.ParseRemoteAuthInfo(cfg.GetRepositoryURL())
			if info.HTTP {
				opts = append(opts, WithGitClient(git.NewClientWithOptions(git.WithHTTPBasicAuth(
					info.Scheme,
					info.Host,
					git.DefaultHTTPSAuthUsername(info.Host, authUsername(cfg)),
					tok,
				))))
			}
		}
		return NewGitVaultWithOptions(cfg.GetRepositoryURL(), opts...)
	case "path":
		return NewPathVault(cfg.GetRepositoryURL())
	default:
		return nil, fmt.Errorf("unsupported vault type: %s", cfg.GetType())
	}
}

func authUsername(cfg Config) string {
	if cfg, ok := cfg.(authUsernameConfig); ok {
		return cfg.GetAuthUsername()
	}
	return ""
}
