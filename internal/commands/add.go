package commands

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/config"
	"github.com/sleuth-io/sx/internal/github"
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/metadata"
	"github.com/sleuth-io/sx/internal/mgmt"
	"github.com/sleuth-io/sx/internal/ui/components"
	"github.com/sleuth-io/sx/internal/ui/theme"
	vaultpkg "github.com/sleuth-io/sx/internal/vault"
)

func addLongHelp() string {
	s := theme.Current().Styles()
	e := s.Emphasis.Render
	m := s.Muted.Render

	return `Add an asset from a local zip file, directory, URL, GitHub path, skills.sh, or marketplace.
If the argument is an existing asset name, configure its installation scope instead.

` + s.Header.Render("Examples:") + `
  ` + m("# Add from a local directory") + `
  ` + e("sx add ./my-skill") + `

  ` + m("# Add from skills.sh") + `
  ` + e("sx add anthropics/skills/frontend-design") + `
  ` + e("sx add vercel-labs/agent-skills") + `
  ` + e("sx add --browse") + `

  ` + m("# Configure scope for an existing asset") + `
  ` + e("sx add my-skill") + `

  ` + m("# Non-interactive with scope options") + `
  ` + e("sx add ./my-skill --yes") + `
  ` + e("sx add ./my-skill --yes --scope-repo git@github.com:org/repo.git") + `
  ` + e("sx add ./my-skill --yes --scope personal") + `

  ` + m("# Add to vault only, skip install") + `
  ` + e("sx add ./my-skill -y --no-install")
}

