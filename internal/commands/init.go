package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/sx/internal/clients"
	"github.com/sleuth-io/sx/internal/config"
	"github.com/sleuth-io/sx/internal/registry"
	"github.com/sleuth-io/sx/internal/ui"
	"github.com/sleuth-io/sx/internal/ui/components"
	"github.com/sleuth-io/sx/internal/utils"
)

const (
	defaultSleuthServerURL = "https://app.skills.new"
)

// NewInitCommand creates the init command
func NewInitCommand() *cobra.Command {
	var (
		repoType    string
		serverURL   string
		repoURL     string
		clientsFlag string
	)

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize configuration (local path, Git repo, or Skills.new)",
		Long: `Initialize sx configuration using a local directory, Git repository,
or Skills.new as the asset source.

By default, runs in interactive mode with local path as the default option.
Use flags for non-interactive mode.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInit(cmd, args, repoType, serverURL, repoURL, clientsFlag)
		},
	}

	cmd.Flags().StringVar(&repoType, "type", "", "Repository type: 'path', 'git', or 'sleuth'")
	cmd.Flags().StringVar(&serverURL, "server-url", "", "Skills.new server URL (for type=sleuth)")
	cmd.Flags().StringVar(&repoURL, "repo-url", "", "Repository URL (git URL, file:// URL, or directory path)")
	cmd.Flags().StringVar(&clientsFlag, "clients", "", "Comma-separated client IDs (e.g., 'claude-code,cursor') or 'all'")

	return cmd
}

// runInit executes the init command
func runInit(cmd *cobra.Command, args []string, repoType, serverURL, repoURL, clientsFlag string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	styledOut := ui.NewOutput(cmd.OutOrStdout(), cmd.ErrOrStderr())

	// Load existing config if present (for pre-populating options)
	var existingCfg *config.Config
	if config.Exists() {
		styledOut.Warning("Configuration already exists.")
		confirmed, err := components.Confirm("Overwrite existing configuration?", false)
		if err != nil || !confirmed {
			return fmt.Errorf("initialization cancelled")
		}
		// Load existing config to pre-populate options
		existingCfg, _ = config.Load()
	}

	// Determine if we're in non-interactive mode
	nonInteractive := repoType != ""

	var enabledClients []string
	var err error

	if nonInteractive {
		// Parse clients flag for non-interactive mode
		enabledClients, err = parseClientsFlag(clientsFlag)
		if err != nil {
			return err
		}
		err = runInitNonInteractive(cmd, ctx, repoType, serverURL, repoURL, enabledClients)
	} else {
		enabledClients, err = runInitInteractive(cmd, ctx, existingCfg)
	}

	if err != nil {
		return err
	}

	// Post-init steps (hooks and featured skills)
	runPostInit(cmd, ctx, enabledClients)

	return nil
}

// runPostInit runs common steps after successful initialization
func runPostInit(cmd *cobra.Command, ctx context.Context, enabledClients []string) {
	out := newOutputHelper(cmd)

	// Install hooks for enabled clients only
	installSelectedClientHooks(ctx, out, enabledClients)

	// Offer to install featured skills
	promptFeaturedSkills(cmd, ctx)
}

// runInitInteractive runs the init command in interactive mode
// Returns the list of enabled client IDs
// existingCfg may be nil if no previous config exists
func runInitInteractive(cmd *cobra.Command, ctx context.Context, existingCfg *config.Config) ([]string, error) {
	styledOut := ui.NewOutput(cmd.OutOrStdout(), cmd.ErrOrStderr())

	styledOut.Header("Initialize sx")
	styledOut.Newline()

	options := []components.Option{
		{Label: "Just for myself", Value: "personal", Description: "Local vault"},
		{Label: "Share with my team", Value: "team", Description: "Git or Skills.new"},
	}

	// Pre-select based on existing config
	defaultIndex := 0
	if existingCfg != nil && (existingCfg.Type == config.RepositoryTypeGit || existingCfg.Type == config.RepositoryTypeSleuth) {
		defaultIndex = 1 // "Share with my team"
	}

	selected, err := components.SelectWithDefault("How will you use sx?", options, defaultIndex)
	if err != nil {
		return nil, err
	}

	// Prompt for client selection (with existing enabled clients pre-selected)
	var existingEnabledClients []string
	if existingCfg != nil {
		existingEnabledClients = existingCfg.EnabledClients
	}
	enabledClients, err := promptClientSelection(styledOut, existingEnabledClients)
	if err != nil {
		return nil, err
	}

	switch selected.Value {
	case "personal":
		err = initPersonalRepository(cmd, ctx, enabledClients)
	case "team":
		err = initTeamRepository(cmd, ctx, enabledClients, existingCfg)
	default:
		return nil, fmt.Errorf("invalid choice: %s", selected.Value)
	}

	if err != nil {
		return nil, err
	}

	return enabledClients, nil
}

// initPersonalRepository sets up a local vault in the user's config directory
func initPersonalRepository(cmd *cobra.Command, ctx context.Context, enabledClients []string) error {
	configDir, err := utils.GetConfigDir()
	if err != nil {
		return fmt.Errorf("failed to get config directory: %w", err)
	}

	vaultPath := filepath.Join(configDir, "vault")
	return configurePathRepo(cmd, ctx, vaultPath, enabledClients)
}

// initTeamRepository prompts for team repository options (git or sleuth)
func initTeamRepository(cmd *cobra.Command, ctx context.Context, enabledClients []string, existingCfg *config.Config) error {
	options := []components.Option{
		{Label: "Skills.new", Value: "sleuth", Description: "Managed assets platform"},
		{Label: "Git repository", Value: "git", Description: "Self-hosted Git repo"},
	}

	// Pre-select based on existing config
	defaultIndex := 0
	if existingCfg != nil && existingCfg.Type == config.RepositoryTypeGit {
		defaultIndex = 1 // "Git repository"
	}

	selected, err := components.SelectWithDefault("Choose how to share with your team:", options, defaultIndex)
	if err != nil {
		return err
	}

	switch selected.Value {
	case "sleuth":
		return initSleuthServer(cmd, ctx, enabledClients, existingCfg)
	case "git":
		return initGitRepository(cmd, ctx, enabledClients, existingCfg)
	default:
		return fmt.Errorf("invalid choice: %s", selected.Value)
	}
}

// runInitNonInteractive runs the init command in non-interactive mode
func runInitNonInteractive(cmd *cobra.Command, ctx context.Context, repoType, serverURL, repoURL string, enabledClients []string) error {
	switch repoType {
	case "sleuth":
		if serverURL == "" {
			serverURL = defaultSleuthServerURL
		}
		return authenticateSleuth(cmd, ctx, serverURL, enabledClients)

	case "git":
		if repoURL == "" {
			return fmt.Errorf("--repo-url is required for type=git")
		}
		return configureGitRepo(cmd, ctx, repoURL, enabledClients)

	case "path":
		if repoURL == "" {
			return fmt.Errorf("--repo-url is required for type=path")
		}
		return configurePathRepo(cmd, ctx, repoURL, enabledClients)

	default:
		return fmt.Errorf("invalid repository type: %s (must be 'path', 'git', or 'sleuth')", repoType)
	}
}

// initSleuthServer initializes Skills.new server configuration
func initSleuthServer(cmd *cobra.Command, ctx context.Context, enabledClients []string, existingCfg *config.Config) error {
	styledOut := ui.NewOutput(cmd.OutOrStdout(), cmd.ErrOrStderr())
	styledOut.Newline()

	// Pre-populate with existing server URL if available
	defaultURL := defaultSleuthServerURL
	if existingCfg != nil && existingCfg.Type == config.RepositoryTypeSleuth && existingCfg.RepositoryURL != "" {
		defaultURL = existingCfg.RepositoryURL
	}

	serverURL, err := components.InputWithDefault("Enter Skills.new server URL", defaultURL)
	if err != nil {
		return err
	}

	return authenticateSleuth(cmd, ctx, serverURL, enabledClients)
}

// authenticateSleuth performs OAuth authentication with Skills.new server
func authenticateSleuth(cmd *cobra.Command, ctx context.Context, serverURL string, enabledClients []string) error {
	styledOut := ui.NewOutput(cmd.OutOrStdout(), cmd.ErrOrStderr())

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
		Type:           config.RepositoryTypeSleuth,
		RepositoryURL:  serverURL,
		AuthToken:      tokenResp.AccessToken,
		EnabledClients: enabledClients,
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
func initGitRepository(cmd *cobra.Command, ctx context.Context, enabledClients []string, existingCfg *config.Config) error {
	styledOut := ui.NewOutput(cmd.OutOrStdout(), cmd.ErrOrStderr())
	styledOut.Newline()

	// Pre-populate with existing Git URL if available
	var repoURL string
	var err error
	if existingCfg != nil && existingCfg.Type == config.RepositoryTypeGit && existingCfg.RepositoryURL != "" {
		repoURL, err = components.InputWithDefault("Enter Git repository URL", existingCfg.RepositoryURL)
	} else {
		repoURL, err = components.Input("Enter Git repository URL")
	}
	if err != nil {
		return err
	}

	if repoURL == "" {
		return fmt.Errorf("repository URL is required")
	}

	return configureGitRepo(cmd, ctx, repoURL, enabledClients)
}

// configureGitRepo configures a Git repository
func configureGitRepo(cmd *cobra.Command, ctx context.Context, repoURL string, enabledClients []string) error {
	styledOut := ui.NewOutput(cmd.OutOrStdout(), cmd.ErrOrStderr())

	// Save configuration
	cfg := &config.Config{
		Type:           config.RepositoryTypeGit,
		RepositoryURL:  repoURL,
		EnabledClients: enabledClients,
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
func configurePathRepo(cmd *cobra.Command, ctx context.Context, repoPath string, enabledClients []string) error {
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
		Type:           config.RepositoryTypePath,
		RepositoryURL:  "file://" + absPath,
		EnabledClients: enabledClients,
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

// parseClientsFlag parses the --clients flag value and validates client IDs
// Returns nil for "all" or empty string (meaning all detected clients)
func parseClientsFlag(clientsFlag string) ([]string, error) {
	if clientsFlag == "" || strings.ToLower(clientsFlag) == "all" {
		return nil, nil // nil means all detected clients
	}

	parts := strings.Split(clientsFlag, ",")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if !clients.IsValidClientID(p) {
			return nil, fmt.Errorf("invalid client ID: %s (valid options: %s)", p, strings.Join(clients.AllClientIDs(), ", "))
		}
		result = append(result, p)
	}

	if len(result) == 0 {
		return nil, nil
	}

	return result, nil
}

// promptClientSelection detects installed clients and prompts user for selection
// existingEnabledClients can be used to pre-select previously enabled clients
func promptClientSelection(styledOut *ui.Output, existingEnabledClients []string) ([]string, error) {
	registry := clients.Global()
	installedClients := registry.DetectInstalled()

	if len(installedClients) == 0 {
		styledOut.Warning("No AI coding clients detected")
		return nil, nil
	}

	if len(installedClients) == 1 {
		// Only one client - no need to prompt
		client := installedClients[0]
		styledOut.Newline()
		styledOut.Muted(fmt.Sprintf("Detected %s - will install assets there", client.DisplayName()))
		return []string{client.ID()}, nil
	}

	// Build set of previously enabled clients for quick lookup
	previouslyEnabled := make(map[string]bool)
	for _, id := range existingEnabledClients {
		previouslyEnabled[id] = true
	}

	// Build client names list for display
	var clientNames []string
	for _, client := range installedClients {
		clientNames = append(clientNames, client.DisplayName())
	}
	clientList := strings.Join(clientNames, ", ")

	// Step 1: Ask if user wants all clients
	// Default to "yes" if no previous config, or if all detected clients were previously enabled
	allPreviouslyEnabled := len(existingEnabledClients) == 0
	if !allPreviouslyEnabled {
		allPreviouslyEnabled = true
		for _, client := range installedClients {
			if !previouslyEnabled[client.ID()] {
				allPreviouslyEnabled = false
				break
			}
		}
	}

	styledOut.Newline()
	installAll, err := components.Confirm(fmt.Sprintf("Install to all detected clients (%s)?", clientList), allPreviouslyEnabled)
	if err != nil {
		return nil, err
	}

	if installAll {
		var clientIDs []string
		for _, client := range installedClients {
			clientIDs = append(clientIDs, client.ID())
		}
		return clientIDs, nil
	}

	// Step 2: Show multi-select for individual client selection
	// Pre-select based on existing config, or all if no existing config
	options := make([]components.MultiSelectOption, len(installedClients))
	for i, client := range installedClients {
		// If we have previous config, use it; otherwise default to all selected
		selected := len(existingEnabledClients) == 0 || previouslyEnabled[client.ID()]
		options[i] = components.MultiSelectOption{
			Label:    client.DisplayName(),
			Value:    client.ID(),
			Selected: selected,
		}
	}

	selected, err := components.MultiSelect("Select clients to install to:", options)
	if err != nil {
		return nil, err
	}

	var clientIDs []string
	for _, opt := range selected {
		if opt.Selected {
			clientIDs = append(clientIDs, opt.Value)
		}
	}

	if len(clientIDs) == 0 {
		styledOut.Warning("No clients selected - assets will not be installed anywhere")
	}

	return clientIDs, nil
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
