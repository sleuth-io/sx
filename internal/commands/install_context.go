package commands

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/sleuth-io/sx/internal/assets"
	"github.com/sleuth-io/sx/internal/clients"
	"github.com/sleuth-io/sx/internal/clients/cursor"
	"github.com/sleuth-io/sx/internal/config"
	"github.com/sleuth-io/sx/internal/gitutil"
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/logger"
	"github.com/sleuth-io/sx/internal/scope"
	"github.com/sleuth-io/sx/internal/ui"
	"github.com/sleuth-io/sx/internal/ui/components"
	vaultpkg "github.com/sleuth-io/sx/internal/vault"
)

// installEnvironment holds the detected environment for installation
type installEnvironment struct {
	GitContext   *gitutil.GitContext
	CurrentScope *scope.Scope
	Clients      []clients.Client
}

// detectInstallEnvironment detects git context, builds scope, and finds target clients.
// If targetDir is non-empty, it is used instead of the current working directory.
func detectInstallEnvironment(ctx context.Context, cfg *config.Config, status *components.Status, targetDir string) (*installEnvironment, error) {
	status.Start("Detecting context")

	var gitCtx *gitutil.GitContext
	var err error
	if targetDir != "" {
		absTarget, absErr := filepath.Abs(targetDir)
		if absErr != nil {
			status.Fail("Failed to resolve target directory")
			return nil, fmt.Errorf("failed to resolve target directory %q: %w", targetDir, absErr)
		}
		if info, statErr := os.Stat(absTarget); statErr != nil {
			status.Fail("Target directory does not exist")
			return nil, fmt.Errorf("target directory does not exist: %s", absTarget)
		} else if !info.IsDir() {
			status.Fail("Target is not a directory")
			return nil, fmt.Errorf("target is not a directory: %s", absTarget)
		}
		gitCtx, err = gitutil.DetectContextForPath(ctx, absTarget)
	} else {
		gitCtx, err = gitutil.DetectContext(ctx)
	}
	if err != nil {
		status.Fail("Failed to detect git context")
		return nil, fmt.Errorf("failed to detect git context: %w", err)
	}

	currentScope := buildScopeFromGitContext(gitCtx)
	targetClients, err := detectTargetClients(cfg, status)
	if err != nil {
		return nil, err
	}

	status.Clear()
	return &installEnvironment{
		GitContext:   gitCtx,
		CurrentScope: currentScope,
		Clients:      targetClients,
	}, nil
}

// buildScopeFromGitContext creates a scope based on the git context
func buildScopeFromGitContext(gitCtx *gitutil.GitContext) *scope.Scope {
	if !gitCtx.IsRepo {
		return &scope.Scope{Type: scope.TypeGlobal}
	}

	if gitCtx.RelativePath == "." {
		return &scope.Scope{
			Type:     scope.TypeRepo,
			RepoURL:  gitCtx.RepoURL,
			RepoPath: "",
		}
	}

	return &scope.Scope{
		Type:     scope.TypePath,
		RepoURL:  gitCtx.RepoURL,
		RepoPath: gitCtx.RelativePath,
	}
}

// detectTargetClients finds and filters clients based on config.
// It includes both detected clients and force-enabled clients (even if not detected).
func detectTargetClients(cfg *config.Config, status *components.Status) ([]clients.Client, error) {
	registry := clients.Global()
	detectedClients := registry.DetectInstalled()
	targetClients := filterClientsByConfig(cfg, detectedClients)

	// Also include force-enabled clients that weren't detected.
	// This allows users to explicitly enable clients like GitHub Copilot
	// even if we can't detect them (e.g., VS Code extension only).
	targetClients = addForceEnabledClients(cfg, registry, targetClients)

	if len(targetClients) == 0 {
		if len(detectedClients) > 0 {
			status.Fail("No enabled AI coding clients available")
			return nil, fmt.Errorf("no enabled AI coding clients available (detected: %d, disabled in config: %v)",
				len(detectedClients), cfg.ForceDisabledClients)
		}
		status.Fail("No AI coding clients detected")
		return nil, errors.New("no AI coding clients detected")
	}

	return targetClients, nil
}

// addForceEnabledClients adds force-enabled clients that weren't already in the list.
// This allows users to target clients that aren't detected automatically.
func addForceEnabledClients(cfg *config.Config, registry *clients.Registry, existing []clients.Client) []clients.Client {
	// Build set of existing client IDs
	existingIDs := make(map[string]bool)
	for _, c := range existing {
		existingIDs[c.ID()] = true
	}

	// Add any force-enabled clients not already present
	for _, id := range cfg.ForceEnabledClients {
		if existingIDs[id] {
			continue // Already in the list
		}
		if client, err := registry.Get(id); err == nil {
			existing = append(existing, client)
		}
	}

	return existing
}

