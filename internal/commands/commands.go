package commands

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/sx/internal/constants"
)

// RunDefaultCommand runs the default command (install if lock file exists)
func RunDefaultCommand(cmd *cobra.Command, args []string) error {
	// Check if sx.lock exists in current directory
	if _, err := os.Stat(constants.SkillLockFile); err == nil {
		// Lock file exists, run install (not in hook mode, no specific client)
		return runInstall(cmd, args, false, "", false, "")
	}

	// No lock file, just show help
	return cmd.Help()
}
