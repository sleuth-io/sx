package commands

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/skills/internal/config"
	"github.com/sleuth-io/skills/internal/constants"
	"github.com/sleuth-io/skills/internal/lockfile"
	"github.com/sleuth-io/skills/internal/repository"
	"github.com/sleuth-io/skills/internal/requirements"
	"github.com/sleuth-io/skills/internal/resolver"
)

// NewLockCommand creates the lock command
func NewLockCommand() *cobra.Command {
	var requirementsFile string
	var outputFile string

	cmd := &cobra.Command{
		Use:   "lock",
		Short: "Generate lock file from requirements file",
		Long: `Generate a skill.lock file from a skill.txt requirements file.

This command reads the requirements file, resolves artifact versions and dependencies,
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

	out.println("Generating lock file from requirements...")
	out.println()

	// Check if requirements file exists
	if _, err := os.Stat(requirementsFile); err != nil {
		return fmt.Errorf("requirements file not found: %s", requirementsFile)
	}

	// Parse requirements file
	out.printf("Reading requirements from %s...\n", requirementsFile)
	reqs, err := requirements.Parse(requirementsFile)
	if err != nil {
		return fmt.Errorf("failed to parse requirements: %w", err)
	}

	out.printf("Found %d requirements\n", len(reqs))
	for _, req := range reqs {
		out.printf("  - %s\n", req.String())
	}
	out.println()

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w\nRun 'skills init' to configure", err)
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// Create repository instance
	repo, err := repository.NewFromConfig(cfg)
	if err != nil {
		return fmt.Errorf("failed to create repository: %w", err)
	}

	// Resolve requirements
	out.println("Resolving artifacts and dependencies...")
	resolverInstance := resolver.New(ctx, repo)
	lockFile, err := resolverInstance.Resolve(reqs)
	if err != nil {
		return fmt.Errorf("failed to resolve requirements: %w", err)
	}

	out.printf("Resolved %d artifacts (including dependencies)\n", len(lockFile.Artifacts))
	out.println()

	// Display resolved artifacts
	out.println("Resolved artifacts:")
	for _, artifact := range lockFile.Artifacts {
		out.printf("  - %s@%s (%s) [%s]\n", artifact.Name, artifact.Version, artifact.Type, artifact.GetSourceType())
	}
	out.println()

	// Write lock file
	out.printf("Writing lock file to %s...\n", outputFile)
	if err := lockfile.Write(lockFile, outputFile); err != nil {
		return fmt.Errorf("failed to write lock file: %w", err)
	}

	out.println()
	out.printf("✓ Lock file generated successfully: %s\n", outputFile)
	out.printf("  Lock version: %s\n", lockFile.LockVersion)
	out.printf("  Created by: %s\n", lockFile.CreatedBy)
	out.printf("  Version: %s\n", lockFile.Version)

	return nil
}
