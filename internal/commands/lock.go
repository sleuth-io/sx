package commands

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/sx/internal/config"
	"github.com/sleuth-io/sx/internal/constants"
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/requirements"
	"github.com/sleuth-io/sx/internal/resolver"
	"github.com/sleuth-io/sx/internal/ui/components"
	vaultpkg "github.com/sleuth-io/sx/internal/vault"
)

// NewLockCommand creates the lock command
func NewLockCommand() *cobra.Command {
	var requirementsFile string
	var outputFile string

	cmd := &cobra.Command{
		Use:   "lock",
		Short: "Generate lock file from requirements file",
		Long: `Generate a sx.lock file from a sx.txt requirements file.

This command reads the requirements file, resolves asset versions and dependencies,
and generates a lock file with exact versions, hashes, and full dependency graph.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLock(cmd, args, requirementsFile, outputFile)
		},
	}

	cmd.Flags().StringVarP(&requirementsFile, "requirements", "r", constants.SkillRequirementsFile, "Requirements file to read")
	cmd.Flags().StringVarP(&outputFile, "output", "o", constants.SkillLockFile, "Output lock file path")

	return cmd
}

// runLock executes the lock command
func runLock(cmd *cobra.Command, args []string, requirementsFile, outputFile string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	out := newOutputHelper(cmd)
	status := components.NewStatus(cmd.OutOrStdout())

	// Check if requirements file exists
	if _, err := os.Stat(requirementsFile); err != nil {
		return fmt.Errorf("requirements file not found: %s", requirementsFile)
	}

	// Parse requirements file
	status.Start("Reading requirements")
	reqs, err := requirements.Parse(requirementsFile)
	if err != nil {
		status.Fail("Failed to parse requirements")
		return fmt.Errorf("failed to parse requirements: %w", err)
	}
	status.Done("")

	out.printf("Found %d requirements\n", len(reqs))
	for _, req := range reqs {
		out.printf("  - %s\n", req.String())
	}
	out.println()

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w\nRun 'sx init' to configure", err)
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// Create vault instance
	vault, err := vaultpkg.NewFromConfig(cfg)
	if err != nil {
		return fmt.Errorf("failed to create vault: %w", err)
	}

	// Resolve requirements
	status.Start("Resolving assets and dependencies")
	resolverInstance := resolver.New(ctx, vault)
	lockFile, err := resolverInstance.Resolve(reqs)
	if err != nil {
		status.Fail("Failed to resolve")
		return fmt.Errorf("failed to resolve requirements: %w", err)
	}
	status.Done("")

	out.printf("Resolved %d assets (including dependencies)\n", len(lockFile.Assets))
	out.println()

	// Display resolved assets
	out.println("Resolved assets:")
	for _, asset := range lockFile.Assets {
		out.printf("  - %s@%s (%s) [%s]\n", asset.Name, asset.Version, asset.Type, asset.GetSourceType())
	}
	out.println()

	// Write lock file
	status.Start("Writing lock file")
	if err := lockfile.Write(lockFile, outputFile); err != nil {
		status.Fail("Failed to write lock file")
		return fmt.Errorf("failed to write lock file: %w", err)
	}
	status.Done("")

	out.println()
	out.printf("✓ Lock file generated successfully: %s\n", outputFile)
	out.printf("  Lock version: %s\n", lockFile.LockVersion)
	out.printf("  Created by: %s\n", lockFile.CreatedBy)
	out.printf("  Version: %s\n", lockFile.Version)

	return nil
}
