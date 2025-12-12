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

	"github.com/sleuth-io/skills/internal/artifact"
	"github.com/sleuth-io/skills/internal/artifacts"
	"github.com/sleuth-io/skills/internal/cache"
	"github.com/sleuth-io/skills/internal/clients"
	"github.com/sleuth-io/skills/internal/config"
	"github.com/sleuth-io/skills/internal/gitutil"
	"github.com/sleuth-io/skills/internal/lockfile"
	"github.com/sleuth-io/skills/internal/logger"
	"github.com/sleuth-io/skills/internal/repository"
)

// NewUninstallCommand creates the uninstall command
func NewUninstallCommand() *cobra.Command {
	var all bool
	var yes bool
	var dryRun bool
	var verbose bool

	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Uninstall all artifacts from the current scope or all scopes",
		Long: `Uninstall removes all installed artifacts from the current scope (global, repository, or path).

Examples:
  # Uninstall from current scope (prompts for confirmation)
  skills uninstall

  # Uninstall from all scopes (global + all repositories)
  skills uninstall --all

  # Preview what would be uninstalled without making changes
  skills uninstall --dry-run

  # Skip confirmation prompt
  skills uninstall --yes

  # Uninstall from all scopes without confirmation
  skills uninstall --all --yes`,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts := UninstallOptions{
				All:     all,
				Yes:     yes,
				DryRun:  dryRun,
				Verbose: verbose,
			}
			return runUninstall(cmd, args, opts)
		},
	}

	cmd.Flags().BoolVar(&all, "all", false, "Uninstall from all scopes (global + all repositories)")
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be uninstalled without removing")
	cmd.Flags().BoolVar(&verbose, "verbose", false, "Verbose output")

	return cmd
}

// UninstallOptions contains options for the uninstall command
type UninstallOptions struct {
	All     bool
	Yes     bool
	DryRun  bool
	Verbose bool
}

// ArtifactUninstallPlan contains info needed to uninstall one artifact
type ArtifactUninstallPlan struct {
	Name      string
	Version   string
	Type      artifact.Type
	IsGlobal  bool
	Clients   []string // client IDs that have this installed
	LockEntry *lockfile.Artifact
}

// UninstallPlan contains the complete uninstall plan
type UninstallPlan struct {
	Artifacts  []ArtifactUninstallPlan
	GitContext *gitutil.GitContext
	TargetBase string // tracking base for updating tracker
}

// UninstallResult tracks what was uninstalled
type UninstallResult struct {
	ArtifactName string
	ClientID     string
	Success      bool
	Error        error
}

// runUninstall executes the uninstall command
func runUninstall(cmd *cobra.Command, args []string, opts UninstallOptions) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	out := newOutputHelper(cmd)

	// Step 1: Load lock file
	lockFile, err := loadLockFileForUninstall(ctx, out)
	if err != nil {
		// If --all is passed, we should still remove system hooks even if lock file fails
		if opts.All {
			out.printfErr("Warning: %v\n", err)
			return handleAllFlagWithoutArtifacts(ctx, opts, out)
		}
		return err
	}

	// Step 2: Detect context and load tracker
	gitContext, tracker, trackingBase, err := loadTrackerForUninstall(ctx, out)
	if err != nil {
		// If --all is passed, we should still remove system hooks even if tracker fails
		if opts.All {
			out.printfErr("Warning: %v\n", err)
			return handleAllFlagWithoutArtifacts(ctx, opts, out)
		}
		return err
	}

	if len(tracker.Artifacts) == 0 {
		// No artifacts, but if --all is passed, still remove system hooks
		if opts.All {
			return handleAllFlagWithoutArtifacts(ctx, opts, out)
		}
		out.println("No artifacts installed")
		return nil
	}

	// Step 3: Build uninstall plan
	plan := buildUninstallPlanFromTracker(lockFile, tracker, gitContext, trackingBase)

	if len(plan.Artifacts) == 0 {
		// No artifacts to uninstall, but if --all is passed, still remove system hooks
		if opts.All {
			return handleAllFlagWithoutArtifacts(ctx, opts, out)
		}
		out.println("No artifacts to uninstall")
		return nil
	}

	// Step 4: Display plan and confirm
	displayUninstallPlan(plan, out)

	if opts.All {
		out.println("System hooks will also be removed (--all flag)")
	}

	if !opts.Yes && !opts.DryRun {
		if !confirmUninstall(out) {
			out.println("Uninstall cancelled")
			return nil
		}
	}

	if opts.DryRun {
		out.println("\nNo changes made (dry run).")
		return nil
	}

	// Step 5: Execute uninstall
	out.println("\nUninstalling artifacts...")
	results := executeUninstall(ctx, plan, opts, out)

	// Step 6: Update tracker
	if err := updateTracker(results, plan, out); err != nil {
		out.printfErr("Warning: failed to update tracker: %v\n", err)
		logger.Get().Error("failed to update tracker", "error", err)
	}

	// Step 7: Regenerate client support
	regenerateClientSupport(ctx, plan, results, out)

	// Step 8: Uninstall system hooks if --all flag is passed
	if opts.All {
		uninstallSystemHooks(ctx, out)
	}

	// Step 9: Report results
	reportResults(results, out)

	return nil
}

