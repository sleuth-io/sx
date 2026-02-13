package github_copilot

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
	"github.com/sleuth-io/sx/internal/clients/github_copilot/handlers"
	"github.com/sleuth-io/sx/internal/handlers/dirasset"
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/metadata"
)

var skillOps = dirasset.NewOperations(handlers.DirSkills, &asset.TypeSkill)

// Client implements the clients.Client interface for GitHub Copilot
type Client struct {
	clients.BaseClient
}

// NewClient creates a new GitHub Copilot client
func NewClient() *Client {
	return &Client{
		BaseClient: clients.NewBaseClient(
			clients.ClientIDGitHubCopilot,
			"GitHub Copilot",
			[]asset.Type{
				asset.TypeSkill,
				asset.TypeRule,
				asset.TypeCommand,
				asset.TypeAgent,
				asset.TypeMCP,
			},
		),
	}
}

// IsInstalled checks if GitHub Copilot is installed by looking for ~/.copilot directory.
// This prevents sx from creating .copilot directories for users who don't use Copilot.
// The ~/.copilot directory is created when Copilot CLI is installed or configured.
// Note: Copilot spans many editors (VS Code, JetBrains, Neovim, CLI), so this is a
// best-effort check. Users can also control targeting via enabledClients configuration.
func (c *Client) IsInstalled() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}

	configDir := filepath.Join(home, ".copilot")
	if stat, err := os.Stat(configDir); err == nil {
		return stat.IsDir()
	}
	return false
}

// GetVersion returns the GitHub Copilot version by running `copilot version`
func (c *Client) GetVersion() string {
	cmd := exec.Command("copilot", "version")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	// Output format: "GitHub Copilot CLI X.Y.Z\n..."
	lines := strings.Split(string(output), "\n")
	if len(lines) > 0 {
		line := strings.TrimSpace(lines[0])
		if version, found := strings.CutPrefix(line, "GitHub Copilot CLI "); found {
			return version
		}
		return line
	}
	return ""
}

