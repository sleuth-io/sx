package commands

import (
	"context"
	"fmt"
	"time"

	"github.com/creativeprojects/go-selfupdate"
	"github.com/spf13/cobra"

	"github.com/sleuth-io/sx/internal/buildinfo"
	"github.com/sleuth-io/sx/internal/ui/components"
)

const (
	githubOwner = "sleuth-io"
	githubRepo  = "skills"
)

// NewUpdateCommand creates the update command
func NewUpdateCommand() *cobra.Command {
	var checkOnly bool

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update sx to the latest version",
		Long: `Check for and install updates to the sx CLI tool.

By default, will check for updates and prompt before installing.
Use --check to only check for updates without installing.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUpdate(cmd, checkOnly)
		},
	}

	cmd.Flags().BoolVar(&checkOnly, "check", false, "Only check for updates without installing")

	return cmd
}

// runUpdate executes the update command
func runUpdate(cmd *cobra.Command, checkOnly bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	out := newOutputHelper(cmd)
	status := components.NewStatus(cmd.OutOrStdout())

	// Get current version
	currentVersion := buildinfo.Version
	if currentVersion == "dev" || currentVersion == "" {
		out.printErr("Cannot update development builds. Please install from a release.")
		return nil
	}

	out.printf("Current version: %s\n", buildinfo.Version)

	repository := selfupdate.ParseSlug(fmt.Sprintf("%s/%s", githubOwner, githubRepo))

	if checkOnly {
		// Just check for latest version without updating
		status.Start("Checking for updates")
		latest, found, err := selfupdate.DetectLatest(ctx, repository)
		if err != nil {
			status.Fail("Failed to check")
			return fmt.Errorf("failed to check for updates: %w", err)
		}
		status.Done("")

		if !found {
			out.printErr("No releases found")
			return nil
		}

		// Compare versions using the library's methods
		if latest.LessOrEqual(currentVersion) {
			out.printf("You are already using the latest version (%s)\n", buildinfo.Version)
			return nil
		}

		out.printf("New version available: %s\n", latest.Version())
		out.printf("\nRun 'sx update' to install the new version\n")
		return nil
	}

	// Check if there's actually a newer version available
	status.Start("Checking for updates")
	latest, found, err := selfupdate.DetectLatest(ctx, repository)
	if err != nil {
		status.Fail("Failed to check")
		return fmt.Errorf("failed to check for updates: %w", err)
	}
	status.Done("")

	if !found {
		out.printErr("No releases found")
		return nil
	}

	// Compare versions - if we're already at or ahead of latest, nothing to do
	if latest.LessOrEqual(currentVersion) {
		out.printf("You are already using the latest version (%s)\n", buildinfo.Version)
		return nil
	}

	// Perform the update using the library's high-level function
	status.Start("Downloading and installing update")
	release, err := selfupdate.UpdateSelf(ctx, currentVersion, repository)
	if err != nil {
		status.Fail("Failed to update")
		return fmt.Errorf("failed to update: %w", err)
	}
	status.Done("")

	out.printf("\nSuccessfully updated to %s!\n", release.Version())
	out.printf("The new version is ready to use.\n")

	return nil
}