// handleAllFlagWithoutArtifacts handles the --all flag when there are no artifacts to uninstall
func handleAllFlagWithoutArtifacts(ctx context.Context, opts UninstallOptions, out *outputHelper) error {
	out.println("No artifacts to uninstall")

	if !opts.Yes && !opts.DryRun {
		out.println("\nSystem hooks will be removed (--all flag)")
		if !confirmUninstall(out) {
			out.println("Uninstall cancelled")
			return nil
		}
	}

	if opts.DryRun {
		out.println("\nWould remove system hooks (dry run).")
		return nil
	}

	uninstallSystemHooks(ctx, out)
	out.println("\nSystem hooks removed")
	return nil
}

// loadLockFileForUninstall loads config and fetches the lock file
func loadLockFileForUninstall(ctx context.Context, out *outputHelper) (*lockfile.LockFile, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load configuration: %w\nRun 'skills init' to configure", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	repo, err := repository.NewFromConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create repository: %w", err)
	}

	out.println("Fetching lock file...")

	var cachedETag string
	var repoURL string
	if cfg.Type == config.RepositoryTypeSleuth {
		repoURL = cfg.GetServerURL()
		cachedETag, _ = cache.LoadETag(repoURL)
	}

	lockFileData, _, notModified, err := repo.GetLockFile(ctx, cachedETag)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch lock file: %w", err)
	}

	if notModified && repoURL != "" {
		lockFileData, err = cache.LoadLockFile(repoURL)
		if err != nil {
			return nil, fmt.Errorf("failed to load cached lock file: %w", err)
		}
	}

	lockFile, err := lockfile.Parse(lockFileData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse lock file: %w", err)
	}

	return lockFile, nil
}

// loadTrackerForUninstall detects git context and loads the installation tracker
func loadTrackerForUninstall(ctx context.Context, out *outputHelper) (*gitutil.GitContext, *artifacts.InstalledArtifacts, string, error) {
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

	out.println("Loading installation state...")
	tracker, err := artifacts.LoadInstalledArtifacts(trackingBase)
	if err != nil {
		return nil, nil, "", fmt.Errorf("failed to load tracker: %w", err)
	}

	return gitContext, tracker, trackingBase, nil
}

// buildUninstallPlanFromTracker creates the uninstall plan by matching tracker with lock file
func buildUninstallPlanFromTracker(lockFile *lockfile.LockFile, tracker *artifacts.InstalledArtifacts, gitContext *gitutil.GitContext, trackingBase string) UninstallPlan {
	log := logger.Get()

	// Build artifact lookup from lock file
	lockFileMap := make(map[string]*lockfile.Artifact)
	for i := range lockFile.Artifacts {
		lockFileMap[lockFile.Artifacts[i].Name] = &lockFile.Artifacts[i]
	}

	plan := UninstallPlan{
		GitContext: gitContext,
		TargetBase: trackingBase,
	}

	for _, installed := range tracker.Artifacts {
		lockEntry, found := lockFileMap[installed.Name]
		if !found {
			// Artifact in tracker but not in lock file - still uninstall it
			log.Warn("artifact in tracker but not in lock file", "name", installed.Name)
			plan.Artifacts = append(plan.Artifacts, ArtifactUninstallPlan{
				Name:     installed.Name,
				Version:  installed.Version,
				Type:     installed.Type,
				IsGlobal: true, // assume global if not in lock file
				Clients:  installed.Clients,
			})
			continue
		}

		plan.Artifacts = append(plan.Artifacts, ArtifactUninstallPlan{
			Name:      installed.Name,
			Version:   installed.Version,
			Type:      installed.Type,
			IsGlobal:  lockEntry.IsGlobal(),
			Clients:   installed.Clients,
			LockEntry: lockEntry,
		})
	}

	return plan
}

