package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/sx/internal/autoupdate"
	"github.com/sleuth-io/sx/internal/buildinfo"
	_ "github.com/sleuth-io/sx/internal/clients/claude_code"    // Register Claude Code client
	_ "github.com/sleuth-io/sx/internal/clients/codex"          // Register Codex client
	_ "github.com/sleuth-io/sx/internal/clients/cursor"         // Register Cursor client
	_ "github.com/sleuth-io/sx/internal/clients/gemini"         // Register Gemini Code Assist client
	_ "github.com/sleuth-io/sx/internal/clients/github_copilot" // Register GitHub Copilot client
	"github.com/sleuth-io/sx/internal/commands"
	"github.com/sleuth-io/sx/internal/config"
	"github.com/sleuth-io/sx/internal/git"
	"github.com/sleuth-io/sx/internal/logger"
	"github.com/sleuth-io/sx/internal/ui"
	"github.com/sleuth-io/sx/internal/ui/theme"
)

func joinStrings(items []string, sep string) string {
	return strings.Join(items, sep)
}

func main() {
	// Log command invocation with context
	log := logger.Get()
	cwd, _ := os.Getwd()

	// Extract --client flag if present (for hook mode context)
	client := ""
	for i, arg := range os.Args {
		if after, ok := strings.CutPrefix(arg, "--client="); ok {
			client = after
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
		Use:   "sx",
		Short: "Your team's private npm for AI assets",
		Long: `sx is your team's private npm for AI assets - skills, MCP configs, commands, and more.
Capture what your best AI users have learned and spread it to everyone automatically.`,
		Version: fmt.Sprintf("%s (commit: %s, built: %s)", buildinfo.Version, buildinfo.Commit, buildinfo.Date),
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			// Initialize SSH key path from flag or environment variable
			git.SetSSHKeyPath(cmd)

			// Set active profile from flag (env var is handled in config package)
			if profile, _ := cmd.Flags().GetString("profile"); profile != "" {
				config.SetActiveProfile(profile)
			}
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			// Default command: run install if lock file exists
			return commands.RunDefaultCommand(cmd, args)
		},
		SilenceUsage:  true,
		SilenceErrors: true, // We handle error output ourselves with styling
		CompletionOptions: cobra.CompletionOptions{
			DisableDefaultCmd: true,
		},
	}

	// Set colored version and help templates
	styles := theme.Current().Styles()
	rootCmd.SetVersionTemplate(
		styles.Bold.Render("sx") + " " +
			styles.Emphasis.Render(buildinfo.Version) + " " +
			styles.Muted.Render("(commit: "+buildinfo.Commit+", built: "+buildinfo.Date+")") + "\n",
	)

	// Set colored help template
	cobra.AddTemplateFunc("bold", styles.Bold.Render)
	cobra.AddTemplateFunc("emphasis", styles.Emphasis.Render)
	cobra.AddTemplateFunc("muted", styles.Muted.Render)
	cobra.AddTemplateFunc("header", styles.Header.Render)
	cobra.AddTemplateFunc("join", joinStrings)

	rootCmd.SetHelpTemplate(`{{if .Long}}{{.Long}}{{else}}{{.Short}}{{end}}{{if .Version}}
{{muted .Version}}{{end}}

{{header "Usage:"}}
  {{emphasis .UseLine}}{{if .HasAvailableSubCommands}}
  {{emphasis (printf "%s [command]" .CommandPath)}}{{end}}{{if gt (len .Aliases) 0}}

{{header "Aliases:"}}
  {{muted (join .Aliases ", ")}}{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Groups) 0}}

{{header "Available Commands:"}}{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{emphasis (rpad .Name .NamePadding)}} {{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{header .Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}
  {{emphasis (rpad .Name .NamePadding)}} {{.Short}}{{end}}{{end}}{{end}}{{if not .AllChildCommandsHaveGroup}}

{{header "Additional Commands:"}}{{range $cmds}}{{if (and (eq .GroupID "") (or .IsAvailableCommand (eq .Name "help")))}}
  {{emphasis (rpad .Name .NamePadding)}} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

{{header "Flags:"}}
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

{{header "Global Flags:"}}
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasHelpSubCommands}}

{{header "Additional help topics:"}}{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{emphasis (rpad .CommandPath .CommandPathPadding)}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

Use "{{muted (printf "%s [command] --help" .CommandPath)}}" for more information about a command.{{end}}
`)

	// Add global flags
	rootCmd.PersistentFlags().String("ssh-key", "",
		"Path to SSH private key file or key content for git operations (can also use SX_SSH_KEY environment variable)")
	rootCmd.PersistentFlags().String("profile", "",
		"Use a specific profile (can also use SX_PROFILE environment variable)")

	// Add subcommands
	rootCmd.AddCommand(commands.NewInitCommand())
	rootCmd.AddCommand(commands.NewProfileCommand())
	rootCmd.AddCommand(commands.NewInstallCommand())
	rootCmd.AddCommand(commands.NewUninstallCommand())
	rootCmd.AddCommand(commands.NewRemoveCommand())
	rootCmd.AddCommand(commands.NewAddCommand())
	rootCmd.AddCommand(commands.NewUpdateTemplatesCommand())
	rootCmd.AddCommand(commands.NewUpdateCommand())
	rootCmd.AddCommand(commands.NewReportUsageCommand())
	rootCmd.AddCommand(commands.NewServeCommand())
	rootCmd.AddCommand(commands.NewConfigCommand())
	rootCmd.AddCommand(commands.NewClientsCommand())
	rootCmd.AddCommand(commands.NewVaultCommand())
	rootCmd.AddCommand(commands.NewRoleCommand())

	if err := rootCmd.Execute(); err != nil {
		// Print error with styling
		styledOut := ui.NewOutput(os.Stdout, os.Stderr)
		styledOut.Error(err.Error())
		os.Exit(1)
	}
}
