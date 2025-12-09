package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/skills/internal/artifacts"
	"github.com/sleuth-io/skills/internal/cache"
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
		if err := cache.SaveETag(repoURL, newETag); err != nil {
			out.printfErr("Warning: failed to save ETag: %v\n", err)
		}
		if err := cache.SaveLockFile(repoURL, lockFileData); err != nil {
			out.printfErr("Warning: failed to cache lock file: %v\n", err)
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

	// Filter artifacts by client compatibility and scope
	clientName := "claude-code"
	var applicableArtifacts []*lockfile.Artifact
	for i := range lockFile.Artifacts {
		artifact := &lockFile.Artifacts[i]
		if artifact.MatchesClient(clientName) && matcherScope.MatchesArtifact(artifact) {
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

	// Determine which artifacts need to be installed (new or changed versions)
	artifactsToInstall := determineArtifactsToInstall(previousInstall, sortedArtifacts, out)

	// Clean up artifacts that were removed from lock file
	cleanupRemovedArtifacts(ctx, previousInstall, sortedArtifacts, trackingBase, repo, out)

	// Early exit if nothing to install
	if len(artifactsToInstall) == 0 {
		// Save state even if nothing changed (updates timestamp)
		saveInstallationState(trackingBase, lockFile, sortedArtifacts, out)

		// Install Claude Code hooks even if no artifacts changed
		if err := installClaudeCodeHooks(claudeDir, out); err != nil {
			out.printfErr("\nWarning: failed to install hooks: %v\n", err)
		}

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
		for _, err := range downloadErrors {
			out.printfErr("  - %v\n", err)
		}
		out.println()
	}

	out.printf("Downloaded %d/%d artifacts successfully\n", len(successfulDownloads), len(artifactsToInstall))
	out.println()

	if len(successfulDownloads) == 0 {
		return fmt.Errorf("no artifacts downloaded successfully")
	}

	// Install artifacts to their appropriate locations
	installResult := installArtifacts(ctx, successfulDownloads, gitContext, currentScope, claudeDir, repo, out)

	// Save new installation state (saves ALL artifacts from lock file, not just changed ones)
	saveInstallationState(trackingBase, lockFile, sortedArtifacts, out)

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
		}
		return fmt.Errorf("some artifacts failed to install")
	}

	// Install Claude Code hooks
	if err := installClaudeCodeHooks(claudeDir, out); err != nil {
		out.printfErr("\nWarning: failed to install hooks: %v\n", err)
		// Don't fail the install command if hook installation fails
	}

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
		return &artifacts.InstalledArtifacts{
			Version:   artifacts.TrackerFormatVersion,
			Artifacts: []artifacts.InstalledArtifact{},
		}
	}
	return previousInstall
}

// determineArtifactsToInstall finds which artifacts need to be installed (new or changed)
func determineArtifactsToInstall(previousInstall *artifacts.InstalledArtifacts, sortedArtifacts []*lockfile.Artifact, out *outputHelper) []*lockfile.Artifact {
	log := logger.Get()
	artifactsToInstall := artifacts.FindChangedOrNewArtifacts(previousInstall, sortedArtifacts)

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

// cleanupRemovedArtifacts removes artifacts that are no longer in the lock file
func cleanupRemovedArtifacts(ctx context.Context, previousInstall *artifacts.InstalledArtifacts, sortedArtifacts []*lockfile.Artifact, trackingBase string, repo repository.Repository, out *outputHelper) {
	removedArtifacts := artifacts.FindRemovedArtifacts(previousInstall, sortedArtifacts)
	if len(removedArtifacts) == 0 {
		return
	}

	out.printf("\nCleaning up %d removed artifact(s)...\n", len(removedArtifacts))
	installer := artifacts.NewArtifactInstaller(repo, trackingBase)
	if err := installer.RemoveArtifacts(ctx, removedArtifacts); err != nil {
		out.printfErr("Warning: cleanup failed: %v\n", err)
		return
	}

	log := logger.Get()
	for _, artifact := range removedArtifacts {
		out.printf("  - Removed %s\n", artifact.Name)
		log.Info("artifact removed", "name", artifact.Name, "version", artifact.Version, "type", artifact.Type)
	}
}

// installArtifacts installs artifacts to their appropriate locations
func installArtifacts(ctx context.Context, successfulDownloads []*artifacts.ArtifactWithMetadata, gitContext *gitutil.GitContext, currentScope *scope.Scope, claudeDir string, repo repository.Repository, out *outputHelper) *artifacts.InstallResult {
	out.println("Installing artifacts...")

	installResult := &artifacts.InstallResult{
		Installed: []string{},
		Failed:    []string{},
		Errors:    []error{},
	}

	for _, item := range successfulDownloads {
		select {
		case <-ctx.Done():
			installResult.Failed = append(installResult.Failed, item.Artifact.Name)
			installResult.Errors = append(installResult.Errors, ctx.Err())
			continue
		default:
		}

		// Get all installation locations for this artifact in the current context
		var installLocations []string
		if gitContext.IsRepo {
			installLocations = scope.GetInstallLocations(item.Artifact, currentScope, gitContext.RepoRoot, claudeDir)
		} else {
			installLocations = []string{claudeDir}
		}

		if len(installLocations) == 0 {
			continue
		}

		// Install artifact to each location
		installFailed := false
		for _, targetBase := range installLocations {
			installer := artifacts.NewArtifactInstaller(repo, targetBase)
			err := installer.Install(ctx, item.Artifact, item.ZipData, item.Metadata)

			if err != nil {
				installResult.Failed = append(installResult.Failed, item.Artifact.Name)
				installResult.Errors = append(installResult.Errors, fmt.Errorf("%s: %w", item.Artifact.Name, err))
				installFailed = true
				break
			}
		}

		if !installFailed {
			installResult.Installed = append(installResult.Installed, item.Artifact.Name)
		}
	}

	return installResult
}

// saveInstallationState saves the current installation state to tracker file
func saveInstallationState(trackingBase string, lockFile *lockfile.LockFile, sortedArtifacts []*lockfile.Artifact, out *outputHelper) {
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
			// InstallPath will be populated in future enhancement
		})
	}

	if err := artifacts.SaveInstalledArtifacts(trackingBase, newInstall); err != nil {
		out.printfErr("Warning: failed to save installation state: %v\n", err)
	}
}
