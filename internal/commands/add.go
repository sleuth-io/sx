package commands

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/config"
	"github.com/sleuth-io/sx/internal/constants"
	"github.com/sleuth-io/sx/internal/github"
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/ui"
	"github.com/sleuth-io/sx/internal/ui/components"
	vaultpkg "github.com/sleuth-io/sx/internal/vault"
)

// NewAddCommand creates the add command
func NewAddCommand() *cobra.Command {
	var (
		yes         bool
		noInstall   bool
		name        string
		assetType   string
		version     string
		scopeGlobal bool
		scopeRepos  []string
	)

	cmd := &cobra.Command{
		Use:   "add [source-or-asset-name]",
		Short: "Add an asset or configure an existing one",
		Long: `Add an asset from a local zip file, directory, URL, GitHub path, or marketplace.
If the argument is an existing asset name, configure its installation scope instead.

Examples:
  sx add ./my-skill                    # Interactive mode
  sx add ./my-skill --yes              # Accept defaults, install globally
  sx add ./my-skill -y --no-install    # Add to vault only
  sx add ./my-skill --yes --scope-global
  sx add ./my-skill --yes --scope-repo git@github.com:org/repo.git
  sx add ./my-skill --yes --scope-repo "git@github.com:org/repo.git#backend/services"
  sx add ./my-skill --yes --scope-repo "git@github.com:org/repo.git#backend,frontend"`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var input string
			if len(args) > 0 {
				input = args[0]
			}
			opts := addOptions{
				Yes:         yes,
				NoInstall:   noInstall,
				Name:        name,
				Type:        assetType,
				Version:     version,
				ScopeGlobal: scopeGlobal,
				ScopeRepos:  scopeRepos,
			}
			return runAddWithFlags(cmd, input, opts)
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Accept all defaults and skip prompts")
	cmd.Flags().BoolVar(&noInstall, "no-install", false, "Skip running install after adding")
	cmd.Flags().StringVar(&name, "name", "", "Override detected asset name")
	cmd.Flags().StringVar(&assetType, "type", "", "Override detected asset type (skill, rule, agent, command, mcp, hook)")
	cmd.Flags().StringVar(&version, "version", "", "Override suggested version")
	cmd.Flags().BoolVar(&scopeGlobal, "scope-global", false, "Install globally (all repositories)")
	cmd.Flags().StringArrayVar(&scopeRepos, "scope-repo", nil, "Install for specific repository, optionally with paths (format: repo_url or repo_url#path1,path2)")

	return cmd
}

// runAddWithFlags is the main entry point
func runAddWithFlags(cmd *cobra.Command, input string, opts addOptions) error {
	// Validate scope flags upfront
	if opts.ScopeGlobal && len(opts.ScopeRepos) > 0 {
		return errors.New("cannot use --scope-global with --scope-repo")
	}
	for _, repo := range opts.ScopeRepos {
		if strings.TrimSpace(repo) == "" {
			return errors.New("--scope-repo cannot be empty")
		}
	}

	// In non-interactive mode, input is required
	if opts.isNonInteractive() && input == "" {
		return errors.New("asset path is required in non-interactive mode")
	}

	return runAddWithOptions(cmd, input, opts)
}

// runAdd executes the add command (interactive mode)
func runAdd(cmd *cobra.Command, zipFile string) error {
	return runAddWithFlags(cmd, zipFile, addOptions{})
}

// runAddSkipInstall executes the add command without prompting to install
func runAddSkipInstall(cmd *cobra.Command, zipFile string) error {
	return runAddWithFlags(cmd, zipFile, addOptions{NoInstall: true})
}

// runAddWithOptions executes the add command with configurable options
func runAddWithOptions(cmd *cobra.Command, input string, opts addOptions) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	out := newOutputHelper(cmd)
	status := components.NewStatus(cmd.OutOrStdout())

	// Check if input is plugin@marketplace syntax
	if input != "" && IsMarketplaceReference(input) {
		promptInstall := !opts.NoInstall && !opts.Yes
		return addFromMarketplace(ctx, cmd, out, status, input, promptInstall)
	}

	// Check if input is an existing asset name (not a file, directory, or URL)
	if input != "" && !isURL(input) && !github.IsTreeURL(input) {
		if _, err := os.Stat(input); os.IsNotExist(err) {
			// Not a file/directory - check if it's an existing asset
			return configureExistingAsset(ctx, cmd, out, status, input, opts)
		}
	}

	// Check if input is an instruction file (CLAUDE.md, AGENTS.md) that can be parsed for sections
	if input != "" && isInstructionFile(input) {
		if opts.isNonInteractive() {
			return errors.New("instruction files require interactive mode (multiple sections may need selection)")
		}
		promptInstall := !opts.NoInstall
		return addFromInstructionFile(ctx, cmd, out, status, input, promptInstall)
	}

	// Get and validate zip file
	zipFile, zipData, err := loadZipFile(out, status, input)
	if err != nil {
		return err
	}

	// Detect asset name and type (with optional overrides from flags)
	name, assetType, metadataExists, err := detectAssetInfo(out, status, zipFile, zipData, opts)
	if err != nil {
		return err
	}

	// Check for context cancellation before expensive vault operations
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Create vault instance
	vault, err := createVault()
	if err != nil {
		return err
	}

	// Check versions and content
	version, contentsIdentical, err := checkVersionAndContents(ctx, status, vault, name, zipData)
	if err != nil {
		return err
	}

	// Use explicit version if provided
	if opts.Version != "" {
		version = opts.Version
		contentsIdentical = false // Force add with explicit version
	}

	// Handle identical content case
	var addErr error
	if contentsIdentical {
		addErr = handleIdenticalAsset(ctx, out, status, vault, name, version, assetType, opts)
	} else {
		// Add new or updated asset
		addErr = addNewAsset(ctx, out, status, vault, name, assetType, version, zipFile, zipData, metadataExists, opts)
	}

	if addErr != nil {
		return addErr
	}

	// Handle install: auto-run if --yes, prompt if interactive, skip if --no-install
	if opts.Yes && !opts.NoInstall {
		out.println()
		if err := runInstall(cmd, nil, false, "", false, ""); err != nil {
			out.printfErr("Install failed: %v\n", err)
		}
	} else if !opts.NoInstall && !opts.isNonInteractive() {
		promptRunInstall(cmd, ctx, out)
	}

	return nil
}