// executeUninstall performs the actual uninstallation
func executeUninstall(ctx context.Context, plan UninstallPlan, opts UninstallOptions, out *outputHelper) []UninstallResult {
	var results []UninstallResult
	registry := clients.Global()

	for _, artPlan := range plan.Artifacts {
		for _, clientID := range artPlan.Clients {
			result := uninstallArtifactFromClient(ctx, artPlan, clientID, plan.GitContext, opts, registry, out)
			results = append(results, result)
		}
	}

	return results
}

// uninstallArtifactFromClient removes a single artifact from a single client
func uninstallArtifactFromClient(ctx context.Context, artPlan ArtifactUninstallPlan, clientID string, gitContext *gitutil.GitContext, opts UninstallOptions, registry *clients.Registry, out *outputHelper) UninstallResult {
	client, err := registry.Get(clientID)
	if err != nil || client == nil {
		out.printfErr("  ✗ Client %s not found, skipping\n", clientID)
		return UninstallResult{
			ArtifactName: artPlan.Name,
			ClientID:     clientID,
			Success:      false,
			Error:        fmt.Errorf("client not found: %s", clientID),
		}
	}

	// Build the correct scope based on artifact's scope from lock file
	installScope := buildScopeForArtifact(artPlan, gitContext)

	req := clients.UninstallRequest{
		Artifacts: []artifact.Artifact{
			{
				Name:    artPlan.Name,
				Version: artPlan.Version,
				Type:    artPlan.Type,
			},
		},
		Scope: installScope,
		Options: clients.UninstallOptions{
			DryRun:  opts.DryRun,
			Verbose: opts.Verbose,
		},
	}

	resp, err := client.UninstallArtifacts(ctx, req)
	if err != nil {
		out.printfErr("  ✗ Failed to uninstall %s from %s: %v\n", artPlan.Name, client.DisplayName(), err)
		return UninstallResult{
			ArtifactName: artPlan.Name,
			ClientID:     clientID,
			Success:      false,
			Error:        err,
		}
	}

	success := len(resp.Results) > 0 && resp.Results[0].Status == clients.StatusSuccess
	if success {
		out.printf("  ✓ Removed %s from %s\n", artPlan.Name, client.DisplayName())
	} else {
		errMsg := "unknown error"
		if len(resp.Results) > 0 && resp.Results[0].Error != nil {
			errMsg = resp.Results[0].Error.Error()
		}
		out.printfErr("  ✗ Failed to remove %s from %s: %s\n", artPlan.Name, client.DisplayName(), errMsg)
	}

	return UninstallResult{
		ArtifactName: artPlan.Name,
		ClientID:     clientID,
		Success:      success,
	}
}

// buildScopeForArtifact creates the correct InstallScope based on artifact's scope
func buildScopeForArtifact(artPlan ArtifactUninstallPlan, gitContext *gitutil.GitContext) *clients.InstallScope {
	if artPlan.IsGlobal {
		return &clients.InstallScope{
			Type: clients.ScopeGlobal,
		}
	}

	// Non-global artifact - use repo scope
	return &clients.InstallScope{
		Type:     clients.ScopeRepository,
		RepoRoot: gitContext.RepoRoot,
		RepoURL:  gitContext.RepoURL,
	}
}

