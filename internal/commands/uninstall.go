package commands

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/assets"
	"github.com/sleuth-io/sx/internal/bootstrap"
	"github.com/sleuth-io/sx/internal/cache"
	"github.com/sleuth-io/sx/internal/clients"
	"github.com/sleuth-io/sx/internal/config"
	"github.com/sleuth-io/sx/internal/gitutil"
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/logger"
	"github.com/sleuth-io/sx/internal/ui"
	"github.com/sleuth-io/sx/internal/ui/components"
	vaultpkg "github.com/sleuth-io/sx/internal/vault"
)

// NewUninstallCommand creates the uninstall command
func NewUninstallCommand() *cobra.Command {
	var all bool
	var yes bool
	var dryRun bool
	var verbose bool
	var clientsFlag string

	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Uninstall all assets from the current scope or all scopes",
		Long: `Uninstall removes all installed assets from the current scope (global, repository, or path).

Examples:
  # Uninstall from current scope (prompts for confirmation)
  sx uninstall

  # Uninstall from all scopes (global + all repositories)
  sx uninstall --all

  # Preview what would be uninstalled without making changes
  sx uninstall --dry-run

  # Skip confirmation prompt
  sx uninstall --yes

  # Uninstall from all scopes without confirmation
  sx uninstall --all --yes`,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts := UninstallOptions{
				All:         all,
				Yes:         yes,
				DryRun:      dryRun,
				Verbose:     verbose,
				ClientsFlag: clientsFlag,
			}
			return runUninstall(cmd, args, opts)
		},
	}

	cmd.Flags().BoolVar(&all, "all", false, "Uninstall from all scopes (global + all repositories)")
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be uninstalled without removing")
	cmd.Flags().BoolVar(&verbose, "verbose", false, "Verbose output")
	cmd.Flags().StringVar(&clientsFlag, "clients", "", "Comma-separated client IDs to uninstall from (e.g., 'claude-code,cursor')")

	return cmd
}

// UninstallOptions contains options for the uninstall command
type UninstallOptions struct {
	All         bool
	Yes         bool
	DryRun      bool
	Verbose     bool
	ClientsFlag string
}

// AssetUninstallPlan contains info needed to uninstall one asset
type AssetUninstallPlan struct {
	Name      string
	Version   string
	Type      asset.Type
	IsGlobal  bool
	Clients   []string // client IDs that have this installed
	LockEntry *lockfile.Asset
}

// UninstallPlan contains the complete uninstall plan
type UninstallPlan struct {
	Assets     []AssetUninstallPlan
	GitContext *gitutil.GitContext
	TargetBase string // tracking base for updating tracker
}

// UninstallResult tracks what was uninstalled
type UninstallResult struct {
	AssetName string
	ClientID  string
	Success   bool
	Error     error
}