// configureExistingAsset handles configuring scope for an asset that already exists in the vault
func configureExistingAsset(ctx context.Context, cmd *cobra.Command, out *outputHelper, status *components.Status, assetName string, opts addOptions) error {
	// Load vault and lock file
	vault, lockFile, err := loadVaultAndLockFile(ctx, status)
	if err != nil {
		return err
	}

	// Find and select asset version
	foundAssets := findAssetsByName(lockFile, assetName)
	foundAsset, err := selectAssetVersion(foundAssets, assetName, out)
	if errors.Is(err, ErrAssetNotFound) {
		// Not in lock file - check if it exists in vault
		promptInstall := !opts.NoInstall && !opts.isNonInteractive()
		return handleNewAssetFromVault(ctx, cmd, out, status, vault, assetName, promptInstall, opts)
	}
	if err != nil {
		return err
	}

	// Configure existing asset
	promptInstall := !opts.NoInstall && !opts.isNonInteractive()
	return configureFoundAsset(ctx, cmd, out, vault, foundAsset, promptInstall, opts)
}

// configureFoundAsset handles configuring an asset that was found in the lock file
func configureFoundAsset(ctx context.Context, cmd *cobra.Command, out *outputHelper, vault vaultpkg.Vault, foundAsset *lockfile.Asset, promptInstall bool, opts addOptions) error {
	out.printf("Configuring scope for %s@%s\n", foundAsset.Name, foundAsset.Version)

	// Normalize nil to empty slice for global installations
	currentScopes := foundAsset.Scopes
	if currentScopes == nil {
		currentScopes = []lockfile.Scope{}
	}

	// Get scopes (from flags if non-interactive, otherwise prompt)
	var scopes []lockfile.Scope
	var err error
	if opts.isNonInteractive() {
		scopes, err = opts.getScopes()
		if err != nil {
			return err
		}
	} else {
		scopes, err = promptForRepositories(out, foundAsset.Name, foundAsset.Version, currentScopes)
		if err != nil {
			return fmt.Errorf("failed to configure repositories: %w", err)
		}
	}

	// If nil, user chose to remove from installation
	if scopes == nil {
		return handleAssetRemoval(ctx, cmd, out, vault, foundAsset, promptInstall)
	}

	// Update asset with new repositories
	foundAsset.Scopes = scopes

	// Update lock file
	if err := updateLockFile(ctx, out, vault, foundAsset); err != nil {
		return fmt.Errorf("failed to update lock file: %w", err)
	}

	// Prompt to run install (if enabled)
	if promptInstall {
		promptRunInstall(cmd, ctx, out)
	}

	return nil
}

