package commands

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/sx/internal/assets"
	"github.com/sleuth-io/sx/internal/clients"
	"github.com/sleuth-io/sx/internal/config"
	"github.com/sleuth-io/sx/internal/constants"
	"github.com/sleuth-io/sx/internal/gitutil"
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/logger"
	"github.com/sleuth-io/sx/internal/scope"
	"github.com/sleuth-io/sx/internal/ui"
	"github.com/sleuth-io/sx/internal/ui/components"
)

// NewInstallCommand creates the install command
func NewInstallCommand() *cobra.Command {
	var hookMode bool
	var clientID string
	var fixMode bool

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Read lock file, fetch assets, and install locally",
		Long: fmt.Sprintf(`Read the %s file, fetch assets from the configured vault,
and install them to ~/.claude/ directory.`, constants.SkillLockFile),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInstall(cmd, args, hookMode, clientID, fixMode)
		},
	}

	cmd.Flags().BoolVar(&hookMode, "hook-mode", false, "Run in hook mode (outputs JSON for Claude Code)")
	cmd.Flags().StringVar(&clientID, "client", "", "Client ID that triggered the hook (used with --hook-mode)")
	cmd.Flags().BoolVar(&fixMode, "repair", false, "Verify assets are actually installed and fix any discrepancies")
	_ = cmd.Flags().MarkHidden("hook-mode") // Hide from help output since it's internal
	_ = cmd.Flags().MarkHidden("client")    // Hide from help output since it's internal

	return cmd
}

// runInstall executes the install command
func runInstall(cmd *cobra.Command, args []string, hookMode bool, hookClientID string, repairMode bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	log := logger.Get()
	styledOut := ui.NewOutput(cmd.OutOrStdout(), cmd.ErrOrStderr())
	styledOut.SetSilent(hookMode)

	status := components.NewStatus(cmd.OutOrStdout())
	status.SetSilent(hookMode)

	out := newOutputHelper(cmd)
	out.silent = hookMode

	// Handle Cursor workspace directory in hook mode
	handleCursorWorkspace(hookMode, hookClientID, log)

	// Load and validate configuration
	cfg, vault, err := loadConfigAndVault()
	if err != nil {
		return err
	}

	// Fetch and parse lock file
	lockFile, err := fetchLockFileWithCache(ctx, vault, cfg, status)
	if err != nil {
		return err
	}

	// Detect environment (git context, scope, clients)
	env, err := detectInstallEnvironment(ctx, cfg, status)
	if err != nil {
		return err
	}

	// Hook mode fast path check
	if hookMode && checkHookModeFastPath(ctx, hookClientID, out) {
		return nil
	}

	// Filter and resolve assets
	matcherScope := scope.NewMatcher(env.CurrentScope)
	applicableAssets := filterAssetsByScope(lockFile, env.Clients, matcherScope)

	sortedAssets, err := resolveAssetDependencies(lockFile, applicableAssets)
	if err != nil {
		return err
	}

	// Load tracker
	tracker := loadTracker(out)
	targetClientIDs := getTargetClientIDs(env.Clients)

	// In repair mode, verify assets against filesystem and update tracker
	if repairMode {
		repairTracker(ctx, tracker, sortedAssets, env.Clients, env.GitContext, env.CurrentScope, out)
	}

	assetsToInstall := determineAssetsToInstall(tracker, sortedAssets, env.CurrentScope, targetClientIDs, out)

	// Clean up assets that were removed from lock file (must run even if no assets to install!)
	cleanupRemovedAssets(ctx, tracker, sortedAssets, env.GitContext, env.CurrentScope, env.Clients, out)

	// Early exit if nothing to install
	if len(assetsToInstall) == 0 {
		return handleNothingToInstall(ctx, hookMode, tracker, sortedAssets, env, targetClientIDs, styledOut, out)
	}

	// Download assets
	downloadResult, err := downloadAssetsWithStatus(ctx, vault, assetsToInstall, status, styledOut)
	if err != nil {
		return err
	}

	// Install assets to their appropriate locations
	installResult := installAssets(ctx, downloadResult.Downloads, env.GitContext, env.CurrentScope, env.Clients, out)

	// Save new installation state (saves ALL assets from lock file, not just changed ones)
	saveInstallationState(tracker, sortedAssets, env.CurrentScope, targetClientIDs, out)

	// Ensure skills support is configured for all clients (creates local rules files, etc.)
	ensureAssetSupport(ctx, env.Clients, buildInstallScope(env.CurrentScope, env.GitContext), out)

	// Report results
	if err := reportInstallResults(installResult, downloadResult.Downloads, env.CurrentScope, styledOut); err != nil {
		return err
	}

	// Install client-specific hooks (e.g., auto-update, usage tracking)
	installClientHooks(ctx, env.Clients, out)

	// Log summary
	log.Info("install completed", "installed", len(installResult.Installed), "failed", len(installResult.Failed))

	// If in hook mode and assets were installed, output JSON message
	if hookMode && len(installResult.Installed) > 0 {
		if err := outputHookModeInstallResult(out, installResult, downloadResult.Downloads); err != nil {
			return err
		}
	}

	return nil
}

