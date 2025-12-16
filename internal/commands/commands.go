package commands

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/skills/internal/constants"
)

// RunDefaultCommand runs the default command (install if lock file exists)
func RunDefaultCommand(cmd *cobra.Command, args []string) error {
	out := newOutputHelper(cmd)

	// Check if sx.lock exists in current directory
	if _, err := os.Stat(constants.SkillLockFile); err == nil {
		// Lock file exists, run install (not in hook mode, no specific client)
		return runInstall(cmd, args, false, "", false)
	}

	// No lock file, show help
	out.printfErr("No %s file found in current directory.", constants.SkillLockFile)
	out.printErr("")
	out.printErr("To get started:")
	out.printErr("  1. Run 'sx init' to configure a vault")
	out.printErr("  2. Run 'sx install' to install assets from the lock file")
	out.printErr("")
	return cmd.Help()
}
