package kiro

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/bootstrap"
	"github.com/sleuth-io/sx/internal/clients"
	"github.com/sleuth-io/sx/internal/clients/kiro/handlers"
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/logger"
	"github.com/sleuth-io/sx/internal/metadata"
)

// Client implements the clients.Client interface for Kiro
type Client struct {
	clients.BaseClient
}

// NewClient creates a new Kiro client
func NewClient() *Client {
	return &Client{
		BaseClient: clients.NewBaseClient(
			clients.ClientIDKiro,
			"Kiro",
			[]asset.Type{
				asset.TypeMCP,
				asset.TypeSkill,
				asset.TypeRule,
				asset.TypeCommand,
				asset.TypeHook,
			},
		),
	}
}

// RuleCapabilities returns Kiro's rule capabilities
func (c *Client) RuleCapabilities() *clients.RuleCapabilities {
	return RuleCapabilities()
}

// IsInstalled checks if Kiro is installed.
// We check for actual installation indicators:
// 1. The kiro-cli binary in PATH (most reliable)
// 2. Global ~/.kiro directory (created by Kiro installation)
// Note: We don't check workspace .kiro directories since those are just
// config files that could be in a repo without Kiro being installed.
func (c *Client) IsInstalled() bool {
	// Check if kiro-cli binary is in PATH
	if _, err := exec.LookPath("kiro-cli"); err == nil {
		return true
	}

	// Check for global .kiro directory (user-level config)
	home, err := os.UserHomeDir()
	if err == nil {
		configDir := filepath.Join(home, handlers.ConfigDir)
		if stat, err := os.Stat(configDir); err == nil && stat.IsDir() {
			return true
		}
	}

	return false
}

