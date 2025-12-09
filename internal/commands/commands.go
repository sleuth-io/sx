package commands

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/skills/internal/constants"
)

// RunDefaultCommand runs the default command (install if lock file exists)
func RunDefaultCommand(cmd *cobra.Command, args []string) error {
	out := newOutputHelper(cmd)

	// Check if skill.lock exists in current directory
	if _, err := os.Stat(constants.SkillLockFile); err == nil {
		// Lock file exists, run install (not in hook mode)
		return runInstall(cmd, args, false)
	}

	// No lock file, show help
	out.printfErr("No %s file found in current directory.", constants.SkillLockFile)
	out.printErr("")
	out.printErr("To get started:")
	out.printErr("  1. Run 'skills init' to configure a repository")
	out.printErr("  2. Run 'skills install' to install artifacts from the lock file")
	out.printErr("")
	return cmd.Help()
}
