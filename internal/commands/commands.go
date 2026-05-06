package commands

import (
	"github.com/spf13/cobra"

	"github.com/sleuth-io/sx/internal/config"
)

// RunDefaultCommand runs `sx install` when the current directory is
// already configured for a vault, and otherwise shows help. "Configured"
// means the sx config file resolves — not that any lock file exists in
// the working directory, because in the current layout the per-user lock
// lives in the cache directory, not the project.
func RunDefaultCommand(cmd *cobra.Command, args []string) error {
	if _, err := config.Load(); err == nil {
		return runInstall(cmd, args, false, "", false, "", "", false, false)
	}
	return cmd.Help()
}
