package codex

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/bootstrap"
	"github.com/sleuth-io/sx/internal/clients"
	"github.com/sleuth-io/sx/internal/clients/codex/handlers"
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/metadata"
)

// Client implements the clients.Client interface for Codex
type Client struct {
	clients.BaseClient
}

// NewClient creates a new Codex client
func NewClient() *Client {
	return &Client{
		BaseClient: clients.NewBaseClient(
			clients.ClientIDCodex,
			"Codex",
			[]asset.Type{
				asset.TypeSkill,
				asset.TypeCommand,
				asset.TypeMCP,
			},
		),
	}
}

// IsInstalled checks if Codex is installed by checking for ~/.codex directory
func (c *Client) IsInstalled() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}

	configDir := filepath.Join(home, ".codex")
	if stat, err := os.Stat(configDir); err == nil {
		return stat.IsDir()
	}
	return false
}

// GetVersion returns the Codex version
func (c *Client) GetVersion() string {
	cmd := exec.Command("codex", "--version")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(output)
}

// InstallAssets installs assets to Codex using client-specific handlers
func (c *Client) InstallAssets(ctx context.Context, req clients.InstallRequest) (clients.InstallResponse, error) {
	resp := clients.InstallResponse{
		Results: make([]clients.AssetResult, 0, len(req.Assets)),
	}

	for _, bundle := range req.Assets {
		result := clients.AssetResult{
			AssetName: bundle.Asset.Name,
		}

		// Determine target directory based on scope and asset type
		targetBase, err := c.determineTargetBase(req.Scope, bundle.Metadata.Asset.Type)
		if err != nil {
			result.Status = clients.StatusFailed
			result.Error = err
			result.Message = fmt.Sprintf("Cannot determine installation directory: %v", err)
			resp.Results = append(resp.Results, result)
			continue
		}

		// Ensure target directory exists
		if err := os.MkdirAll(targetBase, 0755); err != nil {
			result.Status = clients.StatusFailed
			result.Error = err
			result.Message = fmt.Sprintf("Failed to create target directory: %v", err)
			resp.Results = append(resp.Results, result)
			continue
		}

		var installErr error
		switch bundle.Metadata.Asset.Type {
		case asset.TypeSkill:
			handler := handlers.NewSkillHandler(bundle.Metadata)
			installErr = handler.Install(ctx, bundle.ZipData, targetBase)
		case asset.TypeCommand:
			handler := handlers.NewCommandHandler(bundle.Metadata)
			installErr = handler.Install(ctx, bundle.ZipData, targetBase)
		case asset.TypeMCP:
			handler := handlers.NewMCPHandler(bundle.Metadata)
			installErr = handler.Install(ctx, bundle.ZipData, targetBase)
		default:
			result.Status = clients.StatusSkipped
			result.Message = "Unsupported asset type: " + bundle.Metadata.Asset.Type.Key
			resp.Results = append(resp.Results, result)
			continue
		}

		if installErr != nil {
			result.Status = clients.StatusFailed
			result.Error = installErr
			result.Message = fmt.Sprintf("Installation failed: %v", installErr)
		} else {
			result.Status = clients.StatusSuccess
			result.Message = "Installed to " + targetBase
		}

		resp.Results = append(resp.Results, result)
	}

	return resp, nil
}

// UninstallAssets removes assets from Codex
func (c *Client) UninstallAssets(ctx context.Context, req clients.UninstallRequest) (clients.UninstallResponse, error) {
	resp := clients.UninstallResponse{
		Results: make([]clients.AssetResult, 0, len(req.Assets)),
	}

	for _, a := range req.Assets {
		result := clients.AssetResult{
			AssetName: a.Name,
		}

		targetBase, err := c.determineTargetBase(req.Scope, a.Type)
		if err != nil {
			result.Status = clients.StatusFailed
			result.Error = err
			resp.Results = append(resp.Results, result)
			continue
		}

		meta := &metadata.Metadata{
			Asset: metadata.Asset{
				Name: a.Name,
				Type: a.Type,
			},
		}

		var uninstallErr error
		switch a.Type {
		case asset.TypeSkill:
			handler := handlers.NewSkillHandler(meta)
			uninstallErr = handler.Remove(ctx, targetBase)
		case asset.TypeCommand:
			handler := handlers.NewCommandHandler(meta)
			uninstallErr = handler.Remove(ctx, targetBase)
		case asset.TypeMCP:
			handler := handlers.NewMCPHandler(meta)
			uninstallErr = handler.Remove(ctx, targetBase)
		default:
			result.Status = clients.StatusSkipped
			result.Message = "Unsupported asset type: " + a.Type.Key
			resp.Results = append(resp.Results, result)
			continue
		}

		if uninstallErr != nil {
			result.Status = clients.StatusFailed
			result.Error = uninstallErr
		} else {
			result.Status = clients.StatusSuccess
			result.Message = "Uninstalled successfully"
		}

		resp.Results = append(resp.Results, result)
	}

	return resp, nil
}