// InstallAssets installs assets to GitHub Copilot using client-specific handlers
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
		result := clients.AssetResult{
			AssetName: bundle.Asset.Name,
		}

		var installErr error
		switch bundle.Metadata.Asset.Type {
		case asset.TypeSkill:
			handler := handlers.NewSkillHandler(bundle.Metadata)
			installErr = handler.Install(ctx, bundle.ZipData, targetBase)
		case asset.TypeRule:
			handler := handlers.NewRuleHandler(bundle.Metadata)
			installErr = handler.Install(ctx, bundle.ZipData, targetBase)
		case asset.TypeCommand:
			handler := handlers.NewCommandHandler(bundle.Metadata)
			installErr = handler.Install(ctx, bundle.ZipData, targetBase)
		case asset.TypeAgent:
			handler := handlers.NewAgentHandler(bundle.Metadata)
			installErr = handler.Install(ctx, bundle.ZipData, targetBase)
		case asset.TypeMCP:
			// Remote MCP (sse/http) not supported for GitHub Copilot
			if bundle.Metadata.MCP != nil && bundle.Metadata.MCP.IsRemote() {
				result.Status = clients.StatusSkipped
				result.Message = "Remote MCP (sse/http) not supported"
				resp.Results = append(resp.Results, result)
				continue
			}
			// MCP uses .vscode/ instead of .github/
			mcpTargetBase, mcpErr := c.determineMCPTargetBase(req.Scope)
			if mcpErr != nil {
				installErr = mcpErr
			} else {
				handler := handlers.NewMCPHandler(bundle.Metadata)
				installErr = handler.Install(ctx, bundle.ZipData, mcpTargetBase)
			}
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

// UninstallAssets removes assets from GitHub Copilot
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
		case asset.TypeRule:
			handler := handlers.NewRuleHandler(meta)
			uninstallErr = handler.Remove(ctx, targetBase)
		case asset.TypeCommand:
			handler := handlers.NewCommandHandler(meta)
			uninstallErr = handler.Remove(ctx, targetBase)
		case asset.TypeAgent:
			handler := handlers.NewAgentHandler(meta)
			uninstallErr = handler.Remove(ctx, targetBase)
		case asset.TypeMCP:
			// MCP uses .vscode/ instead of .github/
			mcpTargetBase, mcpErr := c.determineMCPTargetBase(req.Scope)
			if mcpErr != nil {
				uninstallErr = mcpErr
			} else {
				handler := handlers.NewMCPHandler(meta)
				uninstallErr = handler.Remove(ctx, mcpTargetBase)
			}
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

// determineTargetBase returns the installation directory based on scope.
// Global scope uses ~/.copilot/ while repo/path scopes use .github/ under the repo.
//
// Path-scoped skills are installed to {repoRoot}/{path}/.github/ to match Claude Code's
// behavior. Note that by default, VS Code's Copilot only discovers skills from
// {repoRoot}/.github/skills/. For path-scoped skills to be discovered, users must
// configure the chat.agentSkillsLocations setting in VS Code to include the subdirectory
// path (e.g., "src/backend/.github/skills").
func (c *Client) determineTargetBase(scope *clients.InstallScope) (string, error) {
	home, _ := os.UserHomeDir()

	switch scope.Type {
	case clients.ScopeGlobal:
		return filepath.Join(home, ".copilot"), nil
	case clients.ScopeRepository:
		if scope.RepoRoot == "" {
			return "", errors.New("repo-scoped install requires RepoRoot but none provided (not in a git repository?)")
		}
		return filepath.Join(scope.RepoRoot, ".github"), nil
	case clients.ScopePath:
		// Install to {repoRoot}/{path}/.github/ to match Claude Code behavior.
		// Users may need to configure chat.agentSkillsLocations in VS Code for discovery.
		if scope.RepoRoot == "" {
			return "", errors.New("path-scoped install requires RepoRoot but none provided (not in a git repository?)")
		}
		return filepath.Join(scope.RepoRoot, scope.Path, ".github"), nil
	default:
		return filepath.Join(home, ".copilot"), nil
	}
}

// determineMCPTargetBase returns the installation directory for MCP servers.
// MCP servers use .vscode/ for VS Code integration, not .github/.
func (c *Client) determineMCPTargetBase(scope *clients.InstallScope) (string, error) {
	home, _ := os.UserHomeDir()

	switch scope.Type {
	case clients.ScopeGlobal:
		// Global MCP config goes to VS Code user settings directory
		return filepath.Join(home, ".vscode"), nil
	case clients.ScopeRepository, clients.ScopePath:
		if scope.RepoRoot == "" {
			return "", errors.New("repo/path-scoped MCP install requires RepoRoot but none provided (not in a git repository?)")
		}
		return filepath.Join(scope.RepoRoot, ".vscode"), nil
	default:
		return filepath.Join(home, ".vscode"), nil
	}
}

// RuleCapabilities returns Copilot's rule capabilities
func (c *Client) RuleCapabilities() *clients.RuleCapabilities {
	return RuleCapabilities()
}

// EnsureAssetSupport is a no-op for GitHub Copilot.
// Copilot discovers skills natively from the skills directory.
func (c *Client) EnsureAssetSupport(ctx context.Context, scope *clients.InstallScope) error {
	return nil
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

// GetBootstrapOptions returns an empty list — GitHub Copilot has no hook mechanism.
func (c *Client) GetBootstrapOptions(ctx context.Context) []bootstrap.Option {
	return []bootstrap.Option{}
}

// InstallBootstrap is a no-op — GitHub Copilot has no hook mechanism.
func (c *Client) InstallBootstrap(ctx context.Context, opts []bootstrap.Option) error {
	return nil
}

// UninstallBootstrap is a no-op — GitHub Copilot has no hook mechanism.
func (c *Client) UninstallBootstrap(ctx context.Context, opts []bootstrap.Option) error {
	return nil
}

// ShouldInstall always returns true — no session tracking needed without hooks.
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

		// Determine target base based on asset type
		var targetBase string
		var err error
		if a.Type == asset.TypeMCP {
			targetBase, err = c.determineMCPTargetBase(scope)
		} else {
			targetBase, err = c.determineTargetBase(scope)
		}
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

// ScanInstalledAssets returns an empty list (not yet supported)
func (c *Client) ScanInstalledAssets(ctx context.Context, scope *clients.InstallScope) ([]clients.InstalledAsset, error) {
	return []clients.InstalledAsset{}, nil
}

// GetAssetPath returns the filesystem path for an asset
func (c *Client) GetAssetPath(ctx context.Context, name string, assetType asset.Type, scope *clients.InstallScope) (string, error) {
	// MCP and MCP-Remote use a different target base
	if assetType == asset.TypeMCP {
		mcpBase, err := c.determineMCPTargetBase(scope)
		if err != nil {
			return "", err
		}
		return filepath.Join(mcpBase, handlers.DirMCPServers, name), nil
	}

	targetBase, err := c.determineTargetBase(scope)
	if err != nil {
		return "", err
	}

	switch assetType {
	case asset.TypeSkill:
		return filepath.Join(targetBase, handlers.DirSkills, name), nil
	case asset.TypeRule:
		return filepath.Join(targetBase, handlers.DirInstructions, name+".instructions.md"), nil
	case asset.TypeCommand:
		return filepath.Join(targetBase, handlers.DirPrompts, name+".prompt.md"), nil
	case asset.TypeAgent:
		return filepath.Join(targetBase, handlers.DirAgents, name+".agent.md"), nil
	default:
		return "", fmt.Errorf("asset type %s not supported for GitHub Copilot", assetType.Key)
	}
}

func init() {
	// Auto-register on package import
	clients.Register(NewClient())
}