// updateTracker removes successfully uninstalled artifacts from tracker
func updateTracker(results []UninstallResult, plan UninstallPlan, out *outputHelper) error {
	out.println("\nUpdating installation state...")

	fullyRemoved := findFullyRemovedArtifacts(results)
	if len(fullyRemoved) == 0 {
		return nil
	}

	tracker, err := artifacts.LoadInstalledArtifacts(plan.TargetBase)
	if err != nil {
		return fmt.Errorf("failed to load tracker: %w", err)
	}

	updated := artifacts.RemoveArtifactsFromTracker(tracker, fullyRemoved)

	trackerPath := artifacts.GetTrackerPath(plan.TargetBase)
	if updated == nil || len(updated.Artifacts) == 0 {
		return artifacts.DeleteTracker(trackerPath)
	}

	return artifacts.SaveInstalledArtifacts(plan.TargetBase, updated)
}

// findFullyRemovedArtifacts returns artifacts where all client removals succeeded
func findFullyRemovedArtifacts(results []UninstallResult) []string {
	// Group results by artifact
	artifactClients := make(map[string]map[string]bool)
	for _, result := range results {
		if _, exists := artifactClients[result.ArtifactName]; !exists {
			artifactClients[result.ArtifactName] = make(map[string]bool)
		}
		artifactClients[result.ArtifactName][result.ClientID] = result.Success
	}

	// Find artifacts where all succeeded
	var fullyRemoved []string
	for artName, clientResults := range artifactClients {
		allSuccess := true
		for _, success := range clientResults {
			if !success {
				allSuccess = false
				break
			}
		}
		if allSuccess {
			fullyRemoved = append(fullyRemoved, artName)
		}
	}

	return fullyRemoved
}

// regenerateClientSupport calls EnsureSkillsSupport on affected clients
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

		if err := client.EnsureSkillsSupport(ctx, scope); err != nil {
			out.printfErr("Warning: failed to regenerate support for %s: %v\n", client.DisplayName(), err)
		}
	}
}

// displayUninstallPlan shows what will be uninstalled
func displayUninstallPlan(plan UninstallPlan, out *outputHelper) {
	out.println("\nThe following artifacts will be uninstalled:")

	for _, art := range plan.Artifacts {
		scopeDesc := "global"
		if !art.IsGlobal {
			scopeDesc = "repository"
		}
		clientNames := strings.Join(art.Clients, ", ")
		out.printf("  - %s (%s) v%s [%s] → %s\n", art.Name, art.Type.Label, art.Version, scopeDesc, clientNames)
	}

	out.println()
}

// confirmUninstall prompts user for confirmation
func confirmUninstall(out *outputHelper) bool {
	reader := bufio.NewReader(os.Stdin)
	out.printf("Continue with uninstall? [y/N]: ")

	response, err := reader.ReadString('\n')
	if err != nil {
		return false
	}

	response = strings.ToLower(strings.TrimSpace(response))
	return response == "y" || response == "yes"
}

// uninstallSystemHooks removes system hooks from all installed clients
func uninstallSystemHooks(ctx context.Context, out *outputHelper) {
	out.println("\nRemoving system hooks...")

	registry := clients.Global()
	installedClients := registry.DetectInstalled()

	for _, client := range installedClients {
		if err := client.UninstallHooks(ctx); err != nil {
			out.printfErr("  ✗ Failed to remove hooks from %s: %v\n", client.DisplayName(), err)
		} else {
			out.printf("  ✓ Removed hooks from %s\n", client.DisplayName())
		}
	}
}

// reportResults displays final results to user
func reportResults(results []UninstallResult, out *outputHelper) {
	out.println()

	removedArtifacts := make(map[string]bool)
	failedArtifacts := make(map[string]bool)

	for _, result := range results {
		if result.Success {
			removedArtifacts[result.ArtifactName] = true
		} else {
			failedArtifacts[result.ArtifactName] = true
		}
	}

	// Don't count as failed if also successfully removed
	for name := range removedArtifacts {
		delete(failedArtifacts, name)
	}

	totalRemoved := len(removedArtifacts)
	totalFailed := len(failedArtifacts)

	if totalFailed > 0 {
		out.printf("Uninstalled %d artifact(s) (%d failed)\n", totalRemoved, totalFailed)
	} else {
		out.printf("Successfully uninstalled %d artifact(s)\n", totalRemoved)
	}
}
