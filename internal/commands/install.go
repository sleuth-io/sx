package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/skills/internal/artifact"
	"github.com/sleuth-io/skills/internal/artifacts"
	"github.com/sleuth-io/skills/internal/cache"
	"github.com/sleuth-io/skills/internal/clients"
	"github.com/sleuth-io/skills/internal/config"
	"github.com/sleuth-io/skills/internal/constants"
	"github.com/sleuth-io/skills/internal/gitutil"
	"github.com/sleuth-io/skills/internal/lockfile"
	"github.com/sleuth-io/skills/internal/logger"
	"github.com/sleuth-io/skills/internal/repository"
	"github.com/sleuth-io/skills/internal/scope"
	"github.com/sleuth-io/skills/internal/utils"
)

// NewInstallCommand creates the install command
func NewInstallCommand() *cobra.Command {
	var hookMode bool

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Read lock file, fetch artifacts, and install locally",
		Long: fmt.Sprintf(`Read the %s file, fetch artifacts from the configured repository,
and install them to ~/.claude/ directory.`, constants.SkillLockFile),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInstall(cmd, args, hookMode)
		},
	}

	cmd.Flags().BoolVar(&hookMode, "hook-mode", false, "Run in hook mode (outputs JSON for Claude Code)")
	_ = cmd.Flags().MarkHidden("hook-mode") // Hide from help output since it's internal

	return cmd
}

