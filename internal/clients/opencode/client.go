package opencode

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
	"github.com/sleuth-io/sx/internal/clients/opencode/handlers"
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/metadata"
)

// Client implements clients.Client for OpenCode (https://opencode.ai).
type Client struct {
	clients.BaseClient
}

// NewClient creates a new OpenCode client.
func NewClient() *Client {
	return &Client{
		BaseClient: clients.NewBaseClient(
			clients.ClientIDOpenCode,
			"OpenCode",
			[]asset.Type{
				asset.TypeSkill,
				asset.TypeCommand,
				asset.TypeMCP,
				asset.TypeAgent,
				asset.TypeRule,
			},
		),
	}
}

// IsInstalled checks for ~/.config/opencode/ or the opencode CLI.
// The config-dir check is preferred because `opencode` is a generic
// binary name; the config dir is a stronger signal of an actual install.
func (c *Client) IsInstalled() bool {
	if home, err := os.UserHomeDir(); err == nil {
		configDir := filepath.Join(home, handlers.GlobalConfigDir)
		if stat, err := os.Stat(configDir); err == nil && stat.IsDir() {
			return true
		}
	}

	if _, err := exec.LookPath("opencode"); err == nil {
		return true
	}
	return false
}

// GetVersion returns the opencode CLI version, or empty if unavailable.
func (c *Client) GetVersion() string {
	cmd := exec.Command("opencode", "--version")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// InstallAssets installs assets into OpenCode using per-type handlers.
func (c *Client) InstallAssets(ctx context.Context, req clients.InstallRequest) (clients.InstallResponse, error) {
	resp := clients.InstallResponse{
		Results: make([]clients.AssetResult, 0, len(req.Assets)),
	}

	targetBase, err := c.determineTargetBase(req.Scope)
	if err != nil {
		return resp, fmt.Errorf("cannot determine installation directory: %w", err)
	}

	if err := os.MkdirAll(targetBase, 0755); err != nil {
		return resp, fmt.Errorf("failed to create target directory: %w", err)
	}

	for _, bundle := range req.Assets {
		result := clients.AssetResult{AssetName: bundle.Asset.Name}

		var installErr error
		switch bundle.Metadata.Asset.Type {
		case asset.TypeSkill:
			installErr = handlers.NewSkillHandler(bundle.Metadata).Install(ctx, bundle.ZipData, targetBase)
		case asset.TypeCommand:
			installErr = handlers.NewCommandHandler(bundle.Metadata).Install(ctx, bundle.ZipData, targetBase)
		case asset.TypeMCP:
			installErr = handlers.NewMCPHandler(bundle.Metadata).Install(ctx, bundle.ZipData, targetBase)
		case asset.TypeAgent:
			installErr = handlers.NewAgentHandler(bundle.Metadata).Install(ctx, bundle.ZipData, targetBase)
		case asset.TypeRule:
			installErr = handlers.NewRuleHandler(bundle.Metadata, c.ruleRegisterPath(req.Scope, bundle.Asset.Name)).Install(ctx, bundle.ZipData, targetBase)
		default:
			result.Status = clients.StatusSkipped
			result.Message = "Unsupported asset type: " + bundle.Metadata.Asset.Type.Key
			resp.Results = append(resp.Results, result)
			continue
		}

		result.Status, result.Message, result.Error = clients.TranslateInstallError(installErr, "Installed to "+targetBase)
		resp.Results = append(resp.Results, result)
	}

	return resp, nil
}

// UninstallAssets removes assets from OpenCode.
func (c *Client) UninstallAssets(ctx context.Context, req clients.UninstallRequest) (clients.UninstallResponse, error) {
	resp := clients.UninstallResponse{
		Results: make([]clients.AssetResult, 0, len(req.Assets)),
	}

	targetBase, err := c.determineTargetBase(req.Scope)
	if err != nil {
		return resp, fmt.Errorf("cannot determine uninstall directory: %w", err)
	}

	for _, a := range req.Assets {
		result := clients.AssetResult{AssetName: a.Name}

		meta := &metadata.Metadata{
			Asset: metadata.Asset{Name: a.Name, Type: a.Type},
		}

		var uninstallErr error
		switch a.Type {
		case asset.TypeSkill:
			uninstallErr = handlers.NewSkillHandler(meta).Remove(ctx, targetBase)
		case asset.TypeCommand:
			uninstallErr = handlers.NewCommandHandler(meta).Remove(ctx, targetBase)
		case asset.TypeMCP:
			uninstallErr = handlers.NewMCPHandler(meta).Remove(ctx, targetBase)
		case asset.TypeAgent:
			uninstallErr = handlers.NewAgentHandler(meta).Remove(ctx, targetBase)
		case asset.TypeRule:
			uninstallErr = handlers.NewRuleHandler(meta, c.ruleRegisterPath(req.Scope, a.Name)).Remove(ctx, targetBase)
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

// ruleRegisterPath returns the string to write into opencode.json's
// `instructions` array for a rule install. Global installs register the
// absolute path under ~/.config/opencode; repo and path installs register a
// path relative to the config file's directory so the project stays
// portable across checkouts.
func (c *Client) ruleRegisterPath(scope *clients.InstallScope, ruleName string) string {
	switch scope.Type {
	case clients.ScopeRepository, clients.ScopePath:
		return filepath.Join(handlers.DirRules, ruleName+".md")
	case clients.ScopeGlobal:
		base, err := c.determineTargetBase(scope)
		if err != nil {
			return ""
		}
		return filepath.Join(base, handlers.DirRules, ruleName+".md")
	}
	return ""
}

// determineTargetBase resolves the install directory for the requested scope.
// Global installs go to ~/.config/opencode; repo/path installs go to the
// project's .opencode directory.
func (c *Client) determineTargetBase(scope *clients.InstallScope) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}

	switch scope.Type {
	case clients.ScopeGlobal:
		return filepath.Join(home, handlers.GlobalConfigDir), nil
	case clients.ScopeRepository:
		if scope.RepoRoot == "" {
			return "", errors.New("repo-scoped install requires RepoRoot but none provided (not in a git repository?)")
		}
		return filepath.Join(scope.RepoRoot, handlers.ProjectConfigDir), nil
	case clients.ScopePath:
		if scope.RepoRoot == "" {
			return "", errors.New("path-scoped install requires RepoRoot but none provided (not in a git repository?)")
		}
		return filepath.Join(scope.RepoRoot, scope.Path, handlers.ProjectConfigDir), nil
	default:
		return filepath.Join(home, handlers.GlobalConfigDir), nil
	}
}

// ListAssets returns installed skills for the given scope.
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

// ReadSkill reads the prompt content of an installed skill.
func (c *Client) ReadSkill(ctx context.Context, name string, scope *clients.InstallScope) (*clients.SkillContent, error) {
	targetBase, err := c.determineTargetBase(scope)
	if err != nil {
		return nil, fmt.Errorf("cannot determine target directory: %w", err)
	}

	result, err := handlers.SkillOps.ReadPromptContent(targetBase, name, handlers.DefaultSkillPromptFile, func(m *metadata.Metadata) string {
		if m.Skill != nil {
			return m.Skill.PromptFile
		}
		return ""
	})
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

// EnsureAssetSupport is a no-op for OpenCode (skills are auto-discovered).
func (c *Client) EnsureAssetSupport(_ context.Context, _ *clients.InstallScope) error {
	return nil
}

// RuleCapabilities exposes OpenCode's rule storage convention so sx can
// detect, parse, and generate rule files alongside the rest of its
// per-client rule pipeline.
func (c *Client) RuleCapabilities() *clients.RuleCapabilities {
	return RuleCapabilities()
}

// GetBootstrapOptions returns optional bootstrap items.
// OpenCode has no documented session-start hook today, so we only expose
// the MCP server registration.
func (c *Client) GetBootstrapOptions(_ context.Context) []bootstrap.Option {
	return []bootstrap.Option{
		bootstrap.SleuthAIQueryMCP(),
	}
}

// GetBootstrapPath returns the global opencode.json path for display.
func (c *Client) GetBootstrapPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, handlers.GlobalConfigDir, handlers.ConfigFile)
}

// InstallBootstrap installs MCP servers declared in the supplied options.
func (c *Client) InstallBootstrap(_ context.Context, opts []bootstrap.Option) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}
	configPath := filepath.Join(home, handlers.GlobalConfigDir, handlers.ConfigFile)

	for _, opt := range opts {
		if opt.MCPConfig == nil {
			continue
		}
		entry := handlers.MCPServerEntryFromBootstrap(opt.MCPConfig)
		if err := handlers.AddMCPServer(configPath, opt.MCPConfig.Name, entry); err != nil {
			return fmt.Errorf("failed to install MCP server %s: %w", opt.MCPConfig.Name, err)
		}
	}
	return nil
}