// promptRunInstall asks if the user wants to run install after adding an asset
func promptRunInstall(cmd *cobra.Command, ctx context.Context, out *outputHelper) {
	out.println()
	confirmed, err := components.ConfirmWithIO("Run install now to install the asset?", true, cmd.InOrStdin(), cmd.OutOrStdout())
	if err != nil {
		return
	}

	if !confirmed {
		out.println("Run 'sx install' when ready to install.")
		return
	}

	out.println()
	if err := runInstall(cmd, nil, false, "", false, ""); err != nil {
		out.printfErr("Install failed: %v\n", err)
	}
}

// createVault loads config and creates a vault instance
func createVault() (vaultpkg.Vault, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load configuration: %w\nRun 'sx init' to configure", err)
	}

	return vaultpkg.NewFromConfig(cfg)
}

// checkVersionAndContents queries vault for versions and checks if content is identical
func checkVersionAndContents(ctx context.Context, status *components.Status, vault vaultpkg.Vault, name string, zipData []byte) (version string, identical bool, err error) {
	status.Start("Checking for existing versions")
	versions, err := vault.GetVersionList(ctx, name)
	status.Clear()
	if err != nil {
		return "", false, fmt.Errorf("failed to get version list: %w", err)
	}

	version, identical, err = determineSuggestedVersionAndCheckIdentical(ctx, status, vault, name, versions, zipData)
	if err != nil {
		return "", false, err
	}

	return version, identical, nil
}

// handleIdenticalAsset handles the case when content is identical to existing version
func handleIdenticalAsset(ctx context.Context, out *outputHelper, status *components.Status, vault vaultpkg.Vault, name, version string, assetType asset.Type, opts addOptions) error {
	_ = status // status not needed for identical assets (no git operations)
	out.println()
	out.printf("✓ Asset %s@%s already exists in vault with identical contents\n", name, version)

	// Skip lock file update if --no-install
	if opts.NoInstall {
		return nil
	}

	// Get scopes (from flags if --yes, otherwise prompt)
	var scopes []lockfile.Scope
	var err error
	if opts.Yes {
		scopes, err = opts.getScopes()
		if err != nil {
			return err
		}
	} else {
		var currentScopes []lockfile.Scope
		lockFilePath := constants.SkillLockFile
		if existingArt, exists := lockfile.FindAsset(lockFilePath, name); exists {
			currentScopes = existingArt.Scopes
		}
		scopes, err = promptForRepositories(out, name, version, currentScopes)
		if err != nil {
			return fmt.Errorf("failed to configure repositories: %w", err)
		}
		// If nil, user chose not to install
		if scopes == nil {
			return nil
		}
	}

	// Update the lock file with asset
	lockAsset := &lockfile.Asset{
		Name:    name,
		Version: version,
		Type:    assetType,
		SourcePath: &lockfile.SourcePath{
			Path: fmt.Sprintf("./assets/%s/%s", name, version),
		},
		Scopes: scopes,
	}

	if err := updateLockFile(ctx, out, vault, lockAsset); err != nil {
		return fmt.Errorf("failed to update lock file: %w", err)
	}

	return nil
}