// determineTargetBase returns the installation directory based on scope and asset type.
// For skills in repo/path scope, uses .agents/ (Codex convention).
// For other assets or global scope, uses .codex/.
func (c *Client) determineTargetBase(scope *clients.InstallScope, assetType asset.Type) (string, error) {
	home, _ := os.UserHomeDir()

	switch scope.Type {
	case clients.ScopeGlobal:
		// Global scope always uses ~/.codex/
		return filepath.Join(home, ".codex"), nil

	case clients.ScopeRepository:
		if scope.RepoRoot == "" {
			return "", errors.New("repo-scoped install requires RepoRoot but none provided (not in a git repository?)")
		}
		// For skills, use .agents/ (Codex convention for repo skills)
		// For other assets, use .codex/
		if assetType == asset.TypeSkill {
			return filepath.Join(scope.RepoRoot, ".agents"), nil
		}
		return filepath.Join(scope.RepoRoot, ".codex"), nil

	case clients.ScopePath:
		if scope.RepoRoot == "" {
			return "", errors.New("path-scoped install requires RepoRoot but none provided (not in a git repository?)")
		}
		// For skills, use .agents/ (Codex convention)
		if assetType == asset.TypeSkill {
			return filepath.Join(scope.RepoRoot, scope.Path, ".agents"), nil
		}
		return filepath.Join(scope.RepoRoot, scope.Path, ".codex"), nil

	default:
		return filepath.Join(home, ".codex"), nil
	}
}

// ListAssets returns all installed skills for a given scope
func (c *Client) ListAssets(ctx context.Context, scope *clients.InstallScope) ([]clients.InstalledSkill, error) {
	targetBase, err := c.determineTargetBase(scope, asset.TypeSkill)
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
	targetBase, err := c.determineTargetBase(scope, asset.TypeSkill)
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

// EnsureAssetSupport is a no-op for Codex since it loads skills natively.
func (c *Client) EnsureAssetSupport(ctx context.Context, scope *clients.InstallScope) error {
	return nil
}

// GetBootstrapOptions returns bootstrap options for Codex.
// Note: Codex doesn't have a session-start hook, only agent-turn-complete via notify.
func (c *Client) GetBootstrapOptions(ctx context.Context) []bootstrap.Option {
	return []bootstrap.Option{
		// No session hook - Codex only has notify for agent-turn-complete
		bootstrap.AnalyticsHook,
		bootstrap.SleuthAIQueryMCP(),
	}
}

// GetBootstrapPath returns the path to Codex's config file.
func (c *Client) GetBootstrapPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".codex", "config.toml")
}

// InstallBootstrap installs Codex infrastructure (notify hooks and MCP servers)
func (c *Client) InstallBootstrap(ctx context.Context, opts []bootstrap.Option) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}
	codexDir := filepath.Join(home, ".codex")

	// Install analytics hook via notify (if enabled)
	if bootstrap.ContainsKey(opts, bootstrap.AnalyticsHookKey) {
		if err := c.installNotifyHook(codexDir); err != nil {
			return err
		}
	}

	// Install MCP servers from options that have MCPConfig
	for _, opt := range opts {
		if opt.MCPConfig != nil {
			if err := c.installMCPServerFromConfig(codexDir, opt.MCPConfig); err != nil {
				return fmt.Errorf("failed to install MCP server %s: %w", opt.MCPConfig.Name, err)
			}
		}
	}

	return nil
}

