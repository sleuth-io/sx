package commands

import (
	"bufio"
	"context"
	"fmt"
	"os"
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

	// Check if config already exists
	if config.Exists() {
		fmt.Fprintln(os.Stderr, "Configuration already exists.")
		reader := bufio.NewReader(os.Stdin)
		fmt.Fprint(os.Stderr, "Overwrite existing configuration? (y/N): ")
		response, _ := reader.ReadString('\n')
		response = strings.TrimSpace(strings.ToLower(response))
		if response != "y" && response != "yes" {
			return fmt.Errorf("initialization cancelled")
		}
	}

	// Determine if we're in non-interactive mode
	nonInteractive := repoType != ""

	if nonInteractive {
		return runInitNonInteractive(ctx, repoType, serverURL, repoURL)
	}

	return runInitInteractive(ctx)
}

// runInitInteractive runs the init command in interactive mode
func runInitInteractive(ctx context.Context) error {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("Initialize Skills CLI")
	fmt.Println()
	fmt.Println("Choose repository type:")
	fmt.Println("  1) Sleuth server (OAuth authentication)")
	fmt.Println("  2) Git repository")
	fmt.Println()
	fmt.Fprint(os.Stderr, "Enter choice (1 or 2): ")

	choice, _ := reader.ReadString('\n')
	choice = strings.TrimSpace(choice)

	switch choice {
	case "1":
		return initSleuthServer(ctx, reader)
	case "2":
		return initGitRepository(ctx, reader)
	default:
		return fmt.Errorf("invalid choice: %s", choice)
	}
}

// runInitNonInteractive runs the init command in non-interactive mode
func runInitNonInteractive(ctx context.Context, repoType, serverURL, repoURL string) error {
	switch repoType {
	case "sleuth":
		if serverURL == "" {
			serverURL = defaultSleuthServerURL
		}
		return authenticateSleuth(ctx, serverURL)

	case "git":
		if repoURL == "" {
			return fmt.Errorf("--repo-url is required for type=git")
		}
		return configureGitRepo(ctx, repoURL)

	default:
		return fmt.Errorf("invalid repository type: %s (must be 'sleuth' or 'git')", repoType)
	}
}

// initSleuthServer initializes Sleuth server configuration
func initSleuthServer(ctx context.Context, reader *bufio.Reader) error {
	fmt.Println()
	fmt.Fprint(os.Stderr, "Enter Sleuth server URL (default: "+defaultSleuthServerURL+"): ")
	serverURL, _ := reader.ReadString('\n')
	serverURL = strings.TrimSpace(serverURL)

	if serverURL == "" {
		serverURL = defaultSleuthServerURL
	}

	return authenticateSleuth(ctx, serverURL)
}

// authenticateSleuth performs OAuth authentication with Sleuth server
func authenticateSleuth(ctx context.Context, serverURL string) error {
	fmt.Println()
	fmt.Println("Authenticating with Sleuth server...")
	fmt.Println()

	// Start OAuth device code flow
	oauthClient := config.NewOAuthClient(serverURL)
	deviceResp, err := oauthClient.StartDeviceFlow(ctx)
	if err != nil {
		return fmt.Errorf("failed to start authentication: %w", err)
	}

	// Display instructions
	fmt.Println("To authenticate, please visit:")
	fmt.Println()
	fmt.Printf("  %s\n", deviceResp.VerificationURI)
	fmt.Println()
	fmt.Printf("And enter code: %s\n", deviceResp.UserCode)
	fmt.Println()

	// Try to open browser
	browserURL := deviceResp.VerificationURIComplete
	if browserURL == "" {
		browserURL = deviceResp.VerificationURI
	}
	if err := config.OpenBrowser(browserURL); err == nil {
		fmt.Println("(Browser opened automatically)")
	}

	fmt.Println()
	fmt.Println("Waiting for authorization...")

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

	fmt.Println()
	fmt.Println("✓ Authentication successful!")
	fmt.Println("Configuration saved.")

	return nil
}

// initGitRepository initializes Git repository configuration
func initGitRepository(ctx context.Context, reader *bufio.Reader) error {
	fmt.Println()
	fmt.Fprint(os.Stderr, "Enter Git repository URL: ")
	repoURL, _ := reader.ReadString('\n')
	repoURL = strings.TrimSpace(repoURL)

	if repoURL == "" {
		return fmt.Errorf("repository URL is required")
	}

	return configureGitRepo(ctx, repoURL)
}

// configureGitRepo configures a Git repository
func configureGitRepo(ctx context.Context, repoURL string) error {
	fmt.Println()
	fmt.Println("Configuring Git repository...")

	// Save configuration
	cfg := &config.Config{
		Type:          config.RepositoryTypeGit,
		RepositoryURL: repoURL,
	}

	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("failed to save configuration: %w", err)
	}

	fmt.Println()
	fmt.Println("✓ Configuration saved!")
	fmt.Println("Git repository:", repoURL)

	return nil
}