// runUninstall executes the uninstall command
func runUninstall(cmd *cobra.Command, args []string, opts UninstallOptions) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	out := newOutputHelper(cmd)
	styledOut := ui.NewOutput(cmd.OutOrStdout(), cmd.ErrOrStderr())

	// Step 1: Load lock file
	lockFile, err := loadLockFileForUninstall(ctx, out)
	if err != nil {
		// If --all is passed, we should still remove system hooks even if lock file fails
		if opts.All {
			styledOut.Warning(fmt.Sprintf("Warning: %v", err))
			return handleAllFlagWithoutAssets(ctx, opts, styledOut)
		}
		return err
	}

	// Step 2: Detect context and load tracker
	gitContext, tracker, trackingBase, err := loadTrackerForUninstall(ctx, out)
	if err != nil {
		// If --all is passed, we should still remove system hooks even if tracker fails
		if opts.All {
			styledOut.Warning(fmt.Sprintf("Warning: %v", err))
			return handleAllFlagWithoutAssets(ctx, opts, styledOut)
		}
		return err
	}

	if len(tracker.Assets) == 0 {
		// No assets, but if --all is passed, still remove system hooks
		if opts.All {
			return handleAllFlagWithoutAssets(ctx, opts, styledOut)
		}
		styledOut.Info("No assets installed")
		return nil
	}

	// Step 3: Build uninstall plan
	plan := buildUninstallPlanFromTracker(lockFile, tracker, gitContext, trackingBase)

	if len(plan.Assets) == 0 {
		// No assets to uninstall, but if --all is passed, still remove system hooks
		if opts.All {
			return handleAllFlagWithoutAssets(ctx, opts, styledOut)
		}
		styledOut.Info("No assets to uninstall")
		return nil
	}

	// Step 4: Filter plan by client flag if provided
	if opts.ClientsFlag != "" {
		plan = filterUninstallPlanByClients(plan, opts.ClientsFlag)
		if len(plan.Assets) == 0 {
			styledOut.Info("No assets to uninstall for specified client(s)")
			return nil
		}
	}

	// Step 5: Display plan and confirm
	displayUninstallPlan(plan, styledOut)

	if opts.All {
		styledOut.Info("System hooks will also be removed (--all flag)")
	}

	if !opts.Yes && !opts.DryRun {
		if !confirmUninstall(styledOut) {
			styledOut.Muted("Uninstall cancelled")
			return nil
		}
	}

	if opts.DryRun {
		styledOut.Muted("\nNo changes made (dry run).")
		return nil
	}

	// Step 5: Execute uninstall
	styledOut.Newline()
	styledOut.Header("Uninstalling assets...")
	results := executeUninstall(ctx, plan, opts, styledOut)

	// Step 6: Update tracker
	if err := updateTracker(results, plan, out); err != nil {
		out.printfErr("Warning: failed to update tracker: %v\n", err)
		logger.Get().Error("failed to update tracker", "error", err)
	}

	// Step 7: Regenerate client support
	regenerateClientSupport(ctx, plan, results, out)

	// Step 8: Uninstall system hooks if --all flag is passed
	if opts.All {
		uninstallSystemHooks(ctx, styledOut)
	}

	// Step 9: Report results
	reportResults(results, styledOut)

	return nil
}

// handleAllFlagWithoutAssets handles the --all flag when there are no assets to uninstall
func handleAllFlagWithoutAssets(ctx context.Context, opts UninstallOptions, styledOut *ui.Output) error {
	styledOut.Info("No assets to uninstall")

	if !opts.Yes && !opts.DryRun {
		styledOut.Info("System hooks will be removed (--all flag)")
		if !confirmUninstall(styledOut) {
			styledOut.Muted("Uninstall cancelled")
			return nil
		}
	}

	if opts.DryRun {
		styledOut.Muted("Would remove system hooks (dry run).")
		return nil
	}

	uninstallSystemHooks(ctx, styledOut)
	styledOut.Success("System hooks removed")
	return nil
}

// loadLockFileForUninstall loads config and fetches the lock file
func loadLockFileForUninstall(ctx context.Context, out *outputHelper) (*lockfile.LockFile, error) {
	status := components.NewStatus(out.cmd.OutOrStdout())

	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load configuration: %w\nRun 'sx init' to configure", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	vault, err := vaultpkg.NewFromConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create vault: %w", err)
	}

	status.Start("Fetching lock file")

	var cachedETag string
	var vaultURL string
	if cfg.Type == config.RepositoryTypeSleuth {
		vaultURL = cfg.GetServerURL()
		cachedETag, _ = cache.LoadETag(vaultURL)
	}

	lockFileData, _, notModified, err := vault.GetLockFile(ctx, cachedETag)
	if err != nil {
		status.Fail("Failed to fetch lock file")
		return nil, fmt.Errorf("failed to fetch lock file: %w", err)
	}

	if notModified && vaultURL != "" {
		lockFileData, err = cache.LoadLockFile(vaultURL)
		if err != nil {
			status.Fail("Failed to load cached lock file")
			return nil, fmt.Errorf("failed to load cached lock file: %w", err)
		}
	}

	lockFile, err := lockfile.Parse(lockFileData)
	if err != nil {
		status.Fail("Failed to parse lock file")
		return nil, fmt.Errorf("failed to parse lock file: %w", err)
	}

	status.Done("")
	return lockFile, nil
}

