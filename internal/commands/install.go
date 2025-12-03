package commands

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/skills/internal/artifacts"
	"github.com/sleuth-io/skills/internal/cache"
	"github.com/sleuth-io/skills/internal/config"
	"github.com/sleuth-io/skills/internal/constants"
	"github.com/sleuth-io/skills/internal/gitutil"
	"github.com/sleuth-io/skills/internal/lockfile"
	"github.com/sleuth-io/skills/internal/repository"
	"github.com/sleuth-io/skills/internal/scope"
	"github.com/sleuth-io/skills/internal/utils"
)

// NewInstallCommand creates the install command
func NewInstallCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Read lock file, fetch artifacts, and install locally",
		Long: fmt.Sprintf(`Read the %s file, fetch artifacts from the configured repository,
and install them to ~/.claude/ directory.`, constants.SkillLockFile),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInstall(cmd, args)
		},
	}

	return cmd
}

// runInstall executes the install command
func runInstall(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	out := newOutputHelper(cmd)

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
	var repo repository.Repository
	switch cfg.Type {
	case config.RepositoryTypeSleuth:
		repo = repository.NewSleuthRepository(cfg.GetServerURL(), cfg.AuthToken)
	case config.RepositoryTypeGit:
		repo, err = repository.NewGitRepository(cfg.RepositoryURL)
		if err != nil {
			return fmt.Errorf("failed to create git repository: %w", err)
		}
	default:
		return fmt.Errorf("unsupported repository type: %s", cfg.Type)
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

	// Download artifacts
	out.println("Downloading artifacts...")
	fetcher := artifacts.NewArtifactFetcher(repo)
	results, err := fetcher.FetchArtifacts(ctx, sortedArtifacts, 10)
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

	out.printf("Downloaded %d/%d artifacts successfully\n", len(successfulDownloads), len(sortedArtifacts))
	out.println()

	if len(successfulDownloads) == 0 {
		return fmt.Errorf("no artifacts downloaded successfully")
	}

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
	previousInstall, err := artifacts.LoadInstalledArtifacts(trackingBase)
	if err != nil {
		out.printfErr("Warning: failed to load previous installation state: %v\n", err)
		previousInstall = &artifacts.InstalledArtifacts{
			Version:   "1.0",
			Artifacts: []artifacts.InstalledArtifact{},
		}
	}

	// Find removed artifacts
	removedArtifacts := artifacts.FindRemovedArtifacts(previousInstall, sortedArtifacts)
	if len(removedArtifacts) > 0 {
		out.printf("\nCleaning up %d removed artifacts...\n", len(removedArtifacts))
		installer := artifacts.NewArtifactInstaller(repo, trackingBase)
		if err := installer.RemoveArtifacts(ctx, removedArtifacts); err != nil {
			out.printfErr("Warning: cleanup failed: %v\n", err)
		} else {
			for _, artifact := range removedArtifacts {
				out.printf("  - Removed %s\n", artifact.Name)
			}
		}
	}

	// Install each artifact to its appropriate location based on scope
	out.println("Installing artifacts...")

	installResult := &artifacts.InstallResult{
		Installed: []string{},
		Failed:    []string{},
		Errors:    []error{},
	}

	for _, item := range successfulDownloads {
		select {
		case <-ctx.Done():
			return ctx.Err()
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
			// No installation locations, skip (shouldn't happen but be safe)
			continue
		}

		// Install artifact to each location
		for _, targetBase := range installLocations {
			// Install the artifact
			installer := artifacts.NewArtifactInstaller(repo, targetBase)
			err := installer.Install(ctx, item.Artifact, item.ZipData, item.Metadata)

			if err != nil {
				installResult.Failed = append(installResult.Failed, item.Artifact.Name)
				installResult.Errors = append(installResult.Errors, fmt.Errorf("%s: %w", item.Artifact.Name, err))
				continue
			}
		}

		// Mark as installed once (even if installed to multiple locations)
		installResult.Installed = append(installResult.Installed, item.Artifact.Name)
	}

	// Save new installation state
	newInstall := &artifacts.InstalledArtifacts{
		Version:         "1.0",
		LockFileVersion: lockFile.Version,
		InstalledAt:     time.Now(),
		Artifacts:       []artifacts.InstalledArtifact{},
	}

	for _, artifact := range sortedArtifacts {
		newInstall.Artifacts = append(newInstall.Artifacts, artifacts.InstalledArtifact{
			Name:    artifact.Name,
			Version: artifact.Version,
			Type:    string(artifact.Type),
			// InstallPath will be determined by handler
		})
	}

	if err := artifacts.SaveInstalledArtifacts(trackingBase, newInstall); err != nil {
		out.printfErr("Warning: failed to save installation state: %v\n", err)
	}

	// Report results
	out.println()
	out.printf("✓ Installed %d artifacts successfully\n", len(installResult.Installed))
	for _, name := range installResult.Installed {
		out.printf("  - %s\n", name)
	}

	if len(installResult.Failed) > 0 {
		out.println()
		out.printfErr("✗ Failed to install %d artifacts:\n", len(installResult.Failed))
		for i, name := range installResult.Failed {
			out.printfErr("  - %s: %v\n", name, installResult.Errors[i])
		}
		return fmt.Errorf("some artifacts failed to install")
	}

	return nil
}
