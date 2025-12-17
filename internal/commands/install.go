package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/assets"
	"github.com/sleuth-io/sx/internal/cache"
	"github.com/sleuth-io/sx/internal/clients"
	"github.com/sleuth-io/sx/internal/clients/cursor"
	"github.com/sleuth-io/sx/internal/config"
	"github.com/sleuth-io/sx/internal/constants"
	"github.com/sleuth-io/sx/internal/gitutil"
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/logger"
	"github.com/sleuth-io/sx/internal/scope"
	"github.com/sleuth-io/sx/internal/ui"
	"github.com/sleuth-io/sx/internal/ui/components"
	vaultpkg "github.com/sleuth-io/sx/internal/vault"
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
	styledOut.SetSilent(hookMode) // Suppress normal output in hook mode

	// Status line for transient updates
	status := components.NewStatus(cmd.OutOrStdout())
	status.SetSilent(hookMode)

	// Keep old outputHelper for functions that still use it (will migrate incrementally)
	out := newOutputHelper(cmd)
	out.silent = hookMode

	// When running in hook mode for Cursor, parse stdin to get workspace directory
	// and chdir to it so git detection and scope logic work correctly
	if hookMode && hookClientID == "cursor" {
		if workspaceDir := cursor.ParseWorkspaceDir(); workspaceDir != "" {
			if err := os.Chdir(workspaceDir); err != nil {
				log.Warn("failed to chdir to workspace", "workspace", workspaceDir, "error", err)
			} else {
				log.Debug("changed to workspace directory", "workspace", workspaceDir)
			}
		}
	}

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w\nRun 'sx init' to configure", err)
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// Create vault instance
	vault, err := vaultpkg.NewFromConfig(cfg)
	if err != nil {
		return fmt.Errorf("failed to create vault: %w", err)
	}

	// Fetch lock file with spinner
	status.Start("Fetching lock file")

	cachedETag, _ := cache.LoadETag(cfg.RepositoryURL)

	lockFileData, newETag, notModified, err := vault.GetLockFile(ctx, cachedETag)
	if err != nil {
		status.Fail("Failed to fetch lock file")
		return fmt.Errorf("failed to fetch lock file: %w", err)
	}

	if notModified {
		lockFileData, err = cache.LoadLockFile(cfg.RepositoryURL)
		if err != nil {
			status.Fail("Failed to load cached lock file")
			return fmt.Errorf("failed to load cached lock file: %w", err)
		}
	} else {
		// Save ETag and lock file content
		log := logger.Get()
		if newETag != "" {
			if err := cache.SaveETag(cfg.RepositoryURL, newETag); err != nil {
				log.Error("failed to save ETag", "error", err)
			}
		}
		if err := cache.SaveLockFile(cfg.RepositoryURL, lockFileData); err != nil {
			log.Error("failed to cache lock file", "error", err)
		}
	}

	// Parse lock file
	lockFile, err := lockfile.Parse(lockFileData)
	if err != nil {
		status.Fail("Failed to parse lock file")
		return fmt.Errorf("failed to parse lock file: %w", err)
	}

	// Validate lock file
	if err := lockFile.Validate(); err != nil {
		status.Fail("Lock file validation failed")
		return fmt.Errorf("lock file validation failed: %w", err)
	}

	status.Clear() // Clear the spinner, no permanent message needed

	// Detect Git context (transient)
	status.Start("Detecting context")
	gitContext, err := gitutil.DetectContext(ctx)
	if err != nil {
		status.Fail("Failed to detect git context")
		return fmt.Errorf("failed to detect git context: %w", err)
	}

	// Build scope and matcher
	var currentScope *scope.Scope
	if gitContext.IsRepo {
		if gitContext.RelativePath == "." {
			currentScope = &scope.Scope{
				Type:     scope.TypeRepo,
				RepoURL:  gitContext.RepoURL,
				RepoPath: "",
			}
		} else {
			currentScope = &scope.Scope{
				Type:     scope.TypePath,
				RepoURL:  gitContext.RepoURL,
				RepoPath: gitContext.RelativePath,
			}
		}
	} else {
		currentScope = &scope.Scope{
			Type: scope.TypeGlobal,
		}
	}

	matcherScope := scope.NewMatcher(currentScope)

	// Detect installed clients and filter by config
	registry := clients.Global()
	detectedClients := registry.DetectInstalled()
	targetClients := filterClientsByConfig(cfg, detectedClients)
	if len(targetClients) == 0 {
		if len(detectedClients) > 0 {
			status.Fail("No enabled AI coding clients available")
			return fmt.Errorf("no enabled AI coding clients available (detected: %d, enabled in config: %v)",
				len(detectedClients), cfg.EnabledClients)
		}
		status.Fail("No AI coding clients detected")
		return fmt.Errorf("no AI coding clients detected")
	}

	status.Clear() // Clear status, we'll show summary at end

	// In hook mode, check if the triggering client says to skip installation
	// This is the fast path for clients like Cursor that fire hooks on every prompt
	if hookMode && hookClientID != "" {
		// Find the specific client that triggered the hook
		hookClient, err := registry.Get(hookClientID)
		if err == nil {
			shouldInstall, err := hookClient.ShouldInstall(ctx)
			if err != nil {
				log := logger.Get()
				log.Warn("ShouldInstall check failed", "client", hookClientID, "error", err)
				// Continue on error
			}
			if !shouldInstall {
				// Fast path - client says skip (e.g., already seen this conversation)
				log := logger.Get()
				log.Info("install skipped by client", "client", hookClientID, "reason", "already ran for this session")
				response := map[string]interface{}{
					"continue": true,
				}
				jsonBytes, err := json.MarshalIndent(response, "", "  ")
				if err != nil {
					return fmt.Errorf("failed to marshal JSON response: %w", err)
				}
				out.printlnAlways(string(jsonBytes))
				return nil
			}
		}
	}

	// Filter assets by client compatibility and scope
	var applicableAssets []*lockfile.Asset
	for i := range lockFile.Assets {
		asset := &lockFile.Assets[i]

		// Check if ANY target client supports this asset AND matches scope
		supported := false
		for _, client := range targetClients {
			if asset.MatchesClient(client.ID()) &&
				client.SupportsAssetType(asset.Type) &&
				matcherScope.MatchesAsset(asset) {
				supported = true
				break
			}
		}

		if supported {
			applicableAssets = append(applicableAssets, asset)
		}
	}

	if len(applicableAssets) == 0 {
		styledOut.Muted("No assets to install.")
		return nil
	}

	// Resolve dependencies
	resolver := assets.NewDependencyResolver(lockFile)
	sortedAssets, err := resolver.Resolve(applicableAssets)
	if err != nil {
		return fmt.Errorf("dependency resolution failed: %w", err)
	}

	// Load tracker
	tracker := loadTracker(out)

	// Determine which assets need to be installed (new or changed versions or missing from clients)
	targetClientIDs := make([]string, len(targetClients))
	for i, client := range targetClients {
		targetClientIDs[i] = client.ID()
	}

	// In repair mode, verify assets against filesystem and update tracker
	if repairMode {
		repairTracker(ctx, tracker, sortedAssets, targetClients, gitContext, currentScope, out)
	}

	assetsToInstall := determineAssetsToInstall(tracker, sortedAssets, currentScope, targetClientIDs, out)

	// Clean up assets that were removed from lock file
	cleanupRemovedAssets(ctx, tracker, sortedAssets, gitContext, currentScope, targetClients, out)

	// Early exit if nothing to install
	if len(assetsToInstall) == 0 {
		// Save state even if nothing changed
		saveInstallationState(tracker, sortedAssets, currentScope, targetClientIDs, out)

		// Install client-specific hooks (e.g., auto-update, usage tracking)
		installClientHooks(ctx, targetClients, out)

		// Ensure asset support is configured for all clients (creates local rules files, etc.)
		// This is important even when no new assets are installed, as the local rules file
		// may not exist yet (e.g., running in a new repo with only global assets)
		ensureAssetSupport(ctx, targetClients, buildInstallScope(currentScope, gitContext), out)

		// Log summary
		log := logger.Get()
		log.Info("install completed", "installed", 0, "total_up_to_date", len(sortedAssets))

		// In hook mode, output JSON even when nothing changed
		if hookMode {
			response := map[string]interface{}{
				"continue": true,
			}
			jsonBytes, err := json.MarshalIndent(response, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal JSON response: %w", err)
			}
			styledOut.PrintlnAlways(string(jsonBytes))
		} else {
			styledOut.Success("All assets up to date")
		}

		return nil
	}

	// Download only the assets that need to be installed
	status.Start(fmt.Sprintf("Downloading %d assets", len(assetsToInstall)))
	fetcher := assets.NewAssetFetcher(vault)
	results, err := fetcher.FetchAssets(ctx, assetsToInstall, 10)
	if err != nil {
		return fmt.Errorf("failed to fetch assets: %w", err)
	}

	// Check for download errors
	var downloadErrors []error
	var successfulDownloads []*assets.AssetWithMetadata
	for _, result := range results {
		if result.Error != nil {
			downloadErrors = append(downloadErrors, fmt.Errorf("%s: %w", result.Asset.Name, result.Error))
		} else {
			successfulDownloads = append(successfulDownloads, &assets.AssetWithMetadata{
				Asset:    result.Asset,
				Metadata: result.Metadata,
				ZipData:  result.ZipData,
			})
		}
	}

	status.Clear()

	if len(downloadErrors) > 0 {
		log := logger.Get()
		for _, err := range downloadErrors {
			styledOut.ErrorItem(err.Error())
			log.Error("asset download failed", "error", err)
		}
	}

	if len(successfulDownloads) == 0 {
		styledOut.Error("No assets downloaded successfully")
		return fmt.Errorf("no assets downloaded successfully")
	}

	// Install assets to their appropriate locations
	installResult := installAssets(ctx, successfulDownloads, gitContext, currentScope, targetClients, out)

	// Save new installation state (saves ALL assets from lock file, not just changed ones)
	saveInstallationState(tracker, sortedAssets, currentScope, targetClientIDs, out)

	// Ensure skills support is configured for all clients (creates local rules files, etc.)
	ensureAssetSupport(ctx, targetClients, buildInstallScope(currentScope, gitContext), out)

	// Report results - clean summary
	if len(installResult.Installed) > 0 {
		styledOut.Success(fmt.Sprintf("Installed %d assets", len(installResult.Installed)))
		for _, name := range installResult.Installed {
			styledOut.SuccessItem(name)
			// Log version for this asset
			for _, art := range successfulDownloads {
				if art.Asset.Name == name {
					log.Info("asset installed", "name", name, "version", art.Asset.Version, "type", art.Metadata.Asset.Type, "scope", currentScope.Type)
					break
				}
			}
		}
	}

	if len(installResult.Failed) > 0 {
		styledOut.Error(fmt.Sprintf("Failed to install %d assets", len(installResult.Failed)))
		for i, name := range installResult.Failed {
			styledOut.ErrorItem(fmt.Sprintf("%s: %v", name, installResult.Errors[i]))
			log.Error("asset installation failed", "name", name, "error", installResult.Errors[i])
		}
		return fmt.Errorf("some assets failed to install")
	}

	// Install client-specific hooks (e.g., auto-update, usage tracking)
	installClientHooks(ctx, targetClients, out)

	// Log summary
	log.Info("install completed", "installed", len(installResult.Installed), "failed", len(installResult.Failed))

	// If in hook mode and assets were installed, output JSON message
	if hookMode && len(installResult.Installed) > 0 {
		// Build asset list message with type info
		type assetInfo struct {
			name string
			typ  string
		}
		var installedAssets []assetInfo
		for _, name := range installResult.Installed {
			for _, art := range successfulDownloads {
				if art.Asset.Name == name {
					installedAssets = append(installedAssets, assetInfo{
						name: name,
						typ:  strings.ToLower(art.Metadata.Asset.Type.Label),
					})
					break
				}
			}
		}

		// ANSI color codes (using bold and blue for better visibility on light/dark terminals)
		const (
			bold      = "\033[1m"
			blue      = "\033[34m"
			red       = "\033[31m"
			resetBold = "\033[22m"
			reset     = "\033[0m"
		)

		var message string
		if len(installedAssets) == 1 {
			// Single asset - more compact message
			message = fmt.Sprintf("%ssx%s installed the %s%s %s%s. %sRestart Claude Code to use it.%s",
				bold, resetBold, blue, installedAssets[0].name, installedAssets[0].typ, reset, red, reset)
		} else if len(installedAssets) <= 3 {
			// List all items
			message = fmt.Sprintf("%ssx%s installed:\n", bold, resetBold)
			for _, asset := range installedAssets {
				message += fmt.Sprintf("- The %s%s %s%s\n", blue, asset.name, asset.typ, reset)
			}
			message += fmt.Sprintf("\n%sRestart Claude Code to use them.%s", red, reset)
		} else {
			// Show first 3 and count remaining
			message = fmt.Sprintf("%ssx%s installed:\n", bold, resetBold)
			for i := 0; i < 3; i++ {
				message += fmt.Sprintf("- The %s%s %s%s\n", blue, installedAssets[i].name, installedAssets[i].typ, reset)
			}
			remaining := len(installedAssets) - 3
			message += fmt.Sprintf("and %d more\n\n%sRestart Claude Code to use them.%s", remaining, red, reset)
		}

		// Output JSON response
		response := map[string]interface{}{
			"systemMessage": message,
			"continue":      true,
		}
		jsonBytes, err := json.MarshalIndent(response, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal JSON response: %w", err)
		}
		out.printlnAlways(string(jsonBytes))
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

	lockFileNames := make(map[string]bool)
	for _, art := range sortedAssets {
		lockFileNames[art.Name] = true
	}

	var removedAssets []assets.InstalledAsset
	for _, installed := range currentInScope {
		if !lockFileNames[installed.Name] {
			removedAssets = append(removedAssets, installed)
		}
	}

	if len(removedAssets) == 0 {
		return
	}

	out.printf("\nCleaning up %d removed asset(s)...\n", len(removedAssets))

	// Build uninstall scope
	uninstallScope := buildInstallScope(currentScope, gitContext)

	// Convert InstalledAsset to asset.Asset for uninstall
	assetsToRemove := make([]asset.Asset, len(removedAssets))
	for i, installed := range removedAssets {
		assetsToRemove[i] = asset.Asset{
			Name:    installed.Name,
			Version: installed.Version,
			Type:    asset.FromString(installed.Type),
		}
	}

	// Create uninstall request
	uninstallReq := clients.UninstallRequest{
		Assets:  assetsToRemove,
		Scope:   uninstallScope,
		Options: clients.UninstallOptions{},
	}

	// Uninstall from all clients
	log := logger.Get()
	for _, client := range targetClients {
		resp, err := client.UninstallAssets(ctx, uninstallReq)
		if err != nil {
			out.printfErr("Warning: cleanup failed for %s: %v\n", client.DisplayName(), err)
			log.Error("cleanup failed", "client", client.ID(), "error", err)
			continue
		}

		for _, result := range resp.Results {
			if result.Status == clients.StatusSuccess {
				out.printf("  - Removed %s from %s\n", result.AssetName, client.DisplayName())
				log.Info("asset removed", "name", result.AssetName, "client", client.ID())
			} else if result.Status == clients.StatusFailed {
				out.printfErr("Warning: failed to remove %s from %s: %v\n", result.AssetName, client.DisplayName(), result.Error)
				log.Error("asset removal failed", "name", result.AssetName, "client", client.ID(), "error", result.Error)
			}
		}
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
		installResult.Errors = append(installResult.Errors, fmt.Errorf("installation failed for one or more clients"))
	}

	return installResult
}

// installClientHooks calls InstallHooks on all clients to install client-specific hooks
func installClientHooks(ctx context.Context, targetClients []clients.Client, out *outputHelper) {
	log := logger.Get()
	for _, client := range targetClients {
		if err := client.InstallHooks(ctx); err != nil {
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