// NewAddCommand creates the add command
func NewAddCommand() *cobra.Command {
	var (
		yes          bool
		noInstall    bool
		browse       bool
		name         string
		assetType    string
		version      string
		org          bool
		repos        []string
		paths        []string
		teams        []string
		users        []string
		bots         []string
		replaceScope bool
		// legacy aliases
		scopeGlobal bool
		scopeRepos  []string
		scope       string
	)

	cmd := &cobra.Command{
		Use:   "add [source-or-asset-name]",
		Short: "Add an asset or configure an existing one",
		Long:  addLongHelp(),
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var input string
			if len(args) > 0 {
				input = args[0]
			}
			opts := addOptions{
				Yes:          yes,
				NoInstall:    noInstall,
				Browse:       browse,
				Name:         name,
				Type:         assetType,
				Version:      version,
				Org:          org,
				Repos:        repos,
				Paths:        paths,
				Teams:        teams,
				Users:        users,
				Bots:         bots,
				ReplaceScope: replaceScope,
				ScopeGlobal:  scopeGlobal,
				ScopeRepos:   scopeRepos,
				Scope:        scope,
			}
			return runAddWithFlags(cmd, input, opts)
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Accept all defaults and skip prompts (including the scope confirmation)")
	cmd.Flags().BoolVar(&noInstall, "no-install", false, "Skip running install after adding")
	cmd.Flags().BoolVar(&browse, "browse", false, "Search and browse skills from skills.sh")
	cmd.Flags().StringVar(&name, "name", "", "Override detected asset name")
	cmd.Flags().StringVar(&assetType, "type", "", "Override detected asset type (skill, rule, agent, command, mcp, hook)")
	cmd.Flags().StringVar(&version, "version", "", "Override suggested version")

	// Unified scope flags (same vocabulary as `sx install`). Setting any
	// pre-fills the scope and shows the same confirmation the menu does (skipped
	// with --yes). The named scopes are appended to the asset's existing scope
	// set by default; --replace-scope makes them the complete set instead.
	cmd.Flags().BoolVar(&org, "org", false, "Scope: install org-wide (global, exclusive — clears other scopes)")
	cmd.Flags().StringArrayVar(&repos, "repo", nil, "Scope: a repository URL (repeatable)")
	cmd.Flags().StringArrayVar(&paths, "path", nil, "Scope: a repo subpath set (repo_url#path1,path2; repeatable)")
	cmd.Flags().StringArrayVar(&teams, "team", nil, "Scope: every member of a team, by name (repeatable)")
	cmd.Flags().StringArrayVar(&users, "user", nil, "Scope: a user email, or 'me' (repeatable)")
	cmd.Flags().StringArrayVar(&bots, "bot", nil, "Scope: a bot identity, by name (repeatable)")
	cmd.Flags().BoolVar(&replaceScope, "replace-scope", false, "Replace the asset's whole scope set with the named scopes (default is to append)")

	// Legacy scope flags — forwarded to the unified set; kept for compatibility.
	cmd.Flags().BoolVar(&scopeGlobal, "scope-global", false, "Deprecated: use --org")
	cmd.Flags().StringArrayVar(&scopeRepos, "scope-repo", nil, "Deprecated: use --repo / --path")
	cmd.Flags().StringVar(&scope, "scope", "", "Deprecated: use --user me (for 'personal')")
	_ = cmd.Flags().MarkDeprecated("scope-global", "use --org")
	_ = cmd.Flags().MarkDeprecated("scope-repo", "use --repo or --path")
	_ = cmd.Flags().MarkDeprecated("scope", "use --user me")

	return cmd
}

// runAddWithFlags is the main entry point
func runAddWithFlags(cmd *cobra.Command, input string, opts addOptions) error {
	// Validate scope flags upfront.
	// --scope only ever supported the "personal" entity (Sleuth's "just for me",
	// now expressed as --user me); any other value silently did nothing, so
	// reject it explicitly rather than ignoring it.
	if opts.Scope != "" && opts.Scope != "personal" {
		return fmt.Errorf("--scope only accepts \"personal\" (deprecated; use --user me); got %q", opts.Scope)
	}
	if opts.Scope != "" && (opts.ScopeGlobal || len(opts.ScopeRepos) > 0) {
		return errors.New("cannot use --scope with --scope-global or --scope-repo")
	}
	if opts.ScopeGlobal && len(opts.ScopeRepos) > 0 {
		return errors.New("cannot use --scope-global with --scope-repo")
	}
	for _, repo := range opts.ScopeRepos {
		if strings.TrimSpace(repo) == "" {
			return errors.New("--scope-repo cannot be empty")
		}
	}

	// Handle --browse flag
	if opts.Browse {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		out := newOutputHelper(cmd)
		if browseCommunitySkills(cmd) {
			promptRunInstall(cmd, ctx, out)
		}
		return nil
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

	// Interactive menu when no input provided
	if input == "" && !opts.isNonInteractive() {
		if handled, err := promptAddMenu(cmd, ctx, out); handled || err != nil {
			return err
		}
	}

	// Try specialized handlers for specific input types
	if input != "" {
		if handled, err := routeSpecializedInput(ctx, cmd, out, status, input, opts); handled || err != nil {
			return err
		}
	}

	// Fall through to zip-based asset add
	return addFromZipSource(ctx, cmd, out, status, input, opts)
}

// routeSpecializedInput checks if input matches a specialized handler (skills.sh, marketplace,
// existing asset, remote MCP, instruction file) and routes to it.
// Returns (true, err) if handled, (false, nil) if the caller should fall through to zip handling.
func routeSpecializedInput(ctx context.Context, cmd *cobra.Command, out *outputHelper, status *components.Status, input string, opts addOptions) (bool, error) {
	// skills.sh reference (owner/repo or owner/repo/skill or skills.sh:...)
	if strings.HasPrefix(input, "skills.sh:") || isSkillsShReference(input) {
		return true, addFromSkillsSh(cmd, input, opts)
	}

	// plugin@marketplace syntax
	if IsMarketplaceReference(input) {
		promptInstall := !opts.NoInstall && !opts.Yes
		return true, addFromMarketplace(ctx, cmd, out, status, input, promptInstall, opts)
	}

	// Existing asset name (not a file, directory, or URL)
	if !isURL(input) && !github.IsTreeURL(input) {
		if _, err := os.Stat(input); os.IsNotExist(err) {
			// If the named asset is installed locally, treat the on-disk
			// directory as the source. This keeps `sx add <name>` and
			// `sx add <path-to-installed-dir>` behaviorally equivalent —
			// either form picks up local edits and uploads a new version
			// when the content differs from the latest in the vault.
			if installedPath := resolveInstalledAssetPath(ctx, input); installedPath != "" {
				return true, addFromZipSource(ctx, cmd, out, status, installedPath, opts)
			}
			return true, configureExistingAsset(ctx, cmd, out, status, input, opts)
		}
	}

	// Remote MCP URL
	if isRemoteMCPURL(input) {
		return true, addRemoteMCP(ctx, cmd, out, status, input, opts)
	}

	// Instruction file (CLAUDE.md, AGENTS.md)
	if isInstructionFile(input) {
		if opts.isNonInteractive() {
			return true, errors.New("instruction files require interactive mode (multiple sections may need selection)")
		}
		promptInstall := !opts.NoInstall
		return true, addFromInstructionFile(ctx, cmd, out, status, input, promptInstall)
	}

	return false, nil
}

// addFromZipSource handles adding an asset from a zip file, directory, or GitHub URL.
func addFromZipSource(ctx context.Context, cmd *cobra.Command, out *outputHelper, status *components.Status, input string, opts addOptions) error {
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

	// Normalize the prompt file to the canonical uppercase form (e.g. skill.md
	// -> SKILL.md). Clients install the zip verbatim and the metadata spec
	// defines the uppercase form as canonical, so this guarantees every client
	// sees a filename it recognizes regardless of how the asset was authored.
	// If a user-supplied metadata.toml also declares the lowercase form, its
	// prompt-file field is rewritten to match — the file and metadata are kept
	// in lockstep.
	zipData, err = normalizePromptFileCase(zipData, assetType)
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
	prOpened := false
	if contentsIdentical {
		addErr = handleIdenticalAsset(ctx, out, status, vault, name, version, assetType, opts)
	} else {
		prOpened, addErr = addNewAsset(ctx, out, status, vault, name, assetType, version, zipFile, zipData, metadataExists, opts)
	}

	if addErr != nil {
		return addErr
	}

	// A pull request was opened instead of a direct publish: the asset isn't live
	// until the PR merges, so there's nothing to install now.
	if prOpened {
		return nil
	}

	// Handle install: auto-run if --yes, prompt if interactive, skip if --no-install
	if opts.Yes && !opts.NoInstall {
		out.println()
		if err := runInstall(cmd, nil, false, "", false, "", "", false, false); err != nil {
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

	// Read the real current installation from the vault (the server view for
	// Sleuth, which includes team/user/bot scopes). The asset is already in the
	// lock file, so if that read can't see it, fall back to the lock-file
	// scopes and still treat it as installed.
	current, installed := resolveCurrentTargets(ctx, vault, foundAsset.Name)
	if !installed {
		current, installed = scopesToTargets(foundAsset.Scopes), true
	}

	// Get scopes: scope flags pre-fill + confirm, otherwise the interactive editor.
	result, err := resolveAddScope(out, vault, foundAsset.Name, foundAsset.Version, current, installed, opts)
	if err != nil {
		return fmt.Errorf("failed to configure repositories: %w", err)
	}

	// If remove, user chose to remove from installation
	if result.Remove {
		return handleAssetRemoval(ctx, cmd, out, vault, foundAsset, promptInstall)
	}

	// If inherit, preserve existing installations
	if result.Inherit {
		if err := inheritLockFile(ctx, out, vault, foundAsset); err != nil {
			return fmt.Errorf("failed to inherit installations: %w", err)
		}
		out.printf("✓ Preserved existing scope for %s@%s\n", foundAsset.Name, foundAsset.Version)
		return nil
	}

	// Update asset with new repositories
	foundAsset.Scopes = result.Scopes

	// Update lock file
	if err := updateLockFile(ctx, out, vault, foundAsset, result); err != nil {
		return fmt.Errorf("failed to update lock file: %w", err)
	}

	out.printf("✓ Updated scope for %s@%s\n", foundAsset.Name, foundAsset.Version)

	// Prompt to run install (if enabled)
	if promptInstall {
		promptRunInstall(cmd, ctx, out)
	}

	return nil
}

// promptAddMenu shows an interactive menu when sx add is called with no arguments.
// Returns (true, nil) if the user browsed and added assets,
// (false, nil) if the user chose "manual" or browse produced nothing (caller should continue),
// or (false, err) on error.
func promptAddMenu(cmd *cobra.Command, ctx context.Context, out *outputHelper) (bool, error) {
	selected, err := components.Select("How would you like to add an asset?", []components.Option{
		{Label: "Browse skills.sh", Value: "browse"},
		{Label: "Enter path or URL", Value: "manual"},
	})
	if err != nil {
		return false, err
	}
	if selected.Value == "browse" {
		browsedAny := browseCommunitySkills(cmd)
		if browsedAny {
			promptRunInstall(cmd, ctx, out)
		}
		return browsedAny, nil
	}
	return false, nil
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
	if err := runInstall(cmd, nil, false, "", false, "", "", false, false); err != nil {
		out.printfErr("Install failed: %v\n", err)
	}
}

// createVault loads config and creates a vault instance
func createVault() (vaultpkg.Vault, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load configuration: %w\nRun 'sx init' to configure", err)
	}

	// Apply the active profile's identity before any vault op so "me"/user
	// scopes and audit entries resolve to the profile's configured email
	// rather than the system git config. The install paths do this via
	// loadConfigAndVault; sx add must do it too or --profile <git vault> falls
	// back to git config user.email (SD-10170). Only override when the profile
	// actually sets an identity — an empty one must NOT clobber the git-config
	// fallback (the process-global override is sticky).
	if cfg.Identity != "" {
		mgmt.SetIdentityOverride(cfg.Identity)
	}
	mgmt.SetAuditProfileTag(cfg.ProfileName)

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

	// Build lock asset
	lockAsset := &lockfile.Asset{
		Name:    name,
		Version: version,
		Type:    assetType,
		SourcePath: &lockfile.SourcePath{
			Path: fmt.Sprintf("./assets/%s/%s", name, version),
		},
	}

	// --no-install: write lock file but skip install prompt. Honors any
	// scope flags so batch flows can pin assets to a target repo/path/scope.
	if opts.NoInstall {
		return writeLockFileForNoInstall(ctx, out, vault, lockAsset, opts)
	}

	// Get scopes (from flags if --yes, otherwise prompt)
	var result *scopeResult
	var err error
	current, installed := resolveCurrentTargets(ctx, vault, name)
	result, err = resolveAddScope(out, vault, name, version, current, installed, opts)
	if err != nil {
		return fmt.Errorf("failed to configure repositories: %w", err)
	}
	if result.Remove {
		out.printf("Run 'sx add %s' to configure where to install it.\n", name)
		return nil
	}

	// If inherit, preserve existing installations
	if result.Inherit {
		if err := inheritLockFile(ctx, out, vault, lockAsset); err != nil {
			return fmt.Errorf("failed to inherit installations: %w", err)
		}
		out.printf("✓ Preserved existing scope for %s@%s\n", name, version)
		return nil
	}

	lockAsset.Scopes = result.Scopes
	if err := updateLockFile(ctx, out, vault, lockAsset, result); err != nil {
		return fmt.Errorf("failed to update lock file: %w", err)
	}

	return nil
}

// addNewAsset adds a new or updated asset to the vault. It returns prOpened=true
// when the RBAC edit gate diverted the add into a pull request instead of a
// direct publish — in that case the asset isn't live yet, so the caller must not
// offer to install it.
func addNewAsset(ctx context.Context, out *outputHelper, status *components.Status, vault vaultpkg.Vault, name string, assetType asset.Type, version, zipFile string, zipData []byte, metadataExists bool, opts addOptions) (prOpened bool, err error) {
	// RBAC edit gate: a skill scoped to a team may only be re-published by a
	// member of that team (or an org-admin); a non-team-scoped or brand-new
	// skill is open to anyone. See docs/rbac.md.
	//
	// When the gate blocks a git vault we don't just fail: the caller can still
	// push to a branch, so we offer to open a pull request and route the rest of
	// the add through that branch instead of the default branch.
	var prv prVault
	if permErr := enforceAssetEditPermission(ctx, vault, name); permErr != nil {
		var denial *vaultpkg.AssetEditPermissionError
		pv, prCapable := vault.(prVault)
		if !errors.As(permErr, &denial) || !prCapable {
			return false, permErr
		}
		open, askErr := promptOpenPR(out, opts.Yes, denial)
		if askErr != nil {
			return false, askErr
		}
		if !open {
			return false, permErr
		}
		prv = pv
	}

	// Prompt user for version (skip if --yes)
	if !opts.Yes {
		version, err = promptForVersion(out, version)
		if err != nil {
			return false, err
		}
	}

	// Create full metadata with confirmed version
	meta := createMetadata(name, version, assetType, zipFile, zipData)

	// Always update metadata.toml to ensure version is correct
	zipData, err = updateMetadataInZip(meta, zipData, metadataExists)
	if err != nil {
		return false, err
	}

	// Validate the (possibly user-authored) metadata before it enters the
	// vault. Catches problems like unknown hook events that would otherwise
	// pass `sx add` and only fail at install time. ValidateZip already wraps
	// its errors with a "metadata validation failed:" prefix.
	if err := metadata.ValidateZip(zipData, &assetType); err != nil {
		return false, err
	}

	// Create asset entry (what it is). Mirror metadata's client filter into
	// the lockfile so isAssetApplicable / MatchesClient see the restriction
	// without re-reading the metadata.toml at install time.
	lockAsset := &lockfile.Asset{
		Name:    meta.Asset.Name,
		Version: meta.Asset.Version,
		Type:    meta.Asset.Type,
		Clients: append([]string(nil), meta.Asset.Clients...),
		SourcePath: &lockfile.SourcePath{
			Path: fmt.Sprintf("./assets/%s/%s", meta.Asset.Name, meta.Asset.Version),
		},
	}

	// Check for context cancellation before vault upload
	if ctx.Err() != nil {
		return false, ctx.Err()
	}

	// PR fallback: stage the add on a branch instead of the default branch, then
	// push it and open a pull request. The asset isn't published (or installed
	// locally) until the PR is merged, so we stop after creating it.
	if prv != nil {
		return true, addViaPullRequest(ctx, out, status, prv, vault, lockAsset, zipData)
	}

	// Upload asset files to vault
	out.println()
	status.Start("Adding asset to vault")
	if err := vault.AddAsset(ctx, lockAsset, zipData); err != nil {
		status.Fail("Failed to add asset")
		return false, fmt.Errorf("failed to add asset: %w", err)
	}
	status.Done("")

	out.printf("✓ Successfully added %s@%s\n", meta.Asset.Name, meta.Asset.Version)

	// --no-install: write lock file but skip install prompt. Honors any
	// scope flags so batch flows can pin assets to a target repo/path/scope.
	if opts.NoInstall {
		return false, writeLockFileForNoInstall(ctx, out, vault, lockAsset, opts)
	}

	// Get scopes (from flags if --yes, otherwise prompt)
	var result *scopeResult
	current, installed := resolveCurrentTargets(ctx, vault, lockAsset.Name)
	result, err = resolveAddScope(out, vault, lockAsset.Name, lockAsset.Version, current, installed, opts)
	if err != nil {
		return false, fmt.Errorf("failed to configure scopes: %w", err)
	}
	// If remove, user chose not to install
	if result.Remove {
		out.printf("Run 'sx add %s' to configure where to install it.\n", lockAsset.Name)
		return false, nil
	}

	// If inherit, preserve existing installations
	if result.Inherit {
		if err := inheritLockFile(ctx, out, vault, lockAsset); err != nil {
			return false, fmt.Errorf("failed to inherit installations: %w", err)
		}
		return false, nil
	}

	// Set scopes on asset
	lockAsset.Scopes = result.Scopes

	// Update lock file with asset
	if err := updateLockFile(ctx, out, vault, lockAsset, result); err != nil {
		return false, fmt.Errorf("failed to update lock file: %w", err)
	}

	return false, nil
}

// promptOpenPR explains why a direct publish was blocked and asks whether to open
// a pull request instead. With --yes it proceeds without prompting.
func promptOpenPR(out *outputHelper, assumeYes bool, permErr *vaultpkg.AssetEditPermissionError) (bool, error) {
	out.println()
	out.printf("You don't have permission to publish %q directly — it's scoped to team %s.\n",
		permErr.Asset, strings.Join(permErr.Teams, ", "))
	if assumeYes {
		return true, nil
	}
	return out.prompter.Confirm("Open a pull request with your changes instead?")
}

// addViaPullRequest stages the asset add on a branch and opens a pull request for
// it, returning after reporting the PR URL. Used when the RBAC edit gate blocks a
// direct publish but the vault can still push to a branch. The asset is not
// published or installed locally until the PR is merged.
func addViaPullRequest(ctx context.Context, out *outputHelper, status *components.Status, prv prVault, vault vaultpkg.Vault, lockAsset *lockfile.Asset, zipData []byte) error {
	branch := prBranchName(lockAsset.Name, lockAsset.Version)
	if err := prv.StartPRBranch(ctx, branch); err != nil {
		return fmt.Errorf("failed to start PR branch: %w", err)
	}

	// FinishPRBranch restores the clone to its base branch itself; this defer
	// covers every earlier exit (a failed AddAsset, a cancelled context) so the
	// shared clone is never left stranded on the PR branch with a local commit.
	finished := false
	defer func() {
		if !finished {
			_ = prv.AbortPRBranch(ctx)
		}
	}()

	out.println()
	status.Start("Preparing pull request")
	if err := vault.AddAsset(ctx, lockAsset, zipData); err != nil {
		status.Fail("Failed to prepare pull request")
		return fmt.Errorf("failed to add asset: %w", err)
	}

	title := fmt.Sprintf("Add %s %s", lockAsset.Name, lockAsset.Version)
	body := fmt.Sprintf("Adds %s@%s via `sx add`.", lockAsset.Name, lockAsset.Version)
	res, err := prv.FinishPRBranch(ctx, title, body)
	finished = true // FinishPRBranch owns the restore from here, even on error.
	if err != nil {
		status.Fail("Failed to open pull request")
		return fmt.Errorf("failed to open pull request: %w", err)
	}
	status.Done("")

	if res.Created {
		out.printf("✓ Opened pull request for %s@%s\n", lockAsset.Name, lockAsset.Version)
		if res.URL != "" {
			out.printf("  %s\n", res.URL)
		}
		out.println("It will be published once the pull request is merged.")
		return nil
	}

	// The branch was pushed but no PR was opened — tell the user why and give them
	// the compare link to finish it by hand, so the outcome isn't reported as done.
	out.printf("Pushed branch for %s@%s, but couldn't open the pull request automatically", lockAsset.Name, lockAsset.Version)
	if res.Fallback != "" {
		out.printf(" (%s)", res.Fallback)
	}
	out.println(".")
	out.println("Open it manually here:")
	if res.URL != "" {
		out.printf("  %s\n", res.URL)
	}
	out.println("It will be published once the pull request is merged.")
	return nil
}

// prBranchName builds a filesystem- and git-safe branch name for an add PR, e.g.
// "sx/add-my-skill-1.2.0-a1b2c3". The random suffix keeps the branch unique per
// attempt: two people adding the same asset/version — or one person retrying —
// each get their own branch, so FinishPRBranch never has to force-push and can't
// clobber a branch (or open PR) someone else already pushed. See docs/rbac.md.
func prBranchName(name, version string) string {
	return "sx/add-" + sanitizeBranchComponent(name) + "-" + sanitizeBranchComponent(version) + "-" + randomBranchSuffix()
}

// randomBranchSuffix returns a short hex token used to make PR branch names
// unique. It falls back to a fixed token only if the system RNG is unavailable,
// which is vanishingly unlikely; even then the non-forcing push simply fails
// loudly on a collision rather than overwriting anything.
func randomBranchSuffix() string {
	var b [3]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "branch"
	}
	return hex.EncodeToString(b[:])
}

// sanitizeBranchComponent keeps git-ref-safe characters and replaces the rest
// with a hyphen, trimming leading/trailing hyphens.
func sanitizeBranchComponent(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}