// GetVersion returns the Kiro CLI version
func (c *Client) GetVersion() string {
	cmd := exec.Command("kiro-cli", "--version")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// InstallAssets installs assets to Kiro using client-specific handlers
func (c *Client) InstallAssets(ctx context.Context, req clients.InstallRequest) (clients.InstallResponse, error) {
	resp := clients.InstallResponse{
		Results: make([]clients.AssetResult, 0, len(req.Assets)),
	}

	// Determine target directory based on scope
	targetBase, err := c.determineTargetBase(req.Scope)
	if err != nil {
		return resp, fmt.Errorf("cannot determine installation directory: %w", err)
	}

	// Ensure target directory exists
	if err := os.MkdirAll(targetBase, 0755); err != nil {
		return resp, fmt.Errorf("failed to create target directory: %w", err)
	}

	// Install each asset using appropriate handler
	for _, bundle := range req.Assets {
		result := clients.AssetResult{
			AssetName: bundle.Asset.Name,
		}

		var err error
		switch bundle.Metadata.Asset.Type {
		case asset.TypeMCP:
			handler := handlers.NewMCPHandler(bundle.Metadata)
			err = handler.Install(ctx, bundle.ZipData, targetBase)
		case asset.TypeSkill:
			handler := handlers.NewSkillHandler(bundle.Metadata)
			err = handler.Install(ctx, bundle.ZipData, targetBase)
		case asset.TypeRule:
			handler := handlers.NewRuleHandler(bundle.Metadata, "")
			err = handler.Install(ctx, bundle.ZipData, targetBase)
		case asset.TypeCommand:
			handler := handlers.NewCommandHandler(bundle.Metadata)
			err = handler.Install(ctx, bundle.ZipData, targetBase)
		case asset.TypeHook:
			handler := handlers.NewHookHandler(bundle.Metadata)
			err = handler.Install(ctx, bundle.ZipData, targetBase)
		default:
			result.Status = clients.StatusSkipped
			result.Message = "Unsupported asset type: " + bundle.Metadata.Asset.Type.Key
			resp.Results = append(resp.Results, result)
			continue
		}

		if err != nil {
			result.Status = clients.StatusFailed
			result.Error = err
			result.Message = fmt.Sprintf("Installation failed: %v", err)
		} else {
			result.Status = clients.StatusSuccess
			result.Message = "Installed to " + targetBase
		}

		resp.Results = append(resp.Results, result)
	}

	return resp, nil
}

// UninstallAssets removes assets from Kiro
func (c *Client) UninstallAssets(ctx context.Context, req clients.UninstallRequest) (clients.UninstallResponse, error) {
	resp := clients.UninstallResponse{
		Results: make([]clients.AssetResult, 0, len(req.Assets)),
	}

	targetBase, err := c.determineTargetBase(req.Scope)
	if err != nil {
		return resp, fmt.Errorf("cannot determine uninstall directory: %w", err)
	}

	for _, a := range req.Assets {
		result := clients.AssetResult{
			AssetName: a.Name,
		}

		// Create minimal metadata for removal
		meta := &metadata.Metadata{
			Asset: metadata.Asset{
				Name: a.Name,
				Type: a.Type,
			},
		}

		var err error
		switch a.Type {
		case asset.TypeMCP:
			handler := handlers.NewMCPHandler(meta)
			err = handler.Remove(ctx, targetBase)
		case asset.TypeSkill:
			handler := handlers.NewSkillHandler(meta)
			err = handler.Remove(ctx, targetBase)
		case asset.TypeRule:
			handler := handlers.NewRuleHandler(meta, "")
			err = handler.Remove(ctx, targetBase)
		case asset.TypeCommand:
			handler := handlers.NewCommandHandler(meta)
			err = handler.Remove(ctx, targetBase)
		case asset.TypeHook:
			handler := handlers.NewHookHandler(meta)
			err = handler.Remove(ctx, targetBase)
		default:
			result.Status = clients.StatusSkipped
			result.Message = "Unsupported asset type: " + a.Type.Key
			resp.Results = append(resp.Results, result)
			continue
		}

		if err != nil {
			result.Status = clients.StatusFailed
			result.Error = err
		} else {
			result.Status = clients.StatusSuccess
			result.Message = "Uninstalled successfully"
		}

		resp.Results = append(resp.Results, result)
	}

	return resp, nil
}

// determineTargetBase returns the installation directory based on scope
// Returns an error if a repo/path-scoped install is requested without a valid RepoRoot
func (c *Client) determineTargetBase(scope *clients.InstallScope) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}

	switch scope.Type {
	case clients.ScopeGlobal:
		return filepath.Join(home, handlers.ConfigDir), nil
	case clients.ScopeRepository:
		if scope.RepoRoot == "" {
			return "", errors.New("repo-scoped install requires RepoRoot but none provided (not in a git repository?)")
		}
		return filepath.Join(scope.RepoRoot, handlers.ConfigDir), nil
	case clients.ScopePath:
		if scope.RepoRoot == "" {
			return "", errors.New("path-scoped install requires RepoRoot but none provided (not in a git repository?)")
		}
		return filepath.Join(scope.RepoRoot, scope.Path, handlers.ConfigDir), nil
	default:
		return filepath.Join(home, handlers.ConfigDir), nil
	}
}

// EnsureAssetSupport ensures asset infrastructure is set up for the current context.
// For Kiro, this registers the sx MCP server (for the query tool) and cleans up
// legacy steering files. Kiro auto-discovers skills from .kiro/skills/ natively.
func (c *Client) EnsureAssetSupport(ctx context.Context, scope *clients.InstallScope) error {
	// Register skills MCP server globally (idempotent)
	if err := c.registerSkillsMCPServer(); err != nil {
		return fmt.Errorf("failed to register MCP server: %w", err)
	}

	// Clean up legacy steering/skills.md if it exists (no longer needed since
	// Kiro auto-discovers skills from .kiro/skills/)
	c.removeLegacySteeringFile(scope)

	return nil
}

// removeLegacySteeringFile removes the old auto-generated steering/skills.md
// that was used before Kiro had native skill discovery.
func (c *Client) removeLegacySteeringFile(scope *clients.InstallScope) {
	log := logger.Get()

	// Check all possible locations where the file could exist
	var targets []string

	if scope.RepoRoot != "" {
		targets = append(targets, filepath.Join(scope.RepoRoot, handlers.ConfigDir))
		if scope.Path != "" {
			targets = append(targets, filepath.Join(scope.RepoRoot, scope.Path, handlers.ConfigDir))
		}
	}

	cwd, err := os.Getwd()
	if err == nil {
		targets = append(targets, filepath.Join(cwd, handlers.ConfigDir))
	}

	for _, target := range targets {
		steeringPath := filepath.Join(target, handlers.DirSteering, "skills.md")
		if _, err := os.Stat(steeringPath); err == nil {
			if err := os.Remove(steeringPath); err != nil {
				log.Debug("failed to remove legacy steering file", "path", steeringPath, "error", err)
			} else {
				log.Info("removed legacy steering/skills.md", "path", steeringPath)
			}
		}
	}
}