// runInstall executes the install command
func runInstall(cmd *cobra.Command, args []string, hookMode bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	out := newOutputHelper(cmd)
	out.silent = hookMode // Suppress normal output in hook mode

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w\nRun 'skills init' to configure", err)
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// Create repository instance
	repo, err := repository.NewFromConfig(cfg)
	if err != nil {
		return fmt.Errorf("failed to create repository: %w", err)
	}

	// Fetch lock file with ETag caching
	out.println("Fetching lock file...")

	var cachedETag string
	var repoURL string
	if cfg.Type == config.RepositoryTypeSleuth {
		repoURL = cfg.GetServerURL()
		cachedETag, _ = cache.LoadETag(repoURL)
	}

	lockFileData, newETag, notModified, err := repo.GetLockFile(ctx, cachedETag)
	if err != nil {
		return fmt.Errorf("failed to fetch lock file: %w", err)
	}

	if notModified && repoURL != "" {
		out.println("Lock file unchanged (using cached version)")
		lockFileData, err = cache.LoadLockFile(repoURL)
		if err != nil {
			return fmt.Errorf("failed to load cached lock file: %w", err)
		}
	} else if repoURL != "" && newETag != "" {
		// Save new ETag and lock file content
		log := logger.Get()
		if err := cache.SaveETag(repoURL, newETag); err != nil {
			out.printfErr("Warning: failed to save ETag: %v\n", err)
			log.Error("failed to save ETag", "repo_url", repoURL, "error", err)
		}
		if err := cache.SaveLockFile(repoURL, lockFileData); err != nil {
			out.printfErr("Warning: failed to cache lock file: %v\n", err)
			log.Error("failed to cache lock file", "repo_url", repoURL, "error", err)
		}
	}

	// Parse lock file
	lockFile, err := lockfile.Parse(lockFileData)
	if err != nil {
		return fmt.Errorf("failed to parse lock file: %w", err)
	}

	// Validate lock file
	if err := lockFile.Validate(); err != nil {
		return fmt.Errorf("lock file validation failed: %w", err)
	}

	out.printf("Lock file version: %s (created by %s)\n", lockFile.LockVersion, lockFile.CreatedBy)
	out.printf("Found %d artifacts\n", len(lockFile.Artifacts))
	out.println()

	// Detect Git context
	gitContext, err := gitutil.DetectContext(ctx)
	if err != nil {
		return fmt.Errorf("failed to detect git context: %w", err)
	}

	// Build scope and matcher
	var currentScope *scope.Scope
	if gitContext.IsRepo {
		if gitContext.RelativePath == "." {
			currentScope = &scope.Scope{
				Type:     "repo",
				RepoURL:  gitContext.RepoURL,
				RepoPath: "",
			}
		} else {
			currentScope = &scope.Scope{
				Type:     "path",
				RepoURL:  gitContext.RepoURL,
				RepoPath: gitContext.RelativePath,
			}
		}
		out.printf("Git context: %s (path: %s)\n", gitContext.RepoURL, gitContext.RelativePath)
	} else {
		currentScope = &scope.Scope{
			Type: "global",
		}
		out.println("Git context: not in a repository (global scope)")
	}
	out.println()

	matcherScope := scope.NewMatcher(currentScope)

	// Detect installed clients
	registry := clients.Global()
	targetClients := registry.DetectInstalled()
	if len(targetClients) == 0 {
		return fmt.Errorf("no AI coding clients detected")
	}

	// Display detected clients
	clientNames := make([]string, len(targetClients))
	for i, client := range targetClients {
		clientNames[i] = client.DisplayName()
	}
	out.printf("Detected clients: %s\n", strings.Join(clientNames, ", "))
	out.println()

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

	out.printf("Found %d artifacts matching current scope\n", len(applicableArtifacts))
	out.println()

	if len(applicableArtifacts) == 0 {
		out.println("No artifacts to install.")
		return nil
	}

	// Resolve dependencies
	resolver := artifacts.NewDependencyResolver(lockFile)
	sortedArtifacts, err := resolver.Resolve(applicableArtifacts)
	if err != nil {
		return fmt.Errorf("dependency resolution failed: %w", err)
	}

	out.printf("Resolved %d artifacts (including dependencies)\n", len(sortedArtifacts))
	out.println()

	// Determine base directories
	claudeDir, err := utils.GetClaudeDir()
	if err != nil {
		return fmt.Errorf("failed to get Claude directory: %w", err)
	}

	// For cleanup and tracking, use a consistent base (repo root if in repo, otherwise global)
	trackingBase := claudeDir
	if gitContext.IsRepo {
		trackingBase = filepath.Join(gitContext.RepoRoot, ".claude")
	}

	// Load previous installation state
	previousInstall := loadPreviousInstallState(trackingBase, out)

	// Determine which artifacts need to be installed (new or changed versions or missing from clients)
	targetClientIDs := make([]string, len(targetClients))
	for i, client := range targetClients {
		targetClientIDs[i] = client.ID()
	}
	artifactsToInstall := determineArtifactsToInstall(previousInstall, sortedArtifacts, targetClientIDs, out)

	// Clean up artifacts that were removed from lock file
	cleanupRemovedArtifacts(ctx, previousInstall, sortedArtifacts, gitContext, currentScope, targetClients, out)

	// Early exit if nothing to install
	if len(artifactsToInstall) == 0 {
		// Save state even if nothing changed (updates timestamp)
		saveInstallationState(trackingBase, lockFile, sortedArtifacts, targetClientIDs, out)

		// Install client-specific hooks (e.g., auto-update, usage tracking)
		installClientHooks(ctx, targetClients, out)

		// Ensure skills support is configured for all clients (creates local rules files, etc.)
		// This is important even when no new artifacts are installed, as the local rules file
		// may not exist yet (e.g., running in a new repo with only global skills)
		ensureSkillsSupport(ctx, targetClients, buildInstallScope(currentScope, gitContext), out)

		// In hook mode, output JSON even when nothing changed
		if hookMode {
			response := map[string]interface{}{
				"continue": true,
			}
			jsonBytes, err := json.MarshalIndent(response, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal JSON response: %w", err)
			}
			out.printlnAlways(string(jsonBytes))
		} else {
			out.println("\n✓ No changes needed")
		}

		return nil
	}

	out.println()

	// Download only the artifacts that need to be installed
	out.println("Downloading artifacts...")
	fetcher := artifacts.NewArtifactFetcher(repo)
	results, err := fetcher.FetchArtifacts(ctx, artifactsToInstall, 10)
	if err != nil {
		return fmt.Errorf("failed to fetch artifacts: %w", err)
	}

	// Check for download errors
	var downloadErrors []error
	var successfulDownloads []*artifacts.ArtifactWithMetadata
	for _, result := range results {
		if result.Error != nil {
			downloadErrors = append(downloadErrors, fmt.Errorf("%s: %w", result.Artifact.Name, result.Error))
		} else {
			successfulDownloads = append(successfulDownloads, &artifacts.ArtifactWithMetadata{
				Artifact: result.Artifact,
				Metadata: result.Metadata,
				ZipData:  result.ZipData,
			})
		}
	}

	if len(downloadErrors) > 0 {
		out.printErr("\nDownload errors:")
		log := logger.Get()
		for _, err := range downloadErrors {
			out.printfErr("  - %v\n", err)
			log.Error("artifact download failed", "error", err)
		}
		out.println()
	}

	out.printf("Downloaded %d/%d artifacts successfully\n", len(successfulDownloads), len(artifactsToInstall))
	out.println()

	if len(successfulDownloads) == 0 {
		return fmt.Errorf("no artifacts downloaded successfully")
	}

	// Install artifacts to their appropriate locations
	installResult := installArtifacts(ctx, successfulDownloads, gitContext, currentScope, targetClients, out)

	// Save new installation state (saves ALL artifacts from lock file, not just changed ones)
	saveInstallationState(trackingBase, lockFile, sortedArtifacts, targetClientIDs, out)

	// Ensure skills support is configured for all clients (creates local rules files, etc.)
	ensureSkillsSupport(ctx, targetClients, buildInstallScope(currentScope, gitContext), out)

	// Report results
	out.println()
	out.printf("✓ Installed %d artifacts successfully\n", len(installResult.Installed))

	// Log successful installations
	log := logger.Get()
	for _, name := range installResult.Installed {
		out.printf("  - %s\n", name)
		// Find version for this artifact
		for _, art := range successfulDownloads {
			if art.Artifact.Name == name {
				log.Info("artifact installed", "name", name, "version", art.Artifact.Version, "type", art.Metadata.Artifact.Type, "scope", currentScope.Type)
				break
			}
		}
	}

	if len(installResult.Failed) > 0 {
		out.println()
		out.printfErr("✗ Failed to install %d artifacts:\n", len(installResult.Failed))
		for i, name := range installResult.Failed {
			out.printfErr("  - %s: %v\n", name, installResult.Errors[i])
			log.Error("artifact installation failed", "name", name, "error", installResult.Errors[i])
		}
		return fmt.Errorf("some artifacts failed to install")
	}

	// Install client-specific hooks (e.g., auto-update, usage tracking)
	installClientHooks(ctx, targetClients, out)

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
			message = fmt.Sprintf("%sSleuth Skills%s installed the %s%s %s%s. %sRestart Claude Code to use it.%s",
				bold, resetBold, blue, artifacts[0].name, artifacts[0].typ, reset, red, reset)
		} else if len(artifacts) <= 3 {
			// List all items
			message = fmt.Sprintf("%sSleuth Skills%s installed:\n", bold, resetBold)
			for _, art := range artifacts {
				message += fmt.Sprintf("- The %s%s %s%s\n", blue, art.name, art.typ, reset)
			}
			message += fmt.Sprintf("\n%sRestart Claude Code to use them.%s", red, reset)
		} else {
			// Show first 3 and count remaining
			message = fmt.Sprintf("%sSleuth Skills%s installed:\n", bold, resetBold)
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

// loadPreviousInstallState loads the previous installation state from tracker file
func loadPreviousInstallState(trackingBase string, out *outputHelper) *artifacts.InstalledArtifacts {
	previousInstall, err := artifacts.LoadInstalledArtifacts(trackingBase)
	if err != nil {
		out.printfErr("Warning: failed to load previous installation state: %v\n", err)
		log := logger.Get()
		log.Error("failed to load previous installation state", "tracking_base", trackingBase, "error", err)
		return &artifacts.InstalledArtifacts{
			Version:   artifacts.TrackerFormatVersion,
			Artifacts: []artifacts.InstalledArtifact{},
		}
	}
	return previousInstall
}

// determineArtifactsToInstall finds which artifacts need to be installed (new or changed)
func determineArtifactsToInstall(previousInstall *artifacts.InstalledArtifacts, sortedArtifacts []*lockfile.Artifact, targetClientIDs []string, out *outputHelper) []*lockfile.Artifact {
	log := logger.Get()
	artifactsToInstall := artifacts.FindArtifactsToInstallForClients(previousInstall, sortedArtifacts, targetClientIDs)

	// Log version updates
	for _, artifact := range artifactsToInstall {
		for _, prev := range previousInstall.Artifacts {
			if prev.Name == artifact.Name && prev.Version != artifact.Version {
				log.Info("artifact version update", "name", artifact.Name, "old_version", prev.Version, "new_version", artifact.Version)
			}
		}
	}

	if len(artifactsToInstall) == 0 {
		out.println("✓ All artifacts are up to date")
		return artifactsToInstall
	}

	if len(artifactsToInstall) < len(sortedArtifacts) {
		skipped := len(sortedArtifacts) - len(artifactsToInstall)
		out.printf("Found %d new/changed artifact(s), %d unchanged\n", len(artifactsToInstall), skipped)
	}

	return artifactsToInstall
}

// cleanupRemovedArtifacts removes artifacts that are no longer in the lock file from all clients
func cleanupRemovedArtifacts(ctx context.Context, previousInstall *artifacts.InstalledArtifacts, sortedArtifacts []*lockfile.Artifact, gitContext *gitutil.GitContext, currentScope *scope.Scope, targetClients []clients.Client, out *outputHelper) {
	removedArtifacts := artifacts.FindRemovedArtifacts(previousInstall, sortedArtifacts)
	if len(removedArtifacts) == 0 {
		return
	}

	out.printf("\nCleaning up %d removed artifact(s)...\n", len(removedArtifacts))

	// Build uninstall scope
	uninstallScope := buildInstallScope(currentScope, gitContext)

	// Convert InstalledArtifact to artifact.Artifact for uninstall
	artifactsToRemove := make([]artifact.Artifact, len(removedArtifacts))
	for i, installed := range removedArtifacts {
		artifactsToRemove[i] = artifact.Artifact{
			Name:    installed.Name,
			Version: installed.Version,
			Type:    installed.Type,
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
}

// installArtifacts installs artifacts to all detected clients using the orchestrator
func installArtifacts(ctx context.Context, successfulDownloads []*artifacts.ArtifactWithMetadata, gitContext *gitutil.GitContext, currentScope *scope.Scope, targetClients []clients.Client, out *outputHelper) *artifacts.InstallResult {
	out.println("Installing artifacts...")

	// Convert downloads to bundles
	bundles := convertToArtifactBundles(successfulDownloads)

	// Determine installation scope
	installScope := buildInstallScope(currentScope, gitContext)

	// Run installation across all clients
	allResults := runMultiClientInstallation(ctx, bundles, installScope, targetClients)

	// Process and report results
	return processInstallationResults(allResults, out)
}

// convertToArtifactBundles converts downloaded artifacts to client bundles
func convertToArtifactBundles(downloads []*artifacts.ArtifactWithMetadata) []*clients.ArtifactBundle {
	bundles := make([]*clients.ArtifactBundle, len(downloads))
	for i, item := range downloads {
		bundles[i] = &clients.ArtifactBundle{
			Artifact: item.Artifact,
			Metadata: item.Metadata,
			ZipData:  item.ZipData,
		}
	}
	return bundles
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

// runMultiClientInstallation executes installation across all clients concurrently
func runMultiClientInstallation(ctx context.Context, bundles []*clients.ArtifactBundle, installScope *clients.InstallScope, targetClients []clients.Client) map[string]clients.InstallResponse {
	orchestrator := clients.NewOrchestrator(clients.Global())
	return orchestrator.InstallToClients(ctx, bundles, installScope, clients.InstallOptions{}, targetClients)
}

// processInstallationResults processes results from all clients and builds the final result
func processInstallationResults(allResults map[string]clients.InstallResponse, out *outputHelper) *artifacts.InstallResult {
	installResult := &artifacts.InstallResult{
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
func saveInstallationState(trackingBase string, lockFile *lockfile.LockFile, sortedArtifacts []*lockfile.Artifact, targetClientIDs []string, out *outputHelper) {
	newInstall := &artifacts.InstalledArtifacts{
		Version:         artifacts.TrackerFormatVersion,
		LockFileVersion: lockFile.Version,
		InstalledAt:     time.Now(),
		Artifacts:       []artifacts.InstalledArtifact{},
	}

	for _, artifact := range sortedArtifacts {
		newInstall.Artifacts = append(newInstall.Artifacts, artifacts.InstalledArtifact{
			Name:    artifact.Name,
			Version: artifact.Version,
			Type:    artifact.Type,
			Clients: targetClientIDs, // Track which clients this was installed to
			// InstallPath will be populated in future enhancement
		})
	}

	if err := artifacts.SaveInstalledArtifacts(trackingBase, newInstall); err != nil {
		out.printfErr("Warning: failed to save installation state: %v\n", err)
		log := logger.Get()
		log.Error("failed to save installation state", "tracking_base", trackingBase, "error", err)
	}
}
