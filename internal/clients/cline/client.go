package cline

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
	"github.com/sleuth-io/sx/internal/clients/cline/handlers"
	"github.com/sleuth-io/sx/internal/handlers/dirasset"
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/logger"
	"github.com/sleuth-io/sx/internal/metadata"
)

var skillOps = dirasset.NewOperations(handlers.DirSkills, &asset.TypeSkill)

// Client implements the clients.Client interface for Cline
type Client struct {
	clients.BaseClient
}

// NewClient creates a new Cline client
func NewClient() *Client {
	return &Client{
		BaseClient: clients.NewBaseClient(
			clients.ClientIDCline,
			"Cline",
			[]asset.Type{
				asset.TypeSkill,   // .cline/skills/{name}/
				asset.TypeCommand, // .cline/skills/{name}/ (same as skill)
				asset.TypeRule,    // .clinerules/{name}.md
				asset.TypeMCP,     // cline_mcp_settings.json
				asset.TypeHook,    // ~/Documents/Cline/Hooks/
			},
		),
	}
}

// RuleCapabilities returns Cline's rule capabilities
func (c *Client) RuleCapabilities() *clients.RuleCapabilities {
	return RuleCapabilities()
}

// IsInstalled checks if Cline is installed by checking for .cline directory
// or VS Code extension's globalStorage
func (c *Client) IsInstalled() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}

	// Check for global .cline directory
	globalConfigDir := filepath.Join(home, handlers.ConfigDir)
	if stat, err := os.Stat(globalConfigDir); err == nil && stat.IsDir() {
		return true
	}

	// Check for VS Code extension's MCP config directory
	mcpConfigDir, err := handlers.GetMCPConfigDir()
	if err == nil {
		if stat, err := os.Stat(mcpConfigDir); err == nil && stat.IsDir() {
			return true
		}
	}

	// Check for .cline in current working directory
	cwd, err := os.Getwd()
	if err == nil {
		localConfigDir := filepath.Join(cwd, handlers.ConfigDir)
		if stat, err := os.Stat(localConfigDir); err == nil && stat.IsDir() {
			return true
		}
	}

	return false
}

// GetVersion returns the Cline CLI version
func (c *Client) GetVersion() string {
	cmd := exec.Command("cline", "--version")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// InstallAssets installs assets to Cline using client-specific handlers
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
		case asset.TypeSkill:
			handler := handlers.NewSkillHandler(bundle.Metadata)
			err = handler.Install(ctx, bundle.ZipData, targetBase)
		case asset.TypeCommand:
			handler := handlers.NewCommandHandler(bundle.Metadata)
			err = handler.Install(ctx, bundle.ZipData, targetBase)
		case asset.TypeRule:
			handler := handlers.NewRuleHandler(bundle.Metadata)
			err = handler.Install(ctx, bundle.ZipData, targetBase)
		case asset.TypeMCP:
			handler := handlers.NewMCPHandler(bundle.Metadata)
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

		result.Status, result.Message, result.Error = clients.TranslateInstallError(err, "Installed to "+targetBase)
		resp.Results = append(resp.Results, result)
	}

	return resp, nil
}

// UninstallAssets removes assets from Cline
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
		case asset.TypeSkill:
			handler := handlers.NewSkillHandler(meta)
			err = handler.Remove(ctx, targetBase)
		case asset.TypeCommand:
			handler := handlers.NewCommandHandler(meta)
			err = handler.Remove(ctx, targetBase)
		case asset.TypeRule:
			handler := handlers.NewRuleHandler(meta)
			err = handler.Remove(ctx, targetBase)
		case asset.TypeMCP:
			handler := handlers.NewMCPHandler(meta)
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