// UninstallBootstrap removes any MCP servers installed by InstallBootstrap.
func (c *Client) UninstallBootstrap(_ context.Context, opts []bootstrap.Option) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}
	configPath := filepath.Join(home, handlers.GlobalConfigDir, handlers.ConfigFile)

	for _, opt := range opts {
		if opt.MCPConfig == nil {
			continue
		}
		if err := handlers.RemoveMCPServer(configPath, opt.MCPConfig.Name); err != nil {
			return err
		}
	}
	return nil
}

// ShouldInstall always returns true (OpenCode has no hook-mode dedup needed yet).
func (c *Client) ShouldInstall(_ context.Context) (bool, error) {
	return true, nil
}

// VerifyAssets checks each asset's on-disk presence.
func (c *Client) VerifyAssets(_ context.Context, assets []*lockfile.Asset, scope *clients.InstallScope) []clients.VerifyResult {
	results := make([]clients.VerifyResult, 0, len(assets))

	targetBase, err := c.determineTargetBase(scope)
	if err != nil {
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
		result := clients.VerifyResult{Asset: a}

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

// ScanInstalledAssets is not yet implemented for OpenCode.
func (c *Client) ScanInstalledAssets(_ context.Context, _ *clients.InstallScope) ([]clients.InstalledAsset, error) {
	return []clients.InstalledAsset{}, nil
}

// GetAssetPath returns the filesystem path of an installed asset.
func (c *Client) GetAssetPath(_ context.Context, name string, assetType asset.Type, scope *clients.InstallScope) (string, error) {
	targetBase, err := c.determineTargetBase(scope)
	if err != nil {
		return "", fmt.Errorf("cannot determine target directory: %w", err)
	}

	switch assetType {
	case asset.TypeSkill:
		return filepath.Join(targetBase, handlers.DirSkills, name), nil
	case asset.TypeCommand:
		return filepath.Join(targetBase, handlers.DirCommands, name+".md"), nil
	case asset.TypeAgent:
		return filepath.Join(targetBase, handlers.DirAgents, name+".md"), nil
	case asset.TypeRule:
		return filepath.Join(targetBase, handlers.DirRules, name+".md"), nil
	default:
		return "", fmt.Errorf("path not supported for type: %s", assetType)
	}
}

func init() {
	clients.Register(NewClient())
}