// installNotifyHook installs the notify hook for analytics
func (c *Client) installNotifyHook(codexDir string) error {
	configPath := filepath.Join(codexDir, "config.toml")

	config, raw, err := handlers.ReadCodexConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config.toml: %w", err)
	}

	// Add notify command for analytics
	notifyCmd := []string{"sx", "report-usage", "--client=codex"}

	// Check if already set with exact match
	if existing, ok := raw["notify"].([]any); ok && len(existing) == len(notifyCmd) {
		match := true
		for i, item := range existing {
			if s, ok := item.(string); !ok || s != notifyCmd[i] {
				match = false
				break
			}
		}
		if match {
			return nil // Already installed with exact command
		}
	}

	// Set notify command
	raw["notify"] = notifyCmd

	return handlers.WriteCodexConfig(configPath, config, raw)
}

// installMCPServerFromConfig installs an MCP server from a bootstrap.MCPServerConfig
func (c *Client) installMCPServerFromConfig(codexDir string, config *bootstrap.MCPServerConfig) error {
	configPath := filepath.Join(codexDir, "config.toml")

	entry := handlers.MCPServerEntry{
		Name:      config.Name,
		Transport: "stdio",
		Command:   config.Command,
		Args:      config.Args,
		Env:       config.Env,
	}

	return handlers.AddMCPServer(configPath, config.Name, entry)
}

// UninstallBootstrap removes Codex infrastructure
func (c *Client) UninstallBootstrap(ctx context.Context, opts []bootstrap.Option) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}
	codexDir := filepath.Join(home, ".codex")

	for _, opt := range opts {
		switch opt.Key {
		case bootstrap.AnalyticsHookKey:
			if err := c.uninstallNotifyHook(codexDir); err != nil {
				return err
			}
		default:
			// Handle MCP options generically
			if opt.MCPConfig != nil {
				configPath := filepath.Join(codexDir, "config.toml")
				if err := handlers.RemoveMCPServer(configPath, opt.MCPConfig.Name); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// uninstallNotifyHook removes the notify hook only if it matches the sx command
func (c *Client) uninstallNotifyHook(codexDir string) error {
	configPath := filepath.Join(codexDir, "config.toml")

	config, raw, err := handlers.ReadCodexConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config.toml: %w", err)
	}

	// Only remove if it matches our sx command
	notifyCmd := []string{"sx", "report-usage", "--client=codex"}
	if existing, ok := raw["notify"].([]any); ok && len(existing) == len(notifyCmd) {
		match := true
		for i, item := range existing {
			if s, ok := item.(string); !ok || s != notifyCmd[i] {
				match = false
				break
			}
		}
		if match {
			delete(raw, "notify")
			return handlers.WriteCodexConfig(configPath, config, raw)
		}
	}

	// Not our command, leave it alone
	return nil
}

// ShouldInstall always returns true for Codex.
// Codex doesn't have a session-start hook, so no deduplication is needed.
func (c *Client) ShouldInstall(ctx context.Context) (bool, error) {
	return true, nil
}

// VerifyAssets checks if assets are actually installed on the filesystem
func (c *Client) VerifyAssets(ctx context.Context, assets []*lockfile.Asset, scope *clients.InstallScope) []clients.VerifyResult {
	results := make([]clients.VerifyResult, 0, len(assets))

	for _, a := range assets {
		result := clients.VerifyResult{
			Asset: a,
		}

		targetBase, err := c.determineTargetBase(scope, a.Type)
		if err != nil {
			result.Installed = false
			result.Message = fmt.Sprintf("cannot determine target directory: %v", err)
			results = append(results, result)
			continue
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

// ScanInstalledAssets scans for unmanaged assets
func (c *Client) ScanInstalledAssets(ctx context.Context, scope *clients.InstallScope) ([]clients.InstalledAsset, error) {
	// Not yet implemented for Codex
	return []clients.InstalledAsset{}, nil
}

// GetAssetPath returns the filesystem path to an installed asset
func (c *Client) GetAssetPath(ctx context.Context, name string, assetType asset.Type, scope *clients.InstallScope) (string, error) {
	targetBase, err := c.determineTargetBase(scope, assetType)
	if err != nil {
		return "", fmt.Errorf("cannot determine target directory: %w", err)
	}

	switch assetType {
	case asset.TypeSkill:
		return filepath.Join(targetBase, handlers.DirSkills, name), nil
	case asset.TypeCommand:
		return filepath.Join(targetBase, handlers.DirCommands, name+".md"), nil
	case asset.TypeMCP:
		return filepath.Join(targetBase, handlers.DirMCPServers, name), nil
	default:
		return "", fmt.Errorf("path not supported for type: %s", assetType)
	}
}

func init() {
	// Auto-register on package import
	clients.Register(NewClient())
}