// loadTrackerForUninstall detects git context and loads the installation tracker
func loadTrackerForUninstall(ctx context.Context, out *outputHelper) (*gitutil.GitContext, *assets.Tracker, string, error) {
	status := components.NewStatus(out.cmd.OutOrStdout())

	gitContext, err := gitutil.DetectContext(ctx)
	if err != nil {
		return nil, nil, "", fmt.Errorf("failed to detect git context: %w", err)
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, nil, "", fmt.Errorf("failed to get home directory: %w", err)
	}

	// Same tracking base logic as install
	trackingBase := filepath.Join(homeDir, ".claude")
	if gitContext.IsRepo {
		trackingBase = filepath.Join(gitContext.RepoRoot, ".claude")
	}

	status.Start("Loading installation state")
	tracker, err := assets.LoadTracker()
	if err != nil {
		status.Fail("Failed to load tracker")
		return nil, nil, "", fmt.Errorf("failed to load tracker: %w", err)
	}
	status.Done("")

	return gitContext, tracker, trackingBase, nil
}

// buildUninstallPlanFromTracker creates the uninstall plan by matching tracker with lock file
func buildUninstallPlanFromTracker(lockFile *lockfile.LockFile, tracker *assets.Tracker, gitContext *gitutil.GitContext, trackingBase string) UninstallPlan {
	log := logger.Get()

	// Build asset lookup from lock file
	lockFileMap := make(map[string]*lockfile.Asset)
	for i := range lockFile.Assets {
		lockFileMap[lockFile.Assets[i].Name] = &lockFile.Assets[i]
	}

	plan := UninstallPlan{
		GitContext: gitContext,
		TargetBase: trackingBase,
	}

	for _, installed := range tracker.Assets {
		lockEntry, found := lockFileMap[installed.Name]
		if !found {
			// Asset in tracker but not in lock file - still uninstall it
			log.Warn("asset in tracker but not in lock file", "name", installed.Name)
			// Determine type from lock file if possible
			assetType := asset.TypeSkill // Default to skill
			plan.Assets = append(plan.Assets, AssetUninstallPlan{
				Name:     installed.Name,
				Version:  installed.Version,
				Type:     assetType,
				IsGlobal: installed.IsGlobal(),
				Clients:  installed.Clients,
			})
			continue
		}

		plan.Assets = append(plan.Assets, AssetUninstallPlan{
			Name:      installed.Name,
			Version:   installed.Version,
			Type:      lockEntry.Type,
			IsGlobal:  lockEntry.IsGlobal(),
			Clients:   installed.Clients,
			LockEntry: lockEntry,
		})
	}

	return plan
}

// filterUninstallPlanByClients filters the plan to only include specified clients
func filterUninstallPlanByClients(plan UninstallPlan, clientsFlag string) UninstallPlan {
	wantedClients := make(map[string]bool)
	for id := range strings.SplitSeq(clientsFlag, ",") {
		id = strings.TrimSpace(id)
		if id != "" {
			wantedClients[id] = true
		}
	}

	var filteredAssets []AssetUninstallPlan
	for _, asset := range plan.Assets {
		var filteredClients []string
		for _, clientID := range asset.Clients {
			if wantedClients[clientID] {
				filteredClients = append(filteredClients, clientID)
			}
		}
		if len(filteredClients) > 0 {
			asset.Clients = filteredClients
			filteredAssets = append(filteredAssets, asset)
		}
	}

	plan.Assets = filteredAssets
	return plan
}

// executeUninstall performs the actual uninstallation
func executeUninstall(ctx context.Context, plan UninstallPlan, opts UninstallOptions, styledOut *ui.Output) []UninstallResult {
	var results []UninstallResult
	registry := clients.Global()

	for _, assetPlan := range plan.Assets {
		for _, clientID := range assetPlan.Clients {
			result := uninstallAssetFromClient(ctx, assetPlan, clientID, plan.GitContext, opts, registry, styledOut)
			results = append(results, result)
		}
	}

	return results
}