// loadTracker loads the global tracker
func loadTracker(out *outputHelper) *assets.Tracker {
	tracker, err := assets.LoadTracker()
	if err != nil {
		out.printfErr("Warning: failed to load tracker: %v\n", err)
		log := logger.Get()
		log.Error("failed to load tracker", "error", err)
		return &assets.Tracker{
			Version: assets.TrackerFormatVersion,
			Assets:  []assets.InstalledAsset{},
		}
	}
	return tracker
}

// determineAssetsToInstall finds which assets need to be installed (new or changed)
func determineAssetsToInstall(tracker *assets.Tracker, sortedAssets []*lockfile.Asset, currentScope *scope.Scope, targetClientIDs []string, out *outputHelper) []*lockfile.Asset {
	log := logger.Get()

	var assetsToInstall []*lockfile.Asset
	for _, art := range sortedAssets {
		key := assetKeyForInstall(art, currentScope)
		if tracker.NeedsInstall(key, art.Version, targetClientIDs) {
			// Check for version updates and log them
			if existing := tracker.FindAsset(key); existing != nil && existing.Version != art.Version {
				log.Info("asset version update", "name", art.Name, "old_version", existing.Version, "new_version", art.Version)
			}
			assetsToInstall = append(assetsToInstall, art)
		}
	}

	return assetsToInstall
}

// assetKeyForInstall returns the correct asset key based on whether the asset is global or scoped
func assetKeyForInstall(asset *lockfile.Asset, currentScope *scope.Scope) assets.AssetKey {
	if asset.IsGlobal() {
		return assets.NewAssetKey(asset.Name, scope.TypeGlobal, "", "")
	}
	return assets.NewAssetKey(asset.Name, currentScope.Type, currentScope.RepoURL, currentScope.RepoPath)
}

// cleanupRemovedAssets removes assets that are no longer in the lock file from all clients
func cleanupRemovedAssets(ctx context.Context, tracker *assets.Tracker, sortedAssets []*lockfile.Asset, gitContext *gitutil.GitContext, currentScope *scope.Scope, targetClients []clients.Client, out *outputHelper) {
	// Find assets in tracker for this scope that are no longer in lock file
	key := assets.NewAssetKey("", currentScope.Type, currentScope.RepoURL, currentScope.RepoPath)
	currentInScope := tracker.FindByScope(key.Repository, key.Path)

	// Also check global assets (not scoped to any repo)
	globalAssets := tracker.FindByScope("", "")

	// Combine both scoped and global assets
	allRelevantAssets := append(currentInScope, globalAssets...)

	lockFileNames := make(map[string]bool)
	for _, art := range sortedAssets {
		lockFileNames[art.Name] = true
	}

	var removedAssets []assets.InstalledAsset
	for _, installed := range allRelevantAssets {
		if !lockFileNames[installed.Name] {
			removedAssets = append(removedAssets, installed)
		}
	}

	if len(removedAssets) == 0 {
		return
	}

	out.printf("\nCleaning up %d removed asset(s)...\n", len(removedAssets))

	// Group assets by scope and uninstall with appropriate scope
	globalAssets, scopedAssets := separateGlobalAndScopedAssets(removedAssets)

	if len(globalAssets) > 0 {
		globalScope := &clients.InstallScope{Type: clients.ScopeGlobal}
		uninstallAssetsWithScope(ctx, globalAssets, globalScope, targetClients, out)
	}

	if len(scopedAssets) > 0 {
		uninstallScope := buildInstallScope(currentScope, gitContext)
		uninstallAssetsWithScope(ctx, scopedAssets, uninstallScope, targetClients, out)
	}

	// Remove from tracker
	for _, removed := range removedAssets {
		tracker.RemoveAsset(removed.Key())
	}
}

