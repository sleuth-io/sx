package gemini

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
	"github.com/sleuth-io/sx/internal/clients/gemini/handlers"
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/metadata"
)

// Client implements the clients.Client interface for Gemini Code Assist
type Client struct {
	clients.BaseClient
}

// NewClient creates a new Gemini client
func NewClient() *Client {
	return &Client{
		BaseClient: clients.NewBaseClient(
			clients.ClientIDGemini,
			"Gemini Code Assist",
			[]asset.Type{
				asset.TypeMCP,     // settings.json mcpServers
				asset.TypeRule,    // GEMINI.md files
				asset.TypeSkill,   // .gemini/commands/*.toml (Gemini CLI only)
				asset.TypeCommand, // .gemini/commands/*.toml (same as skill)
				// Note: Hooks are managed via bootstrap (InstallBootstrap/UninstallBootstrap),
				// not as standalone assets. See installSessionHook and installAnalyticsHook.
			},
		),
	}
}

// RuleCapabilities returns Gemini's rule capabilities
func (c *Client) RuleCapabilities() *clients.RuleCapabilities {
	return RuleCapabilities()
}

// IsInstalled checks if Gemini is installed by checking for ~/.gemini/ directory
func (c *Client) IsInstalled() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}

	// Check for .gemini directory
	configDir := filepath.Join(home, handlers.ConfigDir)
	if stat, err := os.Stat(configDir); err == nil {
		return stat.IsDir()
	}

	return false
}

// GetVersion returns the Gemini version
func (c *Client) GetVersion() string {
	cmd := exec.Command(handlers.CLICommand, "--version")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// InstallAssets installs assets to Gemini using client-specific handlers
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
		var installedPath string
		switch bundle.Metadata.Asset.Type {
		case asset.TypeMCP:
			handler := handlers.NewMCPHandler(bundle.Metadata)
			err = handler.Install(ctx, bundle.ZipData, targetBase)
			installedPath = filepath.Join(targetBase, handlers.SettingsFile)
		case asset.TypeRule:
			handler := handlers.NewRuleHandler(bundle.Metadata)
			err = handler.Install(ctx, bundle.ZipData, targetBase)
			installedPath = filepath.Join(targetBase, handlers.GeminiRuleFile)
		case asset.TypeSkill, asset.TypeCommand:
			handler := handlers.NewSkillHandler(bundle.Metadata)
			err = handler.Install(ctx, bundle.ZipData, targetBase)
			// For global scope, targetBase is already ~/.gemini, so we only add commands/
			// For repo scope, targetBase is /repo, so we add .gemini/commands/
			if filepath.Base(targetBase) == handlers.ConfigDir {
				installedPath = filepath.Join(targetBase, handlers.DirCommands, bundle.Asset.Name+".toml")
			} else {
				installedPath = filepath.Join(targetBase, handlers.ConfigDir, handlers.DirCommands, bundle.Asset.Name+".toml")
			}
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
			result.Message = installedPath
		}

		resp.Results = append(resp.Results, result)
	}

	return resp, nil
}

// UninstallAssets removes assets from Gemini
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
		case asset.TypeRule:
			handler := handlers.NewRuleHandler(meta)
			err = handler.Remove(ctx, targetBase)
		case asset.TypeSkill, asset.TypeCommand:
			handler := handlers.NewSkillHandler(meta)
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
// For Gemini, rules are in GEMINI.md files relative to scope,
// and MCP config is always in ~/.gemini/settings.json
func (c *Client) determineTargetBase(scope *clients.InstallScope) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	switch scope.Type {
	case clients.ScopeGlobal:
		return filepath.Join(home, handlers.ConfigDir), nil
	case clients.ScopeRepository:
		if scope.RepoRoot == "" {
			return "", errors.New("repo-scoped install requires RepoRoot but none provided (not in a git repository?)")
		}
		return scope.RepoRoot, nil
	case clients.ScopePath:
		if scope.RepoRoot == "" {
			return "", errors.New("path-scoped install requires RepoRoot but none provided (not in a git repository?)")
		}
		return filepath.Join(scope.RepoRoot, scope.Path), nil
	default:
		return filepath.Join(home, handlers.ConfigDir), nil
	}
}

// EnsureAssetSupport ensures asset infrastructure is set up for the current context.
// For Gemini, no additional setup is needed.
func (c *Client) EnsureAssetSupport(ctx context.Context, scope *clients.InstallScope) error {
	// Gemini doesn't need additional setup - rules are read directly from GEMINI.md
	return nil
}

// ListAssets returns all installed skills for a given scope.
// Gemini doesn't support skills, so this returns an empty list.
func (c *Client) ListAssets(ctx context.Context, scope *clients.InstallScope) ([]clients.InstalledSkill, error) {
	// Gemini doesn't support skills
	return []clients.InstalledSkill{}, nil
}

// ReadSkill reads the content of a specific skill by name.
// Gemini doesn't support skills.
func (c *Client) ReadSkill(ctx context.Context, name string, scope *clients.InstallScope) (*clients.SkillContent, error) {
	return nil, errors.New("skills not supported for Gemini")
}

