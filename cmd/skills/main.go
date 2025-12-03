package main

import (
	"fmt"
	"os"

	"github.com/sleuth-io/skills/internal/buildinfo"
	"github.com/sleuth-io/skills/internal/commands"
	"github.com/spf13/cobra"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "skills",
		Short: "Skills CLI - Provision AI artifacts from remote servers or Git repositories",
		Long: `Skills is a CLI tool that provisions AI artifacts (skills, agents, MCPs, etc.)
from remote Sleuth servers or Git repositories.`,
		Version: fmt.Sprintf("%s (commit: %s, built: %s)", buildinfo.Version, buildinfo.Commit, buildinfo.Date),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Default command: run install if lock file exists
			return commands.RunDefaultCommand(cmd, args)
		},
		SilenceUsage: true,
	}

	// Add subcommands
	rootCmd.AddCommand(commands.NewInitCommand())
	rootCmd.AddCommand(commands.NewInstallCommand())
	rootCmd.AddCommand(commands.NewLockCommand())
	rootCmd.AddCommand(commands.NewAddCommand())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
