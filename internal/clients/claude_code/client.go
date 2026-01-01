package claude_code

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/clients"
	"github.com/sleuth-io/sx/internal/clients/claude_code/handlers"
	"github.com/sleuth-io/sx/internal/handlers/dirasset"
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/metadata"
)

var skillOps = dirasset.NewOperations("skills", &asset.TypeSkill)

// Client implements the clients.Client interface for Claude Code
type Client struct {
	clients.BaseClient
}

// NewClient creates a new Claude Code client
func NewClient() *Client {
	return &Client{
		BaseClient: clients.NewBaseClient(
			clients.ClientIDClaudeCode,
			"Claude Code",
			asset.AllTypes(),
		),
	}
}

// IsInstalled checks if Claude Code is installed by checking for .claude directory
func (c *Client) IsInstalled() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}

	configDir := filepath.Join(home, ".claude")
	if stat, err := os.Stat(configDir); err == nil {
		return stat.IsDir()
	}
	return false
}

// GetVersion returns the Claude Code version
func (c *Client) GetVersion() string {
	cmd := exec.Command("claude", "--version")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(output)
}

// InstallAssets installs assets to Claude Code using client-specific handlers
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
		case asset.TypeAgent:
			handler := handlers.NewAgentHandler(bundle.Metadata)
			err = handler.Install(ctx, bundle.ZipData, targetBase)
		case asset.TypeCommand:
			handler := handlers.NewCommandHandler(bundle.Metadata)
			err = handler.Install(ctx, bundle.ZipData, targetBase)
		case asset.TypeHook:
			handler := handlers.NewHookHandler(bundle.Metadata)
			err = handler.Install(ctx, bundle.ZipData, targetBase)
		case asset.TypeMCP:
			handler := handlers.NewMCPHandler(bundle.Metadata)
			err = handler.Install(ctx, bundle.ZipData, targetBase)
		case asset.TypeMCPRemote:
			handler := handlers.NewMCPRemoteHandler(bundle.Metadata)
			err = handler.Install(ctx, bundle.ZipData, targetBase)
		default:
			err = fmt.Errorf("unsupported asset type: %s", bundle.Metadata.Asset.Type.Key)
		}

		if err != nil {
			result.Status = clients.StatusFailed
			result.Error = err
			result.Message = fmt.Sprintf("Installation failed: %v", err)
		} else {
			result.Status = clients.StatusSuccess
			result.Message = fmt.Sprintf("Installed to %s", targetBase)
		}

		resp.Results = append(resp.Results, result)
	}

	return resp, nil
}

