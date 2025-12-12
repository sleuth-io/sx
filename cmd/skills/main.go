package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/sleuth-io/skills/internal/autoupdate"
	"github.com/sleuth-io/skills/internal/buildinfo"
	"github.com/sleuth-io/skills/internal/clients"
	"github.com/sleuth-io/skills/internal/clients/claude_code"
	"github.com/sleuth-io/skills/internal/clients/cursor"
	"github.com/sleuth-io/skills/internal/commands"
	"github.com/sleuth-io/skills/internal/git"
	"github.com/sleuth-io/skills/internal/logger"
	"github.com/spf13/cobra"
)

func init() {
	// Register all clients
	clients.Register(claude_code.NewClient())
	clients.Register(cursor.NewClient()) // TODO: Uncomment after thorough testing
}

func main() {
	// Log command invocation with context
	log := logger.Get()
	cwd, _ := os.Getwd()

	// Extract --client flag if present (for hook mode context)
	client := ""
	for i, arg := range os.Args {
		if strings.HasPrefix(arg, "--client=") {
			client = strings.TrimPrefix(arg, "--client=")
			break
		}
		if arg == "--client" && i+1 < len(os.Args) {
			client = os.Args[i+1]
			break
		}
	}

	logArgs := []any{"version", buildinfo.Version, "command", strings.Join(os.Args[1:], " "), "cwd", cwd}
	if client != "" {
		logArgs = append(logArgs, "client", client)
	}
	log.Info("command invoked", logArgs...)

	// Check for updates in the background (non-blocking, once per day)
	// Skip if user is explicitly running the update command
	if len(os.Args) < 2 || os.Args[1] != "update" {
		autoupdate.CheckAndUpdateInBackground()
	}

	rootCmd := &cobra.Command{
		Use:   "skills",
		Short: "Skills CLI - Provision AI artifacts from remote servers or Git repositories",
		Long: `Skills is a CLI tool that provisions AI artifacts (skills, agents, MCPs, etc.)
from remote Sleuth servers or Git repositories.`,
		Version: fmt.Sprintf("%s (commit: %s, built: %s)", buildinfo.Version, buildinfo.Commit, buildinfo.Date),
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			// Initialize SSH key path from flag or environment variable
			git.SetSSHKeyPath(cmd)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			// Default command: run install if lock file exists
			return commands.RunDefaultCommand(cmd, args)
		},
		SilenceUsage: true,
		CompletionOptions: cobra.CompletionOptions{
			DisableDefaultCmd: true,
		},
	}

	// Add global flags
	rootCmd.PersistentFlags().String("ssh-key", "",
		"Path to SSH private key file or key content for git operations (can also use SKILLS_SSH_KEY environment variable)")

	// Add subcommands
	rootCmd.AddCommand(commands.NewInitCommand())
	rootCmd.AddCommand(commands.NewInstallCommand())
	rootCmd.AddCommand(commands.NewUninstallCommand())
	rootCmd.AddCommand(commands.NewLockCommand())
	rootCmd.AddCommand(commands.NewAddCommand())
	rootCmd.AddCommand(commands.NewUpdateTemplatesCommand())
	rootCmd.AddCommand(commands.NewUpdateCommand())
	rootCmd.AddCommand(commands.NewReportUsageCommand())
	rootCmd.AddCommand(commands.NewServeCommand())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