// registerSkillsMCPServer adds skills MCP server to ~/.kiro/settings/mcp.json
func (c *Client) registerSkillsMCPServer() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	mcpConfigPath := filepath.Join(home, handlers.ConfigDir, handlers.DirSettings, "mcp.json")

	// Read existing mcp.json
	config, err := handlers.ReadMCPConfig(mcpConfigPath)
	if err != nil {
		return err
	}

	// Only add if missing (don't overwrite existing entry)
	if config.MCPServers == nil {
		config.MCPServers = make(map[string]any)
	}

	if _, exists := config.MCPServers["skills"]; exists {
		// Already configured, don't overwrite
		return nil
	}

	// Get path to sx binary
	sxBinary, err := os.Executable()
	if err != nil {
		return err
	}

	// Add skills MCP server entry
	config.MCPServers["skills"] = map[string]any{
		"command": sxBinary,
		"args":    []string{"serve"},
	}

	return handlers.WriteMCPConfig(mcpConfigPath, config)
}

// ListAssets returns all installed skills for a given scope
func (c *Client) ListAssets(ctx context.Context, scope *clients.InstallScope) ([]clients.InstalledSkill, error) {
	targetBase, err := c.determineTargetBase(scope)
	if err != nil {
		return nil, fmt.Errorf("cannot determine target directory: %w", err)
	}

	installed, err := handlers.SkillOps.ScanInstalled(targetBase)
	if err != nil {
		return nil, fmt.Errorf("failed to scan installed skills: %w", err)
	}

	skills := make([]clients.InstalledSkill, 0, len(installed))
	for _, info := range installed {
		skills = append(skills, clients.InstalledSkill{
			Name:        info.Name,
			Description: info.Description,
			Version:     info.Version,
		})
	}

	return skills, nil
}

// ReadSkill reads the content of a specific skill by name
func (c *Client) ReadSkill(ctx context.Context, name string, scope *clients.InstallScope) (*clients.SkillContent, error) {
	targetBase, err := c.determineTargetBase(scope)
	if err != nil {
		return nil, fmt.Errorf("cannot determine target directory: %w", err)
	}

	result, err := handlers.SkillOps.ReadPromptContent(targetBase, name, "SKILL.md", func(m *metadata.Metadata) string { return m.Skill.PromptFile })
	if err != nil {
		return nil, err
	}

	return &clients.SkillContent{
		Name:        name,
		Description: result.Description,
		Version:     result.Version,
		Content:     result.Content,
		BaseDir:     result.BaseDir,
	}, nil
}

// GetBootstrapOptions returns bootstrap options for Kiro.
func (c *Client) GetBootstrapOptions(ctx context.Context) []bootstrap.Option {
	return []bootstrap.Option{
		bootstrap.SessionHook,
		bootstrap.AnalyticsHook,
		bootstrap.SleuthAIQueryMCP(),
	}
}

// GetBootstrapPath returns the path to Kiro's hooks directory.
// For Kiro, this is workspace-level: .kiro/hooks/
func (c *Client) GetBootstrapPath() string {
	repoRoot := findGitRoot()
	if repoRoot == "" {
		return ".kiro/hooks/ (per repo)"
	}
	return filepath.Join(repoRoot, handlers.ConfigDir, handlers.DirHooks)
}

// InstallBootstrap installs Kiro infrastructure (hooks and MCP servers).
func (c *Client) InstallBootstrap(ctx context.Context, opts []bootstrap.Option) error {
	// Install hooks to workspace .kiro/hooks/
	installHooks := bootstrap.ContainsKey(opts, bootstrap.SessionHookKey) ||
		bootstrap.ContainsKey(opts, bootstrap.AnalyticsHookKey)

	if installHooks {
		repoRoot := findGitRoot()
		if repoRoot == "" {
			log := logger.Get()
			log.Warn("cannot install Kiro hooks: not in a git repository")
		} else {
			if err := installKiroHooks(repoRoot, opts); err != nil {
				return err
			}
		}
	}

	// Install MCP servers from options that have MCPConfig
	for _, opt := range opts {
		if opt.MCPConfig != nil {
			if err := c.installMCPServerFromConfig(opt.MCPConfig); err != nil {
				return fmt.Errorf("failed to install MCP server %s: %w", opt.MCPConfig.Name, err)
			}
		}
	}

	return nil
}