// UninstallAssets removes assets from Claude Code
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
		case asset.TypeAgent:
			handler := handlers.NewAgentHandler(meta)
			err = handler.Remove(ctx, targetBase)
		case asset.TypeCommand:
			handler := handlers.NewCommandHandler(meta)
			err = handler.Remove(ctx, targetBase)
		case asset.TypeHook:
			handler := handlers.NewHookHandler(meta)
			err = handler.Remove(ctx, targetBase)
		case asset.TypeMCP:
			handler := handlers.NewMCPHandler(meta)
			err = handler.Remove(ctx, targetBase)
		case asset.TypeMCPRemote:
			handler := handlers.NewMCPRemoteHandler(meta)
			err = handler.Remove(ctx, targetBase)
		default:
			err = fmt.Errorf("unsupported asset type: %s", a.Type.Key)
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
	home, _ := os.UserHomeDir()

	switch scope.Type {
	case clients.ScopeGlobal:
		return filepath.Join(home, ".claude"), nil
	case clients.ScopeRepository:
		if scope.RepoRoot == "" {
			return "", fmt.Errorf("repo-scoped install requires RepoRoot but none provided (not in a git repository?)")
		}
		return filepath.Join(scope.RepoRoot, ".claude"), nil
	case clients.ScopePath:
		if scope.RepoRoot == "" {
			return "", fmt.Errorf("path-scoped install requires RepoRoot but none provided (not in a git repository?)")
		}
		return filepath.Join(scope.RepoRoot, scope.Path, ".claude"), nil
	default:
		return filepath.Join(home, ".claude"), nil
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

	// Convert to InstalledSkill format
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

// EnsureAssetSupport is a no-op for Claude Code since it loads global rules fine.
// This method exists to satisfy the Client interface.
func (c *Client) EnsureAssetSupport(ctx context.Context, scope *clients.InstallScope) error {
	// Claude Code loads global rules, so no special setup needed
	return nil
}

// InstallBootstrap installs Claude Code infrastructure (hooks and MCP servers)
func (c *Client) InstallBootstrap(ctx context.Context) error {
	return installBootstrap()
}

// UninstallBootstrap removes Claude Code infrastructure (hooks and MCP servers)
func (c *Client) UninstallBootstrap(ctx context.Context) error {
	return uninstallBootstrap()
}

// ShouldInstall always returns true for Claude Code.
// Claude Code has a SessionStart hook that fires once per session, so no
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

// ScanInstalledAssets scans for unmanaged assets (those without metadata.toml)
// Assets with metadata.toml were installed by sx and are already managed.
func (c *Client) ScanInstalledAssets(ctx context.Context, scope *clients.InstallScope) ([]clients.InstalledAsset, error) {
	targetBase, err := c.determineTargetBase(scope)
	if err != nil {
		return nil, fmt.Errorf("cannot determine target directory: %w", err)
	}

	var assets []clients.InstalledAsset

	// Scan for unmanaged skills (have SKILL.md but no metadata.toml)
	skills, err := scanUnmanagedAssets(targetBase, "skills", "SKILL.md", asset.TypeSkill)
	if err != nil {
		return nil, fmt.Errorf("failed to scan skills: %w", err)
	}
	assets = append(assets, skills...)

	// Scan for unmanaged agents (single .md files in agents directory)
	agents, err := scanUnmanagedAgentFiles(targetBase)
	if err != nil {
		return nil, fmt.Errorf("failed to scan agents: %w", err)
	}
	assets = append(assets, agents...)

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

		// Check for prompt file (SKILL.md or skill.md, AGENT.md or agent.md)
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

// scanUnmanagedAgentFiles finds agent .md files that don't have a companion metadata file
func scanUnmanagedAgentFiles(targetBase string) ([]clients.InstalledAsset, error) {
	var assets []clients.InstalledAsset

	agentsPath := filepath.Join(targetBase, "agents")
	if _, err := os.Stat(agentsPath); os.IsNotExist(err) {
		return assets, nil
	}

	entries, err := os.ReadDir(agentsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read agents directory: %w", err)
	}

	for _, entry := range entries {
		// Skip directories - agents are single files
		if entry.IsDir() {
			continue
		}

		name := entry.Name()

		// Only consider .md files
		if !strings.HasSuffix(strings.ToLower(name), ".md") {
			continue
		}

		// Skip metadata files
		if strings.HasSuffix(name, ".metadata.toml") {
			continue
		}

		// Get agent name (strip .md extension)
		agentName := strings.TrimSuffix(name, filepath.Ext(name))

		// Skip if has companion metadata file (already managed by sx)
		metaPath := filepath.Join(agentsPath, agentName+".metadata.toml")
		if _, err := os.Stat(metaPath); err == nil {
			continue
		}

		assets = append(assets, clients.InstalledAsset{
			Name:    agentName,
			Version: "1.0", // Default version for unmanaged assets
			Type:    asset.TypeAgent,
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
		return filepath.Join(targetBase, "skills", name), nil
	case asset.TypeAgent:
		// Agents are single .md files
		return filepath.Join(targetBase, "agents", name+".md"), nil
	case asset.TypeCommand:
		// Commands are single .md files
		return filepath.Join(targetBase, "commands", name+".md"), nil
	default:
		return "", fmt.Errorf("import not supported for type: %s", assetType)
	}
}