// uninstallAssetFromClient removes a single asset from a single client
func uninstallAssetFromClient(ctx context.Context, assetPlan AssetUninstallPlan, clientID string, gitContext *gitutil.GitContext, opts UninstallOptions, registry *clients.Registry, styledOut *ui.Output) UninstallResult {
	client, err := registry.Get(clientID)
	if err != nil || client == nil {
		styledOut.ErrorItem(fmt.Sprintf("Client %s not found, skipping", clientID))
		return UninstallResult{
			AssetName: assetPlan.Name,
			ClientID:  clientID,
			Success:   false,
			Error:     fmt.Errorf("client not found: %s", clientID),
		}
	}

	// Build the correct scope based on asset's scope from lock file
	installScope := buildScopeForAsset(assetPlan, gitContext)

	req := clients.UninstallRequest{
		Assets: []asset.Asset{
			{
				Name:    assetPlan.Name,
				Version: assetPlan.Version,
				Type:    assetPlan.Type,
			},
		},
		Scope: installScope,
		Options: clients.UninstallOptions{
			DryRun:  opts.DryRun,
			Verbose: opts.Verbose,
		},
	}

	resp, err := client.UninstallAssets(ctx, req)
	if err != nil {
		styledOut.ErrorItem(fmt.Sprintf("Failed to uninstall %s from %s: %v", assetPlan.Name, client.DisplayName(), err))
		return UninstallResult{
			AssetName: assetPlan.Name,
			ClientID:  clientID,
			Success:   false,
			Error:     err,
		}
	}

	success := len(resp.Results) > 0 && resp.Results[0].Status == clients.StatusSuccess
	if success {
		styledOut.SuccessItem(fmt.Sprintf("Removed %s from %s", assetPlan.Name, client.DisplayName()))
	} else {
		errMsg := "unknown error"
		if len(resp.Results) > 0 && resp.Results[0].Error != nil {
			errMsg = resp.Results[0].Error.Error()
		}
		styledOut.ErrorItem(fmt.Sprintf("Failed to remove %s from %s: %s", assetPlan.Name, client.DisplayName(), errMsg))
	}

	return UninstallResult{
		AssetName: assetPlan.Name,
		ClientID:  clientID,
		Success:   success,
	}
}

// buildScopeForAsset creates the correct InstallScope based on asset's scope
func buildScopeForAsset(assetPlan AssetUninstallPlan, gitContext *gitutil.GitContext) *clients.InstallScope {
	if assetPlan.IsGlobal {
		return &clients.InstallScope{
			Type: clients.ScopeGlobal,
		}
	}

	// Non-global asset - use repo scope
	return &clients.InstallScope{
		Type:     clients.ScopeRepository,
		RepoRoot: gitContext.RepoRoot,
		RepoURL:  gitContext.RepoURL,
	}
}

// updateTracker removes successfully uninstalled clients from tracker
// If an asset has no remaining clients, the asset is fully removed
func updateTracker(results []UninstallResult, plan UninstallPlan, out *outputHelper) error {
	status := components.NewStatus(out.cmd.OutOrStdout())

	// Build a map of successful removals: assetName -> set of removed clients
	removedClients := make(map[string]map[string]bool)
	for _, result := range results {
		if result.Success {
			if removedClients[result.AssetName] == nil {
				removedClients[result.AssetName] = make(map[string]bool)
			}
			removedClients[result.AssetName][result.ClientID] = true
		}
	}

	if len(removedClients) == 0 {
		return nil
	}

	status.Start("Updating installation state")
	tracker, err := assets.LoadTracker()
	if err != nil {
		status.Fail("Failed to load tracker")
		return fmt.Errorf("failed to load tracker: %w", err)
	}

	// Update each asset: remove uninstalled clients, or remove entire asset if no clients remain
	for assetName, clientsRemoved := range removedClients {
		for i := range tracker.Assets {
			if tracker.Assets[i].Name != assetName {
				continue
			}

			// Filter out removed clients
			var remainingClients []string
			for _, c := range tracker.Assets[i].Clients {
				if !clientsRemoved[c] {
					remainingClients = append(remainingClients, c)
				}
			}

			if len(remainingClients) == 0 {
				// No clients left, remove the entire asset
				tracker.RemoveAsset(tracker.Assets[i].Key())
			} else {
				// Update with remaining clients
				tracker.Assets[i].Clients = remainingClients
			}
			break
		}
	}

	if len(tracker.Assets) == 0 {
		err = assets.DeleteTracker()
	} else {
		err = assets.SaveTracker(tracker)
	}

	if err != nil {
		status.Fail("Failed to update tracker")
		return err
	}
	status.Done("")
	return nil
}

