package main

import (
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/sx/internal/autoupdate"
	"github.com/sleuth-io/sx/internal/buildinfo"
	_ "github.com/sleuth-io/sx/internal/clients/claude_code"    // Register Claude Code client
	_ "github.com/sleuth-io/sx/internal/clients/cline"          // Register Cline client
	_ "github.com/sleuth-io/sx/internal/clients/codex"          // Register Codex client
	_ "github.com/sleuth-io/sx/internal/clients/cursor"         // Register Cursor client
	_ "github.com/sleuth-io/sx/internal/clients/gemini"         // Register Gemini Code Assist client
	_ "github.com/sleuth-io/sx/internal/clients/github_copilot" // Register GitHub Copilot client
	_ "github.com/sleuth-io/sx/internal/clients/kiro"           // Register Kiro client
	_ "github.com/sleuth-io/sx/internal/clients/openclaw"       // Register OpenClaw client
	_ "github.com/sleuth-io/sx/internal/clients/opencode"       // Register OpenCode client
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

// colorizeFlags returns a template function that colors flag names in cobra's
// FlagUsages output while leaving descriptions in the default color.
// Each line looks like: "      --flag-name string   Description text"
func colorizeFlags(render func(...string) string) func(string) string {
	return func(flagUsages string) string {
		var result strings.Builder
		for i, line := range strings.Split(flagUsages, "\n") {
			if i > 0 {
				result.WriteByte('\n')
			}
			// Find where the description starts: two or more consecutive spaces
			// after the flag+type portion. Flag lines have leading whitespace,
			// then the flag (e.g. "-h, --help" or "    --all"), optionally
			// followed by a type word like "string", then 2+ spaces before desc.
			trimmed := strings.TrimLeft(line, " ")
			if trimmed == "" || !strings.HasPrefix(trimmed, "-") {
				result.WriteString(line)
				continue
			}

			// Walk past the flag and optional type to find the description gap
			leadingSpaces := len(line) - len(trimmed)
			descIdx := -1
			inGap := false
			gapStart := 0
			for j := leadingSpaces; j < len(line); j++ {
				if line[j] == ' ' {
					if !inGap {
						inGap = true
						gapStart = j
					}
					if j-gapStart >= 2 {
						// Check there's actually content after the gap
						rest := strings.TrimLeft(line[j:], " ")
						if rest != "" {
							descIdx = len(line) - len(rest)
						}
						break
					}
				} else {
					inGap = false
				}
			}

			if descIdx > 0 {
				result.WriteString(line[:leadingSpaces])
				result.WriteString(render(line[leadingSpaces:descIdx]))
				result.WriteString(line[descIdx:])
			} else {
				// No description found, color the whole flag part
				result.WriteString(line[:leadingSpaces])
				result.WriteString(render(line[leadingSpaces:]))
			}
		}
		return result.String()
	}
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

	// Resolve executable path before any update replaces the binary on disk.
	// After replacement, /proc/self/exe points to the old (deleted) inode.
	exe, _ := os.Executable()

	// Apply any pending update from a previous background check that didn't complete.
	// If an update was applied, re-exec so the new binary handles this invocation.
	if autoupdate.ApplyPendingUpdate() && exe != "" {
		if err := syscall.Exec(exe, os.Args, os.Environ()); err != nil {
			log.Error("failed to re-exec after update", "error", err)
		}
	}

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
	cobra.AddTemplateFunc("colorizeFlags", colorizeFlags(styles.Emphasis.Render))

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
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces | colorizeFlags}}{{end}}{{if .HasAvailableInheritedFlags}}

{{header "Global Flags:"}}
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces | colorizeFlags}}{{end}}{{if .HasHelpSubCommands}}

{{header "Additional help topics:"}}{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{emphasis (rpad .CommandPath .CommandPathPadding)}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

Use "{{muted (printf "%s [command] --help" .CommandPath)}}" for more information about a command.{{end}}
`)

	// Add global flags
	rootCmd.PersistentFlags().String("ssh-key", "",
		"Path to SSH private key file or key content for git operations (can also use SX_SSH_KEY environment variable)")
	rootCmd.PersistentFlags().String("profile", "",
		"Override the active profile set for this command (comma-separated for multi-active; can also use SX_PROFILE environment variable)")

	// Add subcommands
	rootCmd.AddCommand(commands.NewInitCommand())
	rootCmd.AddCommand(commands.NewProfileCommand())
	rootCmd.AddCommand(commands.NewInstallCommand())
	rootCmd.AddCommand(commands.NewUninstallCommand())
	rootCmd.AddCommand(commands.NewSelfUninstallCommand())
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
	rootCmd.AddCommand(commands.NewTeamCommand())
	rootCmd.AddCommand(commands.NewBotCommand())
	rootCmd.AddCommand(commands.NewStatsCommand())
	rootCmd.AddCommand(commands.NewAuditCommand())
	rootCmd.AddCommand(commands.NewCloudCommand())

	if err := rootCmd.Execute(); err != nil {
		// Print error with styling
		styledOut := ui.NewOutput(os.Stdout, os.Stderr)
		styledOut.Error(err.Error())
		os.Exit(1)
	}
}
