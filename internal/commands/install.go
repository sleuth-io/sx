package commands

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/skills/internal/artifacts"
	"github.com/sleuth-io/skills/internal/config"
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
		Long: `Read the sleuth.lock file, fetch artifacts from the configured repository,
and install them to ~/.claude/ directory.`,
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

	// Fetch lock file
	fmt.Println("Fetching lock file...")
	lockFileData, _, _, err := repo.GetLockFile(ctx, "")
	if err != nil {
		return fmt.Errorf("failed to fetch lock file: %w", err)
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

	fmt.Printf("Lock file version: %s (created by %s)\n", lockFile.LockVersion, lockFile.CreatedBy)
	fmt.Printf("Found %d artifacts\n", len(lockFile.Artifacts))
	fmt.Println()

	// Detect Git context
	gitContext, err := gitutil.DetectContext(ctx)
	if err != nil {
		return fmt.Errorf("failed to detect git context: %w", err)
	}

	// Build scope
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
		fmt.Printf("Git context: %s (path: %s)\n", gitContext.RepoURL, gitContext.RelativePath)
	} else {
		currentScope = &scope.Scope{
			Type: "global",
		}
		fmt.Println("Git context: not in a repository (global scope)")
	}
	fmt.Println()

	// Filter artifacts by client compatibility
	clientName := "claude-code"
	var compatibleArtifacts []lockfile.Artifact
	for _, artifact := range lockFile.Artifacts {
		if artifact.MatchesClient(clientName) {
			compatibleArtifacts = append(compatibleArtifacts, artifact)
		}
	}

	fmt.Printf("Filtered to %d artifacts compatible with %s\n", len(compatibleArtifacts), clientName)

	// Filter by scope
	applicableArtifacts := scope.FilterArtifacts(compatibleArtifacts, currentScope)
	fmt.Printf("Filtered to %d artifacts matching current scope\n", len(applicableArtifacts))
	fmt.Println()

	if len(applicableArtifacts) == 0 {
		fmt.Println("No artifacts to install.")
		return nil
	}

	// Resolve dependencies
	resolver := artifacts.NewDependencyResolver(lockFile)
	artifactPtrs := make([]*lockfile.Artifact, len(applicableArtifacts))
	for i := range applicableArtifacts {
		artifactPtrs[i] = &applicableArtifacts[i]
	}

	sortedArtifacts, err := resolver.Resolve(artifactPtrs)
	if err != nil {
		return fmt.Errorf("dependency resolution failed: %w", err)
	}

	fmt.Printf("Resolved %d artifacts (including dependencies)\n", len(sortedArtifacts))
	fmt.Println()

	// Download artifacts
	fmt.Println("Downloading artifacts...")
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
		fmt.Fprintf(os.Stderr, "\nDownload errors:\n")
		for _, err := range downloadErrors {
			fmt.Fprintf(os.Stderr, "  - %v\n", err)
		}
		fmt.Println()
	}

	fmt.Printf("Downloaded %d/%d artifacts successfully\n", len(successfulDownloads), len(sortedArtifacts))
	fmt.Println()

	if len(successfulDownloads) == 0 {
		return fmt.Errorf("no artifacts downloaded successfully")
	}

	// Determine target base directory
	claudeDir, err := utils.GetClaudeDir()
	if err != nil {
		return fmt.Errorf("failed to get Claude directory: %w", err)
	}

	targetBase := claudeDir
	if gitContext.IsRepo {
		// Use repo-specific base if applicable
		targetBase = gitContext.RepoRoot
	}

	// Install artifacts
	fmt.Println("Installing artifacts...")
	installer := artifacts.NewArtifactInstaller(repo, targetBase)
	installResult, err := installer.InstallAll(ctx, successfulDownloads)
	if err != nil {
		return fmt.Errorf("installation failed: %w", err)
	}

	// Report results
	fmt.Println()
	fmt.Printf("✓ Installed %d artifacts successfully\n", len(installResult.Installed))
	for _, name := range installResult.Installed {
		fmt.Printf("  - %s\n", name)
	}

	if len(installResult.Failed) > 0 {
		fmt.Println()
		fmt.Fprintf(os.Stderr, "✗ Failed to install %d artifacts:\n", len(installResult.Failed))
		for i, name := range installResult.Failed {
			fmt.Fprintf(os.Stderr, "  - %s: %v\n", name, installResult.Errors[i])
		}
		return fmt.Errorf("some artifacts failed to install")
	}

	return nil
}
