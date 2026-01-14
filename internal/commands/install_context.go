package commands

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

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

// detectInstallEnvironment detects git context, builds scope, and finds target clients
func detectInstallEnvironment(ctx context.Context, cfg *config.Config, status *components.Status) (*installEnvironment, error) {
	status.Start("Detecting context")

	gitCtx, err := gitutil.DetectContext(ctx)
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

// detectTargetClients finds and filters clients based on config
func detectTargetClients(cfg *config.Config, status *components.Status) ([]clients.Client, error) {
	registry := clients.Global()
	detectedClients := registry.DetectInstalled()
	targetClients := filterClientsByConfig(cfg, detectedClients)

	if len(targetClients) == 0 {
		if len(detectedClients) > 0 {
			status.Fail("No enabled AI coding clients available")
			return nil, fmt.Errorf("no enabled AI coding clients available (detected: %d, enabled in config: %v)",
				len(detectedClients), cfg.EnabledClients)
		}
		status.Fail("No AI coding clients detected")
		return nil, errors.New("no AI coding clients detected")
	}

	return targetClients, nil
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
	installClientHooks(ctx, env.Clients, out)

	// Ensure asset support is configured for all clients
	ensureAssetSupport(ctx, env.Clients, buildInstallScope(env.CurrentScope, env.GitContext), out)

	// Log summary
	log := logger.Get()
	log.Info("install completed", "installed", 0, "total_up_to_date", len(sortedAssets))

	// In hook mode, output JSON even when nothing changed
	if hookMode {
		outputHookModeJSON(out, map[string]any{"continue": true})
	} else {
		styledOut.Success("All assets up to date")
	}

	return nil
}