// installMCPServerFromConfig installs an MCP server from a bootstrap.MCPServerConfig
func (c *Client) installMCPServerFromConfig(config *bootstrap.MCPServerConfig) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}
	kiroDir := filepath.Join(home, handlers.ConfigDir)
	log := logger.Get()

	serverConfig := map[string]any{
		"command": config.Command,
		"args":    config.Args,
	}

	// Add env if present
	if len(config.Env) > 0 {
		serverConfig["env"] = config.Env
	}

	if err := handlers.AddMCPServer(kiroDir, config.Name, serverConfig); err != nil {
		return err
	}

	log.Info("MCP server installed", "server", config.Name, "command", config.Command)
	return nil
}

// UninstallBootstrap removes Kiro infrastructure (hooks and MCP servers).
func (c *Client) UninstallBootstrap(ctx context.Context, opts []bootstrap.Option) error {
	// Remove hooks if requested
	uninstallSession := bootstrap.ContainsKey(opts, bootstrap.SessionHookKey)
	uninstallAnalytics := bootstrap.ContainsKey(opts, bootstrap.AnalyticsHookKey)

	if uninstallSession || uninstallAnalytics {
		repoRoot := findGitRoot()
		if repoRoot != "" {
			if err := uninstallKiroHooks(repoRoot, uninstallSession, uninstallAnalytics); err != nil {
				log := logger.Get()
				log.Error("failed to uninstall hooks", "error", err)
			}
		}
	}

	// Remove MCP servers
	for _, opt := range opts {
		if opt.MCPConfig != nil {
			if err := c.uninstallMCPServerByName(opt.MCPConfig.Name); err != nil {
				return err
			}
		}
	}
	return nil
}

// uninstallMCPServerByName removes an MCP server by its name
func (c *Client) uninstallMCPServerByName(name string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}
	kiroDir := filepath.Join(home, handlers.ConfigDir)
	log := logger.Get()

	if err := handlers.RemoveMCPServer(kiroDir, name); err != nil {
		return err
	}

	log.Info("MCP server uninstalled", "server", name)
	return nil
}

// ShouldInstall always returns true for Kiro.
// Kiro has a sessionStart hook that fires once per session, so no
// deduplication is needed.
func (c *Client) ShouldInstall(ctx context.Context) (bool, error) {
	return true, nil
}

// VerifyAssets checks if assets are actually installed on the filesystem
func (c *Client) VerifyAssets(ctx context.Context, assets []*lockfile.Asset, scope *clients.InstallScope) []clients.VerifyResult {
	results := make([]clients.VerifyResult, 0, len(assets))

	targetBase, err := c.determineTargetBase(scope)
	if err != nil {
		// Can't determine target - mark all assets as not installed
		for _, a := range assets {
			results = append(results, clients.VerifyResult{
				Asset:     a,
				Installed: false,
				Message:   fmt.Sprintf("cannot determine target directory: %v", err),
			})
		}
		return results
	}

	for _, a := range assets {
		result := clients.VerifyResult{
			Asset: a,
		}

		handler, err := handlers.NewHandler(a.Type, &metadata.Metadata{
			Asset: metadata.Asset{
				Name:    a.Name,
				Version: a.Version,
				Type:    a.Type,
			},
		})
		if err != nil {
			result.Message = err.Error()
		} else {
			result.Installed, result.Message = handler.VerifyInstalled(targetBase)
		}

		results = append(results, result)
	}

	return results
}

// ScanInstalledAssets returns an empty list for Kiro (not yet supported)
func (c *Client) ScanInstalledAssets(ctx context.Context, scope *clients.InstallScope) ([]clients.InstalledAsset, error) {
	// Kiro asset import not yet supported
	return []clients.InstalledAsset{}, nil
}

// GetAssetPath returns an error for Kiro (not yet supported)
func (c *Client) GetAssetPath(ctx context.Context, name string, assetType asset.Type, scope *clients.InstallScope) (string, error) {
	return "", errors.New("asset import not supported for Kiro")
}

func init() {
	// Auto-register on package import
	clients.Register(NewClient())
}
