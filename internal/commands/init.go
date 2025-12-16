package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/skills/internal/config"
	"github.com/sleuth-io/skills/internal/registry"
	"github.com/sleuth-io/skills/internal/ui"
	"github.com/sleuth-io/skills/internal/ui/components"
)

const (
	defaultSleuthServerURL = "https://app.skills.new"
)

// NewInitCommand creates the init command
func NewInitCommand() *cobra.Command {
	var (
		repoType  string
		serverURL string
		repoURL   string
	)

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize configuration (local path, Git repo, or Sleuth server)",
		Long: `Initialize sx configuration using a local directory, Git repository,
or Sleuth server as the asset source.

By default, runs in interactive mode with local path as the default option.
Use flags for non-interactive mode.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInit(cmd, args, repoType, serverURL, repoURL)
		},
	}

	cmd.Flags().StringVar(&repoType, "type", "", "Repository type: 'path', 'git', or 'sleuth'")
	cmd.Flags().StringVar(&serverURL, "server-url", "", "Sleuth server URL (for type=sleuth)")
	cmd.Flags().StringVar(&repoURL, "repo-url", "", "Repository URL (git URL, file:// URL, or directory path)")

	return cmd
}

// runInit executes the init command
func runInit(cmd *cobra.Command, args []string, repoType, serverURL, repoURL string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	styledOut := ui.NewOutput(cmd.OutOrStdout(), cmd.ErrOrStderr())

	// Check if config already exists
	if config.Exists() {
		styledOut.Warning("Configuration already exists.")
		confirmed, err := components.Confirm("Overwrite existing configuration?", false)
		if err != nil || !confirmed {
			return fmt.Errorf("initialization cancelled")
		}
	}

	// Determine if we're in non-interactive mode
	nonInteractive := repoType != ""

	var err error
	if nonInteractive {
		err = runInitNonInteractive(cmd, ctx, repoType, serverURL, repoURL)
	} else {
		err = runInitInteractive(cmd, ctx)
	}

	if err != nil {
		return err
	}

	// Post-init steps (hooks and featured skills)
	runPostInit(cmd, ctx)

	return nil
}

// runPostInit runs common steps after successful initialization
func runPostInit(cmd *cobra.Command, ctx context.Context) {
	out := newOutputHelper(cmd)

	// Install hooks for all detected clients
	installAllClientHooks(ctx, out)

	// Offer to install featured skills
	promptFeaturedSkills(cmd, ctx)
}

// runInitInteractive runs the init command in interactive mode
func runInitInteractive(cmd *cobra.Command, ctx context.Context) error {
	styledOut := ui.NewOutput(cmd.OutOrStdout(), cmd.ErrOrStderr())

	styledOut.Header("Initialize sx")
	styledOut.Newline()

	options := []components.Option{
		{Label: "Just for myself", Value: "personal", Description: "Local vault"},
		{Label: "Share with my team", Value: "team", Description: "Git or Sleuth server"},
	}

	selected, err := components.SelectWithDefault("How will you use sx?", options, 0)
	if err != nil {
		return err
	}

	switch selected.Value {
	case "personal":
		return initPersonalRepository(cmd, ctx)
	case "team":
		return initTeamRepository(cmd, ctx)
	default:
		return fmt.Errorf("invalid choice: %s", selected.Value)
	}
}

// initPersonalRepository sets up a local vault in ~/.config/sx/vault
func initPersonalRepository(cmd *cobra.Command, ctx context.Context) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	vaultPath := filepath.Join(home, ".config", "sx", "vault")
	return configurePathRepo(cmd, ctx, vaultPath)
}

// initTeamRepository prompts for team repository options (git or sleuth)
func initTeamRepository(cmd *cobra.Command, ctx context.Context) error {
	options := []components.Option{
		{Label: "Sleuth", Value: "sleuth", Description: "Managed assets platform"},
		{Label: "Git repository", Value: "git", Description: "Self-hosted Git repo"},
	}

	selected, err := components.SelectWithDefault("Choose how to share with your team:", options, 0)
	if err != nil {
		return err
	}

	switch selected.Value {
	case "sleuth":
		return initSleuthServer(cmd, ctx)
	case "git":
		return initGitRepository(cmd, ctx)
	default:
		return fmt.Errorf("invalid choice: %s", selected.Value)
	}
}

// runInitNonInteractive runs the init command in non-interactive mode
func runInitNonInteractive(cmd *cobra.Command, ctx context.Context, repoType, serverURL, repoURL string) error {
	switch repoType {
	case "sleuth":
		if serverURL == "" {
			serverURL = defaultSleuthServerURL
		}
		return authenticateSleuth(cmd, ctx, serverURL)

	case "git":
		if repoURL == "" {
			return fmt.Errorf("--repo-url is required for type=git")
		}
		return configureGitRepo(cmd, ctx, repoURL)

	case "path":
		if repoURL == "" {
			return fmt.Errorf("--repo-url is required for type=path")
		}
		return configurePathRepo(cmd, ctx, repoURL)

	default:
		return fmt.Errorf("invalid repository type: %s (must be 'path', 'git', or 'sleuth')", repoType)
	}
}

// initSleuthServer initializes Sleuth server configuration
func initSleuthServer(cmd *cobra.Command, ctx context.Context) error {
	styledOut := ui.NewOutput(cmd.OutOrStdout(), cmd.ErrOrStderr())
	styledOut.Newline()

	serverURL, err := components.InputWithDefault("Enter Sleuth server URL", defaultSleuthServerURL)
	if err != nil {
		return err
	}

	return authenticateSleuth(cmd, ctx, serverURL)
}

// authenticateSleuth performs OAuth authentication with Sleuth server
func authenticateSleuth(cmd *cobra.Command, ctx context.Context, serverURL string) error {
	styledOut := ui.NewOutput(cmd.OutOrStdout(), cmd.ErrOrStderr())

	styledOut.Newline()
	styledOut.Muted("Authenticating with Sleuth server...")
	styledOut.Newline()

	// Start OAuth device code flow
	oauthClient := config.NewOAuthClient(serverURL)
	deviceResp, err := oauthClient.StartDeviceFlow(ctx)
	if err != nil {
		return fmt.Errorf("failed to start authentication: %w", err)
	}

	// Display instructions
	styledOut.Println("To authenticate, please visit:")
	styledOut.Newline()
	styledOut.Printf("  %s\n", styledOut.EmphasisText(deviceResp.VerificationURI))
	styledOut.Newline()
	styledOut.Printf("And enter code: %s\n", styledOut.BoldText(deviceResp.UserCode))
	styledOut.Newline()

	// Try to open browser
	browserURL := deviceResp.VerificationURIComplete
	if browserURL == "" {
		browserURL = deviceResp.VerificationURI
	}
	if err := config.OpenBrowser(browserURL); err == nil {
		styledOut.Muted("(Browser opened automatically)")
	}

	styledOut.Newline()

	// Poll for token with spinner
	tokenResp, err := components.RunWithSpinner("Waiting for authorization", func() (*config.OAuthTokenResponse, error) {
		return oauthClient.PollForToken(ctx, deviceResp.DeviceCode)
	})
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	// Save configuration
	cfg := &config.Config{
		Type:          config.RepositoryTypeSleuth,
		RepositoryURL: serverURL,
		AuthToken:     tokenResp.AccessToken,
	}

	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("failed to save configuration: %w", err)
	}

	styledOut.Newline()
	styledOut.Success("Authentication successful!")
	styledOut.Muted("Configuration saved.")

	return nil
}

// initGitRepository initializes Git repository configuration
func initGitRepository(cmd *cobra.Command, ctx context.Context) error {
	styledOut := ui.NewOutput(cmd.OutOrStdout(), cmd.ErrOrStderr())
	styledOut.Newline()

	repoURL, err := components.Input("Enter Git repository URL")
	if err != nil {
		return err
	}

	if repoURL == "" {
		return fmt.Errorf("repository URL is required")
	}

	return configureGitRepo(cmd, ctx, repoURL)
}

// configureGitRepo configures a Git repository
func configureGitRepo(cmd *cobra.Command, ctx context.Context, repoURL string) error {
	styledOut := ui.NewOutput(cmd.OutOrStdout(), cmd.ErrOrStderr())

	styledOut.Newline()
	styledOut.Muted("Configuring Git repository...")

	// Save configuration
	cfg := &config.Config{
		Type:          config.RepositoryTypeGit,
		RepositoryURL: repoURL,
	}

	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("failed to save configuration: %w", err)
	}

	styledOut.Newline()
	styledOut.Success("Configuration saved!")
	styledOut.KeyValue("Git repository", repoURL)

	return nil
}

// configurePathRepo configures a local path repository
func configurePathRepo(cmd *cobra.Command, ctx context.Context, repoPath string) error {
	styledOut := ui.NewOutput(cmd.OutOrStdout(), cmd.ErrOrStderr())

	// Convert path to absolute path first
	var absPath string
	var err error
	if strings.HasPrefix(repoPath, "file://") {
		// Extract path from file:// URL and expand
		repoPath = strings.TrimPrefix(repoPath, "file://")
		absPath, err = expandPath(repoPath)
		if err != nil {
			return fmt.Errorf("invalid path: %w", err)
		}
	} else {
		// Expand and normalize the path
		absPath, err = expandPath(repoPath)
		if err != nil {
			return fmt.Errorf("invalid path: %w", err)
		}
	}

	// Create directory if needed
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		if err := os.MkdirAll(absPath, 0755); err != nil {
			return fmt.Errorf("failed to create directory: %w", err)
		}
	}

	// Save configuration
	cfg := &config.Config{
		Type:          config.RepositoryTypePath,
		RepositoryURL: "file://" + absPath,
	}

	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("failed to save configuration: %w", err)
	}

	styledOut.Newline()
	styledOut.Success("Configuration saved!")

	return nil
}

// expandPath expands tilde and converts relative paths to absolute
func expandPath(path string) (string, error) {
	// Handle tilde
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get home directory: %w", err)
		}
		path = filepath.Join(home, path[2:])
	}

	// Convert to absolute path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	return absPath, nil
}

// promptFeaturedSkills offers to install featured skills after init
func promptFeaturedSkills(cmd *cobra.Command, ctx context.Context) {
	styledOut := ui.NewOutput(cmd.OutOrStdout(), cmd.ErrOrStderr())

	skills, err := registry.FeaturedSkills()
	if err != nil || len(skills) == 0 {
		return
	}

	var addedAny bool
	for {
		styledOut.Newline()

		// Build options with continue at the top, then skills
		options := make([]components.Option, len(skills)+1)
		options[0] = components.Option{
			Label: "Continue",
			Value: "continue",
		}
		for i, skill := range skills {
			options[i+1] = components.Option{
				Label:       skill.Name,
				Value:       skill.URL,
				Description: skill.Description,
			}
		}

		selected, err := components.SelectWithDefault("Would you like to install a featured asset?", options, 0)
		if err != nil || selected.Value == "continue" {
			break
		}

		styledOut.Newline()
		styledOut.Muted(fmt.Sprintf("Adding %s...", selected.Label))

		// Run the add command with the skill URL (skip install prompt, we'll do it at the end)
		if err := runAddSkipInstall(cmd, selected.Value); err != nil {
			styledOut.Error(fmt.Sprintf("Failed to add asset: %v", err))
		} else {
			addedAny = true
		}
	}

	// If any skills were added, prompt to install once
	if addedAny {
		out := newOutputHelper(cmd)
		promptRunInstall(cmd, ctx, out)
	}
}