// ListAssets returns all installed skills for a given scope
func (c *Client) ListAssets(ctx context.Context, scope *clients.InstallScope) ([]clients.InstalledSkill, error) {
	targetBase, err := c.determineTargetBase(scope)
	if err != nil {
		return nil, fmt.Errorf("cannot determine target directory: %w", err)
	}

	installed, err := skillOps.ScanInstalled(targetBase)
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

	result, err := skillOps.ReadPromptContent(targetBase, name, "SKILL.md", func(m *metadata.Metadata) string { return m.Skill.PromptFile })
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

// EnsureAssetSupport is a no-op for Cline since it has native skill discovery.
func (c *Client) EnsureAssetSupport(ctx context.Context, scope *clients.InstallScope) error {
	// Cline loads skills from .cline/skills/ natively, no special setup needed
	return nil
}

// GetBootstrapOptions returns bootstrap options for Cline.
// Cline v3.36+ supports hooks via shell scripts in ~/Documents/Cline/Hooks/.
func (c *Client) GetBootstrapOptions(ctx context.Context) []bootstrap.Option {
	return []bootstrap.Option{
		bootstrap.SessionHook,
		bootstrap.AnalyticsHook,
		bootstrap.SleuthAIQueryMCP(),
	}
}

// GetBootstrapPath returns the path to Cline's MCP settings file.
func (c *Client) GetBootstrapPath() string {
	path, err := handlers.GetMCPConfigPath()
	if err != nil {
		return ""
	}
	return path
}

// InstallBootstrap installs Cline infrastructure (hooks and MCP servers).
func (c *Client) InstallBootstrap(ctx context.Context, opts []bootstrap.Option) error {
	log := logger.Get()

	// Install session hook for auto-update (if enabled)
	if bootstrap.ContainsKey(opts, bootstrap.SessionHookKey) {
		if err := installSessionHook(); err != nil {
			log.Error("failed to install session hook", "error", err)
			return fmt.Errorf("failed to install session hook: %w", err)
		}
	}

	// Install analytics hook for usage tracking (if enabled)
	if bootstrap.ContainsKey(opts, bootstrap.AnalyticsHookKey) {
		if err := installAnalyticsHook(); err != nil {
			log.Error("failed to install analytics hook", "error", err)
			return fmt.Errorf("failed to install analytics hook: %w", err)
		}
	}

	// Install MCP servers from options that have MCPConfig
	for _, opt := range opts {
		if opt.MCPConfig != nil {
			if err := c.installMCPServerFromConfig(opt.MCPConfig); err != nil {
				return fmt.Errorf("failed to install MCP server %s: %w", opt.MCPConfig.Name, err)
			}
			log.Info("MCP server installed", "server", opt.MCPConfig.Name, "client", "cline")
		}
	}

	return nil
}

// installMCPServerFromConfig installs an MCP server from a bootstrap.MCPServerConfig
func (c *Client) installMCPServerFromConfig(config *bootstrap.MCPServerConfig) error {
	serverConfig := map[string]any{
		"command": config.Command,
		"args":    config.Args,
	}

	if len(config.Env) > 0 {
		serverConfig["env"] = config.Env
	}

	return handlers.AddMCPServer(config.Name, serverConfig)
}

// UninstallBootstrap removes Cline infrastructure (hooks and MCP servers).
func (c *Client) UninstallBootstrap(ctx context.Context, opts []bootstrap.Option) error {
	log := logger.Get()

	for _, opt := range opts {
		switch opt.Key {
		case bootstrap.SessionHookKey:
			if err := uninstallSessionHook(); err != nil {
				return err
			}
		case bootstrap.AnalyticsHookKey:
			if err := uninstallAnalyticsHook(); err != nil {
				return err
			}
		}

		if opt.MCPConfig != nil {
			if err := handlers.RemoveMCPServer(opt.MCPConfig.Name); err != nil {
				return err
			}
			log.Info("MCP server uninstalled", "server", opt.MCPConfig.Name, "client", "cline")
		}
	}

	return nil
}

// ShouldInstall always returns true for Cline.
// Cline's TaskStart hook fires once per task, so no deduplication is needed.
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

// ScanInstalledAssets scans for unmanaged assets (those without metadata.toml)
func (c *Client) ScanInstalledAssets(ctx context.Context, scope *clients.InstallScope) ([]clients.InstalledAsset, error) {
	targetBase, err := c.determineTargetBase(scope)
	if err != nil {
		return nil, fmt.Errorf("cannot determine target directory: %w", err)
	}

	var assets []clients.InstalledAsset

	// Scan for unmanaged skills (have SKILL.md but no metadata.toml)
	skills, err := scanUnmanagedAssets(targetBase, handlers.DirSkills, "SKILL.md", asset.TypeSkill)
	if err != nil {
		return nil, fmt.Errorf("failed to scan skills: %w", err)
	}
	assets = append(assets, skills...)

	return assets, nil
}

// scanUnmanagedAssets finds assets that have the prompt file but no metadata.toml
func scanUnmanagedAssets(targetBase, subdir, promptFile string, assetType asset.Type) ([]clients.InstalledAsset, error) {
	var assets []clients.InstalledAsset

	assetsPath := filepath.Join(targetBase, subdir)
	if _, err := os.Stat(assetsPath); os.IsNotExist(err) {
		return assets, nil
	}

	dirs, err := os.ReadDir(assetsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s directory: %w", subdir, err)
	}

	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}

		dirPath := filepath.Join(assetsPath, dir.Name())

		// Skip if has metadata.toml (already managed by sx)
		metaPath := filepath.Join(dirPath, "metadata.toml")
		if _, err := os.Stat(metaPath); err == nil {
			continue
		}

		// Check for prompt file (SKILL.md or skill.md)
		hasPromptFile := false
		promptLower := strings.ToLower(promptFile)
		if _, err := os.Stat(filepath.Join(dirPath, promptFile)); err == nil {
			hasPromptFile = true
		} else if _, err := os.Stat(filepath.Join(dirPath, promptLower)); err == nil {
			hasPromptFile = true
		}

		if !hasPromptFile {
			continue
		}

		assets = append(assets, clients.InstalledAsset{
			Name:    dir.Name(),
			Version: "1.0", // Default version for unmanaged assets
			Type:    assetType,
		})
	}

	return assets, nil
}

// GetAssetPath returns the filesystem path to an installed asset
func (c *Client) GetAssetPath(ctx context.Context, name string, assetType asset.Type, scope *clients.InstallScope) (string, error) {
	targetBase, err := c.determineTargetBase(scope)
	if err != nil {
		return "", fmt.Errorf("cannot determine target directory: %w", err)
	}

	switch assetType {
	case asset.TypeSkill:
		// Skills are directories
		return filepath.Join(targetBase, handlers.DirSkills, name), nil
	default:
		return "", fmt.Errorf("import not supported for type: %s", assetType)
	}
}

func init() {
	// Auto-register on package import
	clients.Register(NewClient())
}