// repairTracker verifies assets against the filesystem and updates the tracker to match reality
// This is called when --repair flag is used to fix discrepancies between tracker and actual installation
func repairTracker(ctx context.Context, tracker *assets.Tracker, sortedAssets []*lockfile.Asset, targetClients []clients.Client, gitContext *gitutil.GitContext, currentScope *scope.Scope, out *outputHelper) {
	log := logger.Get()
	out.println("Repair mode: verifying installed assets...")

	// Track which assets are missing for each client
	var totalMissing int
	var totalOutdated int

	// First, check for version mismatches in the tracker and remove outdated entries
	for _, art := range sortedAssets {
		key := assetKeyForInstall(art, currentScope)
		existing := tracker.FindAsset(key)
		if existing != nil && existing.Version != art.Version {
			out.printf("  ↻ %s version mismatch (tracker: %s, lock file: %s)\n", art.Name, existing.Version, art.Version)
			log.Info("asset version mismatch", "name", art.Name, "tracker_version", existing.Version, "lock_version", art.Version)
			// Remove from tracker so it will be reinstalled with correct version
			tracker.RemoveAsset(key)
			totalOutdated++
		}
	}

	// Verify each asset at its proper install location (based on asset's scope)
	for _, art := range sortedAssets {
		// Get the proper scope for this asset
		artScope := buildInstallScopeForAsset(art, gitContext)

		for _, client := range targetClients {
			// Verify this single asset at its proper location
			results := client.VerifyAssets(ctx, []*lockfile.Asset{art}, artScope)

			for _, result := range results {
				if !result.Installed {
					out.printf("  ✗ %s not installed for %s: %s\n", result.Asset.Name, client.DisplayName(), result.Message)
					log.Info("asset verification failed", "name", result.Asset.Name, "client", client.ID(), "reason", result.Message)

					// Remove this client from the asset's tracker entry
					key := assetKeyForInstall(result.Asset, currentScope)
					existing := tracker.FindAsset(key)
					if existing != nil {
						// Remove this client from the list
						var updatedClients []string
						for _, c := range existing.Clients {
							if c != client.ID() {
								updatedClients = append(updatedClients, c)
							}
						}

						if len(updatedClients) == 0 {
							// No clients left, remove entirely
							tracker.RemoveAsset(key)
						} else {
							existing.Clients = updatedClients
							tracker.UpsertAsset(*existing)
						}
					}
					totalMissing++
				}
			}
		}
	}

	if totalMissing == 0 && totalOutdated == 0 {
		out.println("  ✓ All assets verified")
	} else {
		if totalOutdated > 0 {
			out.printf("  Found %d outdated assets that will be updated\n", totalOutdated)
		}
		if totalMissing > 0 {
			out.printf("  Found %d missing assets that will be reinstalled\n", totalMissing)
		}
	}
	out.println()
}

// installAssets installs assets to all detected clients using the orchestrator
func installAssets(ctx context.Context, successfulDownloads []*assets.AssetWithMetadata, gitContext *gitutil.GitContext, currentScope *scope.Scope, targetClients []clients.Client, out *outputHelper) *assets.InstallResult {
	out.println("Installing assets...")

	// Install each asset to its proper scope
	// Global assets go to ~/.claude, repo-scoped assets go to {repoRoot}/.claude
	allResults := make(map[string]clients.InstallResponse)

	for _, download := range successfulDownloads {
		bundle := &clients.AssetBundle{
			Asset:    download.Asset,
			Metadata: download.Metadata,
			ZipData:  download.ZipData,
		}

		// Determine installation scope based on the ASSET's scope, not current directory
		installScope := buildInstallScopeForAsset(download.Asset, gitContext)

		// Run installation for this asset
		results := runMultiClientInstallation(ctx, []*clients.AssetBundle{bundle}, installScope, targetClients)

		// Merge results
		for clientID, resp := range results {
			if existing, ok := allResults[clientID]; ok {
				existing.Results = append(existing.Results, resp.Results...)
				allResults[clientID] = existing
			} else {
				allResults[clientID] = resp
			}
		}
	}

	// Process and report results
	return processInstallationResults(allResults, out)
}

// buildInstallScope creates the installation scope from current context
func buildInstallScope(currentScope *scope.Scope, gitContext *gitutil.GitContext) *clients.InstallScope {
	installScope := &clients.InstallScope{
		Type:    clients.ScopeType(currentScope.Type),
		RepoURL: currentScope.RepoURL,
		Path:    currentScope.RepoPath,
	}

	if gitContext.IsRepo {
		installScope.RepoRoot = gitContext.RepoRoot
	}

	return installScope
}

// buildInstallScopeForAsset creates the installation scope based on the asset's own scope
// Global assets go to ~/.claude, repo-scoped assets go to {repoRoot}/.claude
func buildInstallScopeForAsset(art *lockfile.Asset, gitContext *gitutil.GitContext) *clients.InstallScope {
	if art.IsGlobal() {
		// Global asset - install to ~/.claude
		return &clients.InstallScope{
			Type: clients.ScopeGlobal,
		}
	}

	// Repo or path-scoped asset - install to repo's .claude directory
	installScope := &clients.InstallScope{
		Type: clients.ScopeRepository,
	}

	if gitContext.IsRepo {
		installScope.RepoRoot = gitContext.RepoRoot
		installScope.RepoURL = gitContext.RepoURL
	}

	// For path-scoped assets, we'd need to handle the path too
	// but for now we install to repo root
	return installScope
}

