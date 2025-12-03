package commands

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/skills/internal/config"
)

const (
	defaultSleuthServerURL = "https://app.sleuth.io"
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
		Short: "Initialize configuration (authenticate with Sleuth server or configure Git repo)",
		Long: `Initialize skills configuration by authenticating with a Sleuth server
or configuring a Git repository as the artifact source.

By default, runs in interactive mode. Use flags for non-interactive mode.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInit(cmd, args, repoType, serverURL, repoURL)
		},
	}

	cmd.Flags().StringVar(&repoType, "type", "", "Repository type: 'sleuth' or 'git'")
	cmd.Flags().StringVar(&serverURL, "server-url", "", "Sleuth server URL (for type=sleuth)")
	cmd.Flags().StringVar(&repoURL, "repo-url", "", "Git repository URL (for type=git)")

	return cmd
}

// runInit executes the init command
func runInit(cmd *cobra.Command, args []string, repoType, serverURL, repoURL string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	out := newOutputHelper(cmd)

	// Check if config already exists
	if config.Exists() {
		out.printErr("Configuration already exists.")
		response, _ := out.prompt("Overwrite existing configuration? (y/N): ")
		response = strings.ToLower(response)
		if response != "y" && response != "yes" {
			return fmt.Errorf("initialization cancelled")
		}
	}

	// Determine if we're in non-interactive mode
	nonInteractive := repoType != ""

	if nonInteractive {
		return runInitNonInteractive(cmd, ctx, repoType, serverURL, repoURL)
	}

	return runInitInteractive(cmd, ctx)
}

// runInitInteractive runs the init command in interactive mode
func runInitInteractive(cmd *cobra.Command, ctx context.Context) error {
	out := newOutputHelper(cmd)

	out.println("Initialize Skills CLI")
	out.println()
	out.println("Choose repository type:")
	out.println("  1) Sleuth server (OAuth authentication)")
	out.println("  2) Git repository")
	out.println()

	choice, _ := out.prompt("Enter choice (1 or 2): ")

	switch choice {
	case "1":
		return initSleuthServer(cmd, ctx)
	case "2":
		return initGitRepository(cmd, ctx)
	default:
		return fmt.Errorf("invalid choice: %s", choice)
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

	default:
		return fmt.Errorf("invalid repository type: %s (must be 'sleuth' or 'git')", repoType)
	}
}

// initSleuthServer initializes Sleuth server configuration
func initSleuthServer(cmd *cobra.Command, ctx context.Context) error {
	out := newOutputHelper(cmd)

	out.println()
	serverURL, _ := out.promptWithDefault("Enter Sleuth server URL", defaultSleuthServerURL)

	return authenticateSleuth(cmd, ctx, serverURL)
}

// authenticateSleuth performs OAuth authentication with Sleuth server
func authenticateSleuth(cmd *cobra.Command, ctx context.Context, serverURL string) error {
	out := newOutputHelper(cmd)

	out.println()
	out.println("Authenticating with Sleuth server...")
	out.println()

	// Start OAuth device code flow
	oauthClient := config.NewOAuthClient(serverURL)
	deviceResp, err := oauthClient.StartDeviceFlow(ctx)
	if err != nil {
		return fmt.Errorf("failed to start authentication: %w", err)
	}

	// Display instructions
	out.println("To authenticate, please visit:")
	out.println()
	out.printf("  %s\n", deviceResp.VerificationURI)
	out.println()
	out.printf("And enter code: %s\n", deviceResp.UserCode)
	out.println()

	// Try to open browser
	browserURL := deviceResp.VerificationURIComplete
	if browserURL == "" {
		browserURL = deviceResp.VerificationURI
	}
	if err := config.OpenBrowser(browserURL); err == nil {
		out.println("(Browser opened automatically)")
	}

	out.println()
	out.println("Waiting for authorization...")

	// Poll for token
	tokenResp, err := oauthClient.PollForToken(ctx, deviceResp.DeviceCode)
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	// Save configuration
	cfg := &config.Config{
		Type:      config.RepositoryTypeSleuth,
		ServerURL: serverURL,
		AuthToken: tokenResp.AccessToken,
	}

	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("failed to save configuration: %w", err)
	}

	out.println()
	out.println("✓ Authentication successful!")
	out.println("Configuration saved.")

	return nil
}

// initGitRepository initializes Git repository configuration
func initGitRepository(cmd *cobra.Command, ctx context.Context) error {
	out := newOutputHelper(cmd)

	out.println()
	repoURL, _ := out.prompt("Enter Git repository URL: ")

	if repoURL == "" {
		return fmt.Errorf("repository URL is required")
	}

	return configureGitRepo(cmd, ctx, repoURL)
}

// configureGitRepo configures a Git repository
func configureGitRepo(cmd *cobra.Command, ctx context.Context, repoURL string) error {
	out := newOutputHelper(cmd)

	out.println()
	out.println("Configuring Git repository...")

	// Save configuration
	cfg := &config.Config{
		Type:          config.RepositoryTypeGit,
		RepositoryURL: repoURL,
	}

	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("failed to save configuration: %w", err)
	}

	out.println()
	out.println("✓ Configuration saved!")
	out.println("Git repository:", repoURL)

	return nil
}