// regenerateClientSupport calls EnsureAssetSupport on affected clients
func regenerateClientSupport(ctx context.Context, plan UninstallPlan, results []UninstallResult, out *outputHelper) {
	affectedClients := make(map[string]bool)
	for _, result := range results {
		if result.Success {
			affectedClients[result.ClientID] = true
		}
	}

	registry := clients.Global()
	for clientID := range affectedClients {
		client, err := registry.Get(clientID)
		if err != nil || client == nil {
			continue
		}

		scope := &clients.InstallScope{Type: clients.ScopeGlobal}
		if plan.GitContext.IsRepo {
			scope = &clients.InstallScope{
				Type:     clients.ScopeRepository,
				RepoRoot: plan.GitContext.RepoRoot,
				RepoURL:  plan.GitContext.RepoURL,
			}
		}

		if err := client.EnsureAssetSupport(ctx, scope); err != nil {
			out.printfErr("Warning: failed to regenerate support for %s: %v\n", client.DisplayName(), err)
		}
	}
}

// displayUninstallPlan shows what will be uninstalled
func displayUninstallPlan(plan UninstallPlan, styledOut *ui.Output) {
	styledOut.Newline()
	styledOut.SubHeader("The following assets will be uninstalled:")

	for _, a := range plan.Assets {
		scopeDesc := styledOut.MutedText("global")
		if !a.IsGlobal {
			scopeDesc = styledOut.MutedText("repository")
		}
		clientNames := styledOut.EmphasisText(strings.Join(a.Clients, ", "))
		styledOut.Printf("  %s %s v%s [%s] → %s\n",
			styledOut.BoldText(a.Name),
			styledOut.MutedText("("+a.Type.Label+")"),
			a.Version,
			scopeDesc,
			clientNames)
	}

	styledOut.Newline()
}

// confirmUninstall prompts user for confirmation
func confirmUninstall(styledOut *ui.Output) bool {
	reader := bufio.NewReader(os.Stdin)
	styledOut.Printf("Continue with uninstall? [y/N]: ")

	response, err := reader.ReadString('\n')
	if err != nil {
		return false
	}

	response = strings.ToLower(strings.TrimSpace(response))
	return response == "y" || response == "yes"
}

// uninstallSystemHooks removes system hooks from all installed clients
func uninstallSystemHooks(ctx context.Context, styledOut *ui.Output) {
	styledOut.Newline()
	styledOut.Header("Removing system hooks...")

	registry := clients.Global()
	installedClients := registry.DetectInstalled()

	// Load config to get enabled bootstrap options
	mpc, _ := config.LoadMultiProfile()

	// Get vault for its bootstrap options
	cfg, _ := config.Load()
	v, _ := vaultpkg.NewFromConfig(cfg)

	for _, client := range installedClients {
		// Gather all options from vault and client
		var allOpts []bootstrap.Option
		if v != nil {
			allOpts = append(allOpts, v.GetBootstrapOptions(ctx)...)
		}
		allOpts = append(allOpts, client.GetBootstrapOptions(ctx)...)

		// Filter to enabled options (nil config = all enabled for backwards compat)
		var enabledOpts []bootstrap.Option
		if mpc == nil {
			enabledOpts = allOpts
		} else {
			enabledOpts = bootstrap.Filter(allOpts, mpc.GetBootstrapOption)
		}

		if err := client.UninstallBootstrap(ctx, enabledOpts); err != nil {
			styledOut.ErrorItem("Failed to remove hooks from " + client.DisplayName() + ": " + err.Error())
		} else {
			styledOut.SuccessItem("Removed hooks from " + client.DisplayName())
		}
	}
}

// reportResults displays final results to user
func reportResults(results []UninstallResult, styledOut *ui.Output) {
	styledOut.Newline()

	removedAssets := make(map[string]bool)
	failedAssets := make(map[string]bool)

	for _, result := range results {
		if result.Success {
			removedAssets[result.AssetName] = true
		} else {
			failedAssets[result.AssetName] = true
		}
	}

	// Don't count as failed if also successfully removed
	for name := range removedAssets {
		delete(failedAssets, name)
	}

	totalRemoved := len(removedAssets)
	totalFailed := len(failedAssets)

	if totalFailed > 0 {
		styledOut.Warning(fmt.Sprintf("Uninstalled %d asset(s) (%d failed)", totalRemoved, totalFailed))
	} else {
		styledOut.Success(fmt.Sprintf("Successfully uninstalled %d asset(s)", totalRemoved))
	}
}
