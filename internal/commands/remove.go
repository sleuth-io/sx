package commands

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/sx/internal/config"
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/ui/components"
	vaultpkg "github.com/sleuth-io/sx/internal/vault"
)

// NewRemoveCommand creates the remove command
func NewRemoveCommand() *cobra.Command {
	var yes bool
	var versionFlag string

	cmd := &cobra.Command{
		Use:   "remove <asset-name>",
		Short: "Remove an asset from the lock file",
		Long: `Remove an asset from the lock file. The asset remains in the vault/repository
and can be re-added later.

Examples:
  sx remove my-skill              # Remove my-skill from the lock file
  sx remove my-skill -v 1.0.0     # Remove specific version
  sx remove my-skill --yes        # Remove and run install without prompts`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRemove(cmd, args[0], versionFlag, yes)
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip prompts and automatically run install")
	cmd.Flags().StringVarP(&versionFlag, "version", "v", "", "Version to remove (defaults to highest version in lock file)")

	return cmd
}

// runRemove executes the remove command
func runRemove(cmd *cobra.Command, assetName, versionFlag string, yes bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	out := newOutputHelper(cmd)
	status := components.NewStatus(cmd.OutOrStdout())

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w\nRun 'sx init' to configure", err)
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// Create vault
	vault, err := vaultpkg.NewFromConfig(cfg)
	if err != nil {
		return fmt.Errorf("failed to create vault: %w", err)
	}

	// Get all versions from the lock file
	status.Start("Loading lock file")
	lockFileData, _, _, err := vault.GetLockFile(ctx, "")
	if err != nil {
		status.Fail("Failed to get lock file")
		return fmt.Errorf("failed to get lock file: %w", err)
	}

	lf, err := lockfile.Parse(lockFileData)
	if err != nil {
		status.Fail("Failed to parse lock file")
		return fmt.Errorf("failed to parse lock file: %w", err)
	}
	status.Clear()

	// Collect all versions of this asset
	var versions []string
	for _, asset := range lf.Assets {
		if asset.Name == assetName {
			versions = append(versions, asset.Version)
		}
	}

	if len(versions) == 0 {
		return fmt.Errorf("asset %q not found in lock file", assetName)
	}

	// If version specified, only remove that version
	// Otherwise, remove all versions
	versionsToRemove := versions
	if versionFlag != "" {
		versionsToRemove = []string{versionFlag}
	}

	// Remove from vault (includes git operations for GitVault)
	_, isGitVault := vault.(*vaultpkg.GitVault)

	for _, assetVersion := range versionsToRemove {
		if isGitVault {
			status.Start(fmt.Sprintf("Removing %s@%s from lock file and pushing", assetName, assetVersion))
		} else {
			status.Start(fmt.Sprintf("Removing %s@%s from lock file", assetName, assetVersion))
		}

		if err := vault.RemoveAsset(ctx, assetName, assetVersion); err != nil {
			status.Fail("Failed to remove asset")
			return fmt.Errorf("failed to remove asset: %w", err)
		}

		status.Done(fmt.Sprintf("Removed %s@%s", assetName, assetVersion))
	}

	// Prompt to run install (or auto-run if --yes)
	shouldInstall := yes
	if !yes {
		out.println()
		confirmed, err := components.ConfirmWithIO("Run install now to remove the asset from clients?", true, cmd.InOrStdin(), cmd.OutOrStdout())
		if err != nil {
			return nil
		}
		shouldInstall = confirmed
	}

	if shouldInstall {
		out.println()
		if err := runInstall(cmd, nil, false, "", false); err != nil {
			out.printfErr("Install failed: %v\n", err)
		}
	} else {
		out.println("Run 'sx install' when ready to remove the asset from clients.")
	}

	return nil
}