// runMultiClientInstallation executes installation across all clients concurrently
func runMultiClientInstallation(ctx context.Context, bundles []*clients.AssetBundle, installScope *clients.InstallScope, targetClients []clients.Client) map[string]clients.InstallResponse {
	orchestrator := clients.NewOrchestrator(clients.Global())
	return orchestrator.InstallToClients(ctx, bundles, installScope, clients.InstallOptions{}, targetClients)
}

// processInstallationResults processes results from all clients and builds the final result
func processInstallationResults(allResults map[string]clients.InstallResponse, out *outputHelper) *assets.InstallResult {
	installResult := &assets.InstallResult{
		Installed: []string{},
		Failed:    []string{},
		Errors:    []error{},
	}

	successfullyInstalled := make(map[string]bool)

	for clientID, resp := range allResults {
		client, _ := clients.Global().Get(clientID)

		for _, result := range resp.Results {
			switch result.Status {
			case clients.StatusSuccess:
				out.printf("  ✓ %s → %s\n", result.AssetName, client.DisplayName())
				successfullyInstalled[result.AssetName] = true
			case clients.StatusFailed:
				out.printfErr("  ✗ %s → %s: %v\n", result.AssetName, client.DisplayName(), result.Error)
				installResult.Failed = append(installResult.Failed, result.AssetName)
				installResult.Errors = append(installResult.Errors, result.Error)
			case clients.StatusSkipped:
				// Don't print skipped assets
			}
		}
	}

	// Build list of successfully installed assets
	for name := range successfullyInstalled {
		installResult.Installed = append(installResult.Installed, name)
	}

	// Add error if ANY client failed
	if clients.HasAnyErrors(allResults) {
		installResult.Errors = append(installResult.Errors, errors.New("installation failed for one or more clients"))
	}

	return installResult
}

// installClientHooks calls InstallHooks on all clients to install client-specific hooks
func installClientHooks(ctx context.Context, targetClients []clients.Client, out *outputHelper) {
	log := logger.Get()
	for _, client := range targetClients {
		if err := client.InstallBootstrap(ctx); err != nil {
			out.printfErr("Warning: failed to install hooks for %s: %v\n", client.DisplayName(), err)
			log.Error("failed to install client hooks", "client", client.ID(), "error", err)
			// Don't fail the install command if hook installation fails
		}
	}
}

// ensureAssetSupport calls EnsureAssetSupport on all clients to set up local rules files, etc.
func ensureAssetSupport(ctx context.Context, targetClients []clients.Client, scope *clients.InstallScope, out *outputHelper) {
	log := logger.Get()
	for _, client := range targetClients {
		if err := client.EnsureAssetSupport(ctx, scope); err != nil {
			out.printfErr("Warning: failed to ensure asset support for %s: %v\n", client.DisplayName(), err)
			log.Error("failed to ensure asset support", "client", client.ID(), "error", err)
		}
	}
}

// saveInstallationState saves the current installation state to tracker file
func saveInstallationState(tracker *assets.Tracker, sortedAssets []*lockfile.Asset, currentScope *scope.Scope, targetClientIDs []string, out *outputHelper) {
	for _, art := range sortedAssets {
		key := assetKeyForInstall(art, currentScope)
		tracker.UpsertAsset(assets.InstalledAsset{
			Name:       art.Name,
			Version:    art.Version,
			Type:       art.Type.Key,
			Repository: key.Repository,
			Path:       key.Path,
			Clients:    targetClientIDs,
		})
	}

	if err := assets.SaveTracker(tracker); err != nil {
		out.printfErr("Warning: failed to save installation state: %v\n", err)
		log := logger.Get()
		log.Error("failed to save tracker", "error", err)
	}
}

// filterClientsByConfig returns only the clients that are both detected as installed
// and enabled in the config. If EnabledClients is empty/nil, all detected clients are returned.
func filterClientsByConfig(cfg *config.Config, detectedClients []clients.Client) []clients.Client {
	if len(cfg.EnabledClients) == 0 {
		// No restrictions - use all detected clients (backwards compatible)
		return detectedClients
	}

	enabledMap := make(map[string]bool)
	for _, id := range cfg.EnabledClients {
		enabledMap[id] = true
	}

	var filtered []clients.Client
	for _, client := range detectedClients {
		if enabledMap[client.ID()] {
			filtered = append(filtered, client)
		}
	}

	return filtered
}