// GetBootstrapOptions returns bootstrap options for Gemini.
// Includes session hook, analytics hook, and MCP servers.
func (c *Client) GetBootstrapOptions(ctx context.Context) []bootstrap.Option {
	return []bootstrap.Option{
		bootstrap.SessionHook,
		bootstrap.AnalyticsHook,
		bootstrap.SleuthAIQueryMCP(),
	}
}

// GetBootstrapPath returns the path to Gemini's settings file.
func (c *Client) GetBootstrapPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, handlers.ConfigDir, handlers.SettingsFile)
}

// InstallBootstrap installs Gemini infrastructure (hooks and MCP servers).
func (c *Client) InstallBootstrap(ctx context.Context, opts []bootstrap.Option) error {
	// Install session hook
	if bootstrap.ContainsKey(opts, bootstrap.SessionHookKey) {
		if err := c.installSessionHook(); err != nil {
			return err
		}
	}

	// Install analytics hook
	if bootstrap.ContainsKey(opts, bootstrap.AnalyticsHookKey) {
		if err := c.installAnalyticsHook(); err != nil {
			return err
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

// installSessionHook installs the SessionStart hook for auto-install
func (c *Client) installSessionHook() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}
	geminiDir := filepath.Join(home, handlers.ConfigDir)

	hookCommand := "sx install --hook-mode --client=gemini"
	return handlers.AddHook(geminiDir, "SessionStart", "sx-session", hookCommand)
}

// installAnalyticsHook installs the AfterTool hook for usage tracking
func (c *Client) installAnalyticsHook() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}
	geminiDir := filepath.Join(home, handlers.ConfigDir)

	hookCommand := "sx report-usage --client=gemini"
	return handlers.AddHook(geminiDir, "AfterTool", "sx-analytics", hookCommand)
}

// installMCPServerFromConfig installs an MCP server from a bootstrap.MCPServerConfig
func (c *Client) installMCPServerFromConfig(config *bootstrap.MCPServerConfig) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}
	geminiDir := filepath.Join(home, handlers.ConfigDir)

	serverConfig := map[string]any{
		"command": config.Command,
		"args":    config.Args,
	}

	// Add env if present
	if len(config.Env) > 0 {
		serverConfig["env"] = config.Env
	}

	return handlers.AddMCPServer(geminiDir, config.Name, serverConfig)
}

// UninstallBootstrap removes Gemini infrastructure (hooks and MCP servers).
func (c *Client) UninstallBootstrap(ctx context.Context, opts []bootstrap.Option) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}
	geminiDir := filepath.Join(home, handlers.ConfigDir)

	for _, opt := range opts {
		switch opt.Key {
		case bootstrap.SessionHookKey:
			if err := handlers.RemoveHook(geminiDir, "SessionStart", "sx-session"); err != nil {
				return err
			}
		case bootstrap.AnalyticsHookKey:
			if err := handlers.RemoveHook(geminiDir, "AfterTool", "sx-analytics"); err != nil {
				return err
			}
		default:
			// Handle MCP options
			if opt.MCPConfig != nil {
				if err := c.uninstallMCPServerByName(opt.MCPConfig.Name); err != nil {
					return err
				}
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
	geminiDir := filepath.Join(home, handlers.ConfigDir)

	return handlers.RemoveMCPServer(geminiDir, name)
}

// ShouldInstall checks if installation should proceed.
// Gemini doesn't have hooks that fire repeatedly, so always proceed.
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

		switch a.Type {
		case asset.TypeMCP:
			handler := handlers.NewMCPHandler(&metadata.Metadata{
				Asset: metadata.Asset{
					Name:    a.Name,
					Version: a.Version,
					Type:    a.Type,
				},
			})
			result.Installed, result.Message = handler.VerifyInstalled(targetBase)
		case asset.TypeRule:
			handler := handlers.NewRuleHandler(&metadata.Metadata{
				Asset: metadata.Asset{
					Name:    a.Name,
					Version: a.Version,
					Type:    a.Type,
				},
			})
			result.Installed, result.Message = handler.VerifyInstalled(targetBase)
		case asset.TypeSkill, asset.TypeCommand:
			handler := handlers.NewSkillHandler(&metadata.Metadata{
				Asset: metadata.Asset{
					Name:    a.Name,
					Version: a.Version,
					Type:    a.Type,
				},
			})
			result.Installed, result.Message = handler.VerifyInstalled(targetBase)
		default:
			result.Message = "unsupported asset type"
		}

		results = append(results, result)
	}

	return results
}

// ScanInstalledAssets returns an empty list for Gemini (not yet supported)
func (c *Client) ScanInstalledAssets(ctx context.Context, scope *clients.InstallScope) ([]clients.InstalledAsset, error) {
	// Gemini asset import not yet supported
	return []clients.InstalledAsset{}, nil
}

// GetAssetPath returns an error for Gemini (not yet supported)
func (c *Client) GetAssetPath(ctx context.Context, name string, assetType asset.Type, scope *clients.InstallScope) (string, error) {
	return "", errors.New("asset import not supported for Gemini")
}

func init() {
	// Auto-register on package import
	clients.Register(NewClient())
}
