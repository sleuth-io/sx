package commands

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/skills/internal/config"
	vaultpkg "github.com/sleuth-io/skills/internal/vault"
)

// NewUpdateTemplatesCommand creates the update-templates command (hidden)
func NewUpdateTemplatesCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "update-templates",
		Hidden: true,
		Short:  "Update templates in Git repository if needed",
		Long: `Check and update templates (install.sh, README.md) in the Git repository
if they are outdated or missing. Only updates files if their version is older
than the current template version. This is a hidden maintenance command.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUpdateTemplates(cmd, args)
		},
	}

	return cmd
}

func runUpdateTemplates(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	out := newOutputHelper(cmd)

	// Load config
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Only works with git vaults
	if cfg.Type != config.RepositoryTypeGit {
		return fmt.Errorf("update-templates only works with git vaults (current type: %s)", cfg.Type)
	}

	if cfg.RepositoryURL == "" {
		return fmt.Errorf("git vault URL not configured")
	}

	// Create vault instance
	vault, err := vaultpkg.NewGitVault(cfg.RepositoryURL)
	if err != nil {
		return fmt.Errorf("failed to create vault: %w", err)
	}

	// Update templates if needed (with auto-commit)
	// Note: For git vaults, this will update files, then commit and push
	updatedFiles, err := vault.UpdateTemplates(ctx, true)
	if err != nil {
		return fmt.Errorf("failed to update templates: %w", err)
	}

	if len(updatedFiles) == 0 {
		out.println("No templates needed updating")
	} else {
		out.println("✓ Templates updated successfully")
		out.println("Updated files:")
		for _, file := range updatedFiles {
			out.printf("  - %s\n", file)
		}
	}

	return nil
}