// addNewAsset adds a new or updated asset to the vault
func addNewAsset(ctx context.Context, out *outputHelper, status *components.Status, vault vaultpkg.Vault, name string, assetType asset.Type, version, zipFile string, zipData []byte, metadataExists bool, opts addOptions) error {
	// Prompt user for version (skip if --yes)
	if !opts.Yes {
		var err error
		version, err = promptForVersion(out, version)
		if err != nil {
			return err
		}
	}

	// Create full metadata with confirmed version
	meta := createMetadata(name, version, assetType, zipFile, zipData)

	// Always update metadata.toml to ensure version is correct
	zipData, err := updateMetadataInZip(meta, zipData, metadataExists)
	if err != nil {
		return err
	}

	// Create asset entry (what it is)
	lockAsset := &lockfile.Asset{
		Name:    meta.Asset.Name,
		Version: meta.Asset.Version,
		Type:    meta.Asset.Type,
		SourcePath: &lockfile.SourcePath{
			Path: fmt.Sprintf("./assets/%s/%s", meta.Asset.Name, meta.Asset.Version),
		},
	}

	// Check for context cancellation before vault upload
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Upload asset files to vault
	out.println()
	status.Start("Adding asset to vault")
	if err := vault.AddAsset(ctx, lockAsset, zipData); err != nil {
		status.Fail("Failed to add asset")
		return fmt.Errorf("failed to add asset: %w", err)
	}
	status.Done("")

	out.printf("✓ Successfully added %s@%s\n", meta.Asset.Name, meta.Asset.Version)

	// Skip lock file update if --no-install
	if opts.NoInstall {
		return nil
	}

	// Get scopes (from flags if --yes, otherwise prompt)
	var scopes []lockfile.Scope
	if opts.Yes {
		scopes, err = opts.getScopes()
		if err != nil {
			return err
		}
	} else {
		var currentScopes []lockfile.Scope
		lockFilePath := constants.SkillLockFile
		if existingArt, exists := lockfile.FindAsset(lockFilePath, lockAsset.Name); exists {
			currentScopes = existingArt.Scopes
		}
		scopes, err = promptForRepositories(out, lockAsset.Name, lockAsset.Version, currentScopes)
		if err != nil {
			return fmt.Errorf("failed to configure scopes: %w", err)
		}
		// If nil, user chose not to install
		if scopes == nil {
			return nil
		}
	}

	// Set scopes on asset
	lockAsset.Scopes = scopes

	// Update lock file with asset
	if err := updateLockFile(ctx, out, vault, lockAsset); err != nil {
		return fmt.Errorf("failed to update lock file: %w", err)
	}

	return nil
}

// promptForRepositories prompts user for repository configurations and returns them
// Takes currentRepos (nil if not installed, empty slice if global, or list of repos)
// Returns nil, nil if user chooses not to install (which removes it from lock file if present)
func promptForRepositories(out *outputHelper, assetName, version string, currentRepos []lockfile.Scope) ([]lockfile.Scope, error) {
	// Use the new UI components (they automatically fall back to simple text in non-TTY)
	styledOut := ui.NewOutput(out.cmd.OutOrStdout(), out.cmd.ErrOrStderr())
	ioc := components.NewIOContext(out.cmd.InOrStdin(), out.cmd.OutOrStdout())
	return promptForRepositoriesWithUI(assetName, version, currentRepos, styledOut, ioc)
}