// filterAssetsByScope filters assets to those applicable to the current context
func filterAssetsByScope(lf *lockfile.LockFile, targetClients []clients.Client, matcherScope *scope.Matcher) []*lockfile.Asset {
	var applicableAssets []*lockfile.Asset
	for i := range lf.Assets {
		asset := &lf.Assets[i]
		if isAssetApplicable(asset, targetClients, matcherScope) {
			applicableAssets = append(applicableAssets, asset)
		}
	}
	return applicableAssets
}

// isAssetApplicable checks if an asset is supported by any target client and matches scope
func isAssetApplicable(asset *lockfile.Asset, targetClients []clients.Client, matcherScope *scope.Matcher) bool {
	for _, client := range targetClients {
		if asset.MatchesClient(client.ID()) &&
			client.SupportsAssetType(asset.Type) &&
			matcherScope.MatchesAsset(asset) {
			return true
		}
	}
	return false
}

// getTargetClientIDs extracts client IDs from a slice of clients
func getTargetClientIDs(targetClients []clients.Client) []string {
	ids := make([]string, len(targetClients))
	for i, client := range targetClients {
		ids[i] = client.ID()
	}
	return ids
}

// filterClientsByID returns only the client matching the given ID.
// Returns an empty slice if no client matches.
func filterClientsByID(allClients []clients.Client, clientID string) []clients.Client {
	for _, c := range allClients {
		if c.ID() == clientID {
			return []clients.Client{c}
		}
	}
	return nil
}

// filterClientsByFlag filters clients by a comma-separated list of client IDs.
// Returns only clients whose ID is in the list.
func filterClientsByFlag(allClients []clients.Client, clientsFlag string) []clients.Client {
	// Parse comma-separated list
	wantedIDs := make(map[string]bool)
	for _, id := range strings.Split(clientsFlag, ",") {
		id = strings.TrimSpace(id)
		if id != "" {
			wantedIDs[id] = true
		}
	}

	var filtered []clients.Client
	for _, c := range allClients {
		if wantedIDs[c.ID()] {
			filtered = append(filtered, c)
		}
	}
	return filtered
}

// handleCursorWorkspace handles changing to Cursor workspace directory in hook mode
func handleCursorWorkspace(hookMode bool, hookClientID string, log *slog.Logger) {
	if !hookMode || hookClientID != "cursor" {
		return
	}

	if workspaceDir := cursor.ParseWorkspaceDir(); workspaceDir != "" {
		if err := os.Chdir(workspaceDir); err != nil {
			log.Warn("failed to chdir to workspace", "workspace", workspaceDir, "error", err)
		} else {
			log.Debug("changed to workspace directory", "workspace", workspaceDir)
		}
	}
}

// loadConfigAndVault loads configuration and creates vault instance
func loadConfigAndVault() (*config.Config, vaultpkg.Vault, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load configuration: %w\nRun 'sx init' to configure", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, nil, fmt.Errorf("invalid configuration: %w", err)
	}

	vault, err := vaultpkg.NewFromConfig(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create vault: %w", err)
	}

	return cfg, vault, nil
}

// resolveAssetDependencies resolves dependencies for applicable assets
func resolveAssetDependencies(lf *lockfile.LockFile, applicableAssets []*lockfile.Asset) ([]*lockfile.Asset, error) {
	if len(applicableAssets) == 0 {
		return nil, nil
	}

	resolver := assets.NewDependencyResolver(lf)
	sortedAssets, err := resolver.Resolve(applicableAssets)
	if err != nil {
		return nil, fmt.Errorf("dependency resolution failed: %w", err)
	}
	return sortedAssets, nil
}

// handleNothingToInstall handles the case when no assets need to be installed
func handleNothingToInstall(
	ctx context.Context,
	hookMode bool,
	hookClientID string,
	tracker *assets.Tracker,
	sortedAssets []*lockfile.Asset,
	env *installEnvironment,
	targetClientIDs []string,
	styledOut *ui.Output,
	out *outputHelper,
) error {
	// Save state even if nothing changed (nil downloads since nothing was downloaded)
	saveInstallationState(tracker, sortedAssets, nil, env.CurrentScope, targetClientIDs, out)

	// Install client-specific hooks
	// In hook mode, only install hooks for the triggering client
	hookClients := env.Clients
	if hookMode && hookClientID != "" {
		hookClients = filterClientsByID(env.Clients, hookClientID)
	}
	installClientHooks(ctx, hookClients, out)

	// Ensure asset support is configured for all clients
	ensureAssetSupport(ctx, env.Clients, buildInstallScope(env.CurrentScope, env.GitContext), out)

	// Log summary
	log := logger.Get()
	log.Info("install completed", "installed", 0, "total_up_to_date", len(sortedAssets))

	// In hook mode, output JSON even when nothing changed
	if hookMode {
		outputHookModeJSON(out, map[string]any{"continue": true})
	} else {
		if len(sortedAssets) == 0 {
			styledOut.Success("No assets to install")
		} else if len(sortedAssets) == 1 {
			styledOut.Success(fmt.Sprintf("%s is up to date", sortedAssets[0].Name))
		} else {
			styledOut.Success(fmt.Sprintf("All %d assets up to date", len(sortedAssets)))
		}
	}

	return nil
}
