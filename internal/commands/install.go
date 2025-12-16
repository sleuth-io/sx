package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/skills/internal/asset"
	"github.com/sleuth-io/skills/internal/assets"
	"github.com/sleuth-io/skills/internal/cache"
	"github.com/sleuth-io/skills/internal/clients"
	"github.com/sleuth-io/skills/internal/clients/cursor"
	"github.com/sleuth-io/skills/internal/config"
	"github.com/sleuth-io/skills/internal/constants"
	"github.com/sleuth-io/skills/internal/gitutil"
	"github.com/sleuth-io/skills/internal/lockfile"
	"github.com/sleuth-io/skills/internal/logger"
	"github.com/sleuth-io/skills/internal/scope"
	vaultpkg "github.com/sleuth-io/skills/internal/vault"
	"github.com/sleuth-io/skills/internal/ui"
	"github.com/sleuth-io/skills/internal/ui/components"
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

	// Detect installed clients
	registry := clients.Global()
	targetClients := registry.DetectInstalled()
	if len(targetClients) == 0 {
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

	// Filter artifacts by client compatibility and scope
	var applicableArtifacts []*lockfile.Artifact
	for i := range lockFile.Artifacts {
		artifact := &lockFile.Artifacts[i]

		// Check if ANY target client supports this artifact AND matches scope
		supported := false
		for _, client := range targetClients {
			if artifact.MatchesClient(client.ID()) &&
				client.SupportsArtifactType(artifact.Type) &&
				matcherScope.MatchesArtifact(artifact) {
				supported = true
				break
			}
		}

		if supported {
			applicableArtifacts = append(applicableArtifacts, artifact)
		}
	}

	if len(applicableArtifacts) == 0 {
		styledOut.Muted("No artifacts to install.")
		return nil
	}

	// Resolve dependencies
	resolver := assets.NewDependencyResolver(lockFile)
	sortedArtifacts, err := resolver.Resolve(applicableArtifacts)
	if err != nil {
		return fmt.Errorf("dependency resolution failed: %w", err)
	}

	// Load tracker
	tracker := loadTracker(out)

	// Determine which artifacts need to be installed (new or changed versions or missing from clients)
	targetClientIDs := make([]string, len(targetClients))
	for i, client := range targetClients {
		targetClientIDs[i] = client.ID()
	}

	// In repair mode, verify artifacts against filesystem and update tracker
	if repairMode {
		repairTracker(ctx, tracker, sortedArtifacts, targetClients, gitContext, currentScope, out)
	}

	artifactsToInstall := determineArtifactsToInstall(tracker, sortedArtifacts, currentScope, targetClientIDs, out)

	// Clean up artifacts that were removed from lock file
	cleanupRemovedArtifacts(ctx, tracker, sortedArtifacts, gitContext, currentScope, targetClients, out)

	// Early exit if nothing to install
	if len(artifactsToInstall) == 0 {
		// Save state even if nothing changed
		saveInstallationState(tracker, sortedArtifacts, currentScope, targetClientIDs, out)

		// Install client-specific hooks (e.g., auto-update, usage tracking)
		installClientHooks(ctx, targetClients, out)

		// Ensure skills support is configured for all clients (creates local rules files, etc.)
		// This is important even when no new artifacts are installed, as the local rules file
		// may not exist yet (e.g., running in a new repo with only global skills)
		ensureSkillsSupport(ctx, targetClients, buildInstallScope(currentScope, gitContext), out)

		// Log summary
		log := logger.Get()
		log.Info("install completed", "installed", 0, "total_up_to_date", len(sortedArtifacts))

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

	// Download only the artifacts that need to be installed
	status.Start(fmt.Sprintf("Downloading %d assets", len(artifactsToInstall)))
	fetcher := assets.NewArtifactFetcher(vault)
	results, err := fetcher.FetchArtifacts(ctx, artifactsToInstall, 10)
	if err != nil {
		return fmt.Errorf("failed to fetch artifacts: %w", err)
	}

	// Check for download errors
	var downloadErrors []error
	var successfulDownloads []*assets.ArtifactWithMetadata
	for _, result := range results {
		if result.Error != nil {
			downloadErrors = append(downloadErrors, fmt.Errorf("%s: %w", result.Artifact.Name, result.Error))
		} else {
			successfulDownloads = append(successfulDownloads, &assets.ArtifactWithMetadata{
				Artifact: result.Artifact,
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
			log.Error("artifact download failed", "error", err)
		}
	}

	if len(successfulDownloads) == 0 {
		styledOut.Error("No assets downloaded successfully")
		return fmt.Errorf("no assets downloaded successfully")
	}

	// Install artifacts to their appropriate locations
	installResult := installArtifacts(ctx, successfulDownloads, gitContext, currentScope, targetClients, out)

	// Save new installation state (saves ALL artifacts from lock file, not just changed ones)
	saveInstallationState(tracker, sortedArtifacts, currentScope, targetClientIDs, out)

	// Ensure skills support is configured for all clients (creates local rules files, etc.)
	ensureSkillsSupport(ctx, targetClients, buildInstallScope(currentScope, gitContext), out)

	// Report results - clean summary
	if len(installResult.Installed) > 0 {
		styledOut.Success(fmt.Sprintf("Installed %d assets", len(installResult.Installed)))
		for _, name := range installResult.Installed {
			styledOut.SuccessItem(name)
			// Log version for this artifact
			for _, art := range successfulDownloads {
				if art.Artifact.Name == name {
					log.Info("artifact installed", "name", name, "version", art.Artifact.Version, "type", art.Metadata.Artifact.Type, "scope", currentScope.Type)
					break
				}
			}
		}
	}

	if len(installResult.Failed) > 0 {
		styledOut.Error(fmt.Sprintf("Failed to install %d assets", len(installResult.Failed)))
		for i, name := range installResult.Failed {
			styledOut.ErrorItem(fmt.Sprintf("%s: %v", name, installResult.Errors[i]))
			log.Error("artifact installation failed", "name", name, "error", installResult.Errors[i])
		}
		return fmt.Errorf("some assets failed to install")
	}

	// Install client-specific hooks (e.g., auto-update, usage tracking)
	installClientHooks(ctx, targetClients, out)

	// Log summary
	log.Info("install completed", "installed", len(installResult.Installed), "failed", len(installResult.Failed))

	// If in hook mode and artifacts were installed, output JSON message
	if hookMode && len(installResult.Installed) > 0 {
		// Build artifact list message with type info
		type artifactInfo struct {
			name string
			typ  string
		}
		var artifacts []artifactInfo
		for _, name := range installResult.Installed {
			for _, art := range successfulDownloads {
				if art.Artifact.Name == name {
					artifacts = append(artifacts, artifactInfo{
						name: name,
						typ:  strings.ToLower(art.Metadata.Artifact.Type.Label),
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
		if len(artifacts) == 1 {
			// Single artifact - more compact message
			message = fmt.Sprintf("%ssx%s installed the %s%s %s%s. %sRestart Claude Code to use it.%s",
				bold, resetBold, blue, artifacts[0].name, artifacts[0].typ, reset, red, reset)
		} else if len(artifacts) <= 3 {
			// List all items
			message = fmt.Sprintf("%ssx%s installed:\n", bold, resetBold)
			for _, art := range artifacts {
				message += fmt.Sprintf("- The %s%s %s%s\n", blue, art.name, art.typ, reset)
			}
			message += fmt.Sprintf("\n%sRestart Claude Code to use them.%s", red, reset)
		} else {
			// Show first 3 and count remaining
			message = fmt.Sprintf("%ssx%s installed:\n", bold, resetBold)
			for i := 0; i < 3; i++ {
				message += fmt.Sprintf("- The %s%s %s%s\n", blue, artifacts[i].name, artifacts[i].typ, reset)
			}
			remaining := len(artifacts) - 3
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
			Version:   assets.TrackerFormatVersion,
			Artifacts: []assets.InstalledArtifact{},
		}
	}
	return tracker
}

// determineArtifactsToInstall finds which artifacts need to be installed (new or changed)
func determineArtifactsToInstall(tracker *assets.Tracker, sortedArtifacts []*lockfile.Artifact, currentScope *scope.Scope, targetClientIDs []string, out *outputHelper) []*lockfile.Artifact {
	log := logger.Get()

	var artifactsToInstall []*lockfile.Artifact
	for _, art := range sortedArtifacts {
		key := artifactKeyForInstall(art, currentScope)
		if tracker.NeedsInstall(key, art.Version, targetClientIDs) {
			// Check for version updates and log them
			if existing := tracker.FindArtifact(key); existing != nil && existing.Version != art.Version {
				log.Info("artifact version update", "name", art.Name, "old_version", existing.Version, "new_version", art.Version)
			}
			artifactsToInstall = append(artifactsToInstall, art)
		}
	}

	return artifactsToInstall
}

// artifactKeyForInstall returns the correct artifact key based on whether the artifact is global or scoped
func artifactKeyForInstall(art *lockfile.Artifact, currentScope *scope.Scope) assets.ArtifactKey {
	if art.IsGlobal() {
		return assets.NewArtifactKey(art.Name, scope.TypeGlobal, "", "")
	}
	return assets.NewArtifactKey(art.Name, currentScope.Type, currentScope.RepoURL, currentScope.RepoPath)
}

// cleanupRemovedArtifacts removes artifacts that are no longer in the lock file from all clients
func cleanupRemovedArtifacts(ctx context.Context, tracker *assets.Tracker, sortedArtifacts []*lockfile.Artifact, gitContext *gitutil.GitContext, currentScope *scope.Scope, targetClients []clients.Client, out *outputHelper) {
	// Find artifacts in tracker for this scope that are no longer in lock file
	key := assets.NewArtifactKey("", currentScope.Type, currentScope.RepoURL, currentScope.RepoPath)
	currentInScope := tracker.FindByScope(key.Repository, key.Path)

	lockFileNames := make(map[string]bool)
	for _, art := range sortedArtifacts {
		lockFileNames[art.Name] = true
	}

	var removedArtifacts []assets.InstalledArtifact
	for _, installed := range currentInScope {
		if !lockFileNames[installed.Name] {
			removedArtifacts = append(removedArtifacts, installed)
		}
	}

	if len(removedArtifacts) == 0 {
		return
	}

	out.printf("\nCleaning up %d removed asset(s)...\n", len(removedArtifacts))

	// Build uninstall scope
	uninstallScope := buildInstallScope(currentScope, gitContext)

	// Convert InstalledArtifact to asset.Asset for uninstall
	artifactsToRemove := make([]asset.Asset, len(removedArtifacts))
	for i, installed := range removedArtifacts {
		artifactsToRemove[i] = asset.Asset{
			Name:    installed.Name,
			Version: installed.Version,
			Type:    asset.FromString(installed.Type),
		}
	}

	// Create uninstall request
	uninstallReq := clients.UninstallRequest{
		Artifacts: artifactsToRemove,
		Scope:     uninstallScope,
		Options:   clients.UninstallOptions{},
	}

	// Uninstall from all clients
	log := logger.Get()
	for _, client := range targetClients {
		resp, err := client.UninstallArtifacts(ctx, uninstallReq)
		if err != nil {
			out.printfErr("Warning: cleanup failed for %s: %v\n", client.DisplayName(), err)
			log.Error("cleanup failed", "client", client.ID(), "error", err)
			continue
		}

		for _, result := range resp.Results {
			if result.Status == clients.StatusSuccess {
				out.printf("  - Removed %s from %s\n", result.ArtifactName, client.DisplayName())
				log.Info("artifact removed", "name", result.ArtifactName, "client", client.ID())
			} else if result.Status == clients.StatusFailed {
				out.printfErr("Warning: failed to remove %s from %s: %v\n", result.ArtifactName, client.DisplayName(), result.Error)
				log.Error("artifact removal failed", "name", result.ArtifactName, "client", client.ID(), "error", result.Error)
			}
		}
	}

	// Remove from tracker
	for _, removed := range removedArtifacts {
		tracker.RemoveArtifact(removed.Key())
	}
}

// repairTracker verifies artifacts against the filesystem and updates the tracker to match reality
// This is called when --repair flag is used to fix discrepancies between tracker and actual installation
func repairTracker(ctx context.Context, tracker *assets.Tracker, sortedArtifacts []*lockfile.Artifact, targetClients []clients.Client, gitContext *gitutil.GitContext, currentScope *scope.Scope, out *outputHelper) {
	log := logger.Get()
	out.println("Repair mode: verifying installed assets...")

	// Track which artifacts are missing for each client
	var totalMissing int
	var totalOutdated int

	// First, check for version mismatches in the tracker and remove outdated entries
	for _, art := range sortedArtifacts {
		key := artifactKeyForInstall(art, currentScope)
		existing := tracker.FindArtifact(key)
		if existing != nil && existing.Version != art.Version {
			out.printf("  ↻ %s version mismatch (tracker: %s, lock file: %s)\n", art.Name, existing.Version, art.Version)
			log.Info("artifact version mismatch", "name", art.Name, "tracker_version", existing.Version, "lock_version", art.Version)
			// Remove from tracker so it will be reinstalled with correct version
			tracker.RemoveArtifact(key)
			totalOutdated++
		}
	}

	// Verify each artifact at its proper install location (based on artifact's scope)
	for _, art := range sortedArtifacts {
		// Get the proper scope for this artifact
		artScope := buildInstallScopeForArtifact(art, gitContext)

		for _, client := range targetClients {
			// Verify this single artifact at its proper location
			results := client.VerifyArtifacts(ctx, []*lockfile.Artifact{art}, artScope)

			for _, result := range results {
				if !result.Installed {
					out.printf("  ✗ %s not installed for %s: %s\n", result.Artifact.Name, client.DisplayName(), result.Message)
					log.Info("artifact verification failed", "name", result.Artifact.Name, "client", client.ID(), "reason", result.Message)

					// Remove this client from the artifact's tracker entry
					key := artifactKeyForInstall(result.Artifact, currentScope)
					existing := tracker.FindArtifact(key)
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
							tracker.RemoveArtifact(key)
						} else {
							existing.Clients = updatedClients
							tracker.UpsertArtifact(*existing)
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

// installArtifacts installs artifacts to all detected clients using the orchestrator
func installArtifacts(ctx context.Context, successfulDownloads []*assets.ArtifactWithMetadata, gitContext *gitutil.GitContext, currentScope *scope.Scope, targetClients []clients.Client, out *outputHelper) *assets.InstallResult {
	out.println("Installing assets...")

	// Install each artifact to its proper scope
	// Global artifacts go to ~/.claude, repo-scoped artifacts go to {repoRoot}/.claude
	allResults := make(map[string]clients.InstallResponse)

	for _, download := range successfulDownloads {
		bundle := &clients.ArtifactBundle{
			Artifact: download.Artifact,
			Metadata: download.Metadata,
			ZipData:  download.ZipData,
		}

		// Determine installation scope based on the ARTIFACT's scope, not current directory
		installScope := buildInstallScopeForArtifact(download.Artifact, gitContext)

		// Run installation for this artifact
		results := runMultiClientInstallation(ctx, []*clients.ArtifactBundle{bundle}, installScope, targetClients)

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

// buildInstallScopeForArtifact creates the installation scope based on the artifact's own scope
// Global artifacts go to ~/.claude, repo-scoped artifacts go to {repoRoot}/.claude
func buildInstallScopeForArtifact(art *lockfile.Artifact, gitContext *gitutil.GitContext) *clients.InstallScope {
	if art.IsGlobal() {
		// Global artifact - install to ~/.claude
		return &clients.InstallScope{
			Type: clients.ScopeGlobal,
		}
	}

	// Repo or path-scoped artifact - install to repo's .claude directory
	installScope := &clients.InstallScope{
		Type: clients.ScopeRepository,
	}

	if gitContext.IsRepo {
		installScope.RepoRoot = gitContext.RepoRoot
		installScope.RepoURL = gitContext.RepoURL
	}

	// For path-scoped artifacts, we'd need to handle the path too
	// but for now we install to repo root
	return installScope
}

// runMultiClientInstallation executes installation across all clients concurrently
func runMultiClientInstallation(ctx context.Context, bundles []*clients.ArtifactBundle, installScope *clients.InstallScope, targetClients []clients.Client) map[string]clients.InstallResponse {
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

	installedArtifacts := make(map[string]bool)

	for clientID, resp := range allResults {
		client, _ := clients.Global().Get(clientID)

		for _, result := range resp.Results {
			switch result.Status {
			case clients.StatusSuccess:
				out.printf("  ✓ %s → %s\n", result.ArtifactName, client.DisplayName())
				installedArtifacts[result.ArtifactName] = true
			case clients.StatusFailed:
				out.printfErr("  ✗ %s → %s: %v\n", result.ArtifactName, client.DisplayName(), result.Error)
				installResult.Failed = append(installResult.Failed, result.ArtifactName)
				installResult.Errors = append(installResult.Errors, result.Error)
			case clients.StatusSkipped:
				// Don't print skipped artifacts
			}
		}
	}

	// Build list of successfully installed artifacts
	for name := range installedArtifacts {
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

// ensureSkillsSupport calls EnsureSkillsSupport on all clients to set up local rules files, etc.
func ensureSkillsSupport(ctx context.Context, targetClients []clients.Client, scope *clients.InstallScope, out *outputHelper) {
	log := logger.Get()
	for _, client := range targetClients {
		if err := client.EnsureSkillsSupport(ctx, scope); err != nil {
			out.printfErr("Warning: failed to ensure skills support for %s: %v\n", client.DisplayName(), err)
			log.Error("failed to ensure skills support", "client", client.ID(), "error", err)
		}
	}
}

// saveInstallationState saves the current installation state to tracker file
func saveInstallationState(tracker *assets.Tracker, sortedArtifacts []*lockfile.Artifact, currentScope *scope.Scope, targetClientIDs []string, out *outputHelper) {
	for _, art := range sortedArtifacts {
		key := artifactKeyForInstall(art, currentScope)
		tracker.UpsertArtifact(assets.InstalledArtifact{
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
