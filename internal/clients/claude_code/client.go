package claude_code

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/sleuth-io/skills/internal/asset"
	"github.com/sleuth-io/skills/internal/clients"
	"github.com/sleuth-io/skills/internal/clients/claude_code/handlers"
	"github.com/sleuth-io/skills/internal/handlers/dirasset"
	"github.com/sleuth-io/skills/internal/lockfile"
	"github.com/sleuth-io/skills/internal/metadata"
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
			"claude-code",
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

// InstallArtifacts installs artifacts to Claude Code using client-specific handlers
func (c *Client) InstallArtifacts(ctx context.Context, req clients.InstallRequest) (clients.InstallResponse, error) {
	resp := clients.InstallResponse{
		Results: make([]clients.ArtifactResult, 0, len(req.Artifacts)),
	}

	// Determine target directory based on scope
	targetBase := c.determineTargetBase(req.Scope)

	// Ensure target directory exists
	if err := os.MkdirAll(targetBase, 0755); err != nil {
		return resp, fmt.Errorf("failed to create target directory: %w", err)
	}

	// Install each artifact using appropriate handler
	for _, bundle := range req.Artifacts {
		result := clients.ArtifactResult{
			ArtifactName: bundle.Artifact.Name,
		}

		var err error
		switch bundle.Metadata.Artifact.Type {
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
			err = fmt.Errorf("unsupported artifact type: %s", bundle.Metadata.Artifact.Type.Key)
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

// UninstallArtifacts removes artifacts from Claude Code
func (c *Client) UninstallArtifacts(ctx context.Context, req clients.UninstallRequest) (clients.UninstallResponse, error) {
	resp := clients.UninstallResponse{
		Results: make([]clients.ArtifactResult, 0, len(req.Artifacts)),
	}

	targetBase := c.determineTargetBase(req.Scope)

	for _, art := range req.Artifacts {
		result := clients.ArtifactResult{
			ArtifactName: art.Name,
		}

		// Create minimal metadata for removal
		meta := &metadata.Metadata{
			Artifact: metadata.Artifact{
				Name: art.Name,
				Type: art.Type,
			},
		}

		var err error
		switch art.Type {
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
			err = fmt.Errorf("unsupported artifact type: %s", art.Type.Key)
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
func (c *Client) determineTargetBase(scope *clients.InstallScope) string {
	home, _ := os.UserHomeDir()

	switch scope.Type {
	case clients.ScopeGlobal:
		return filepath.Join(home, ".claude")
	case clients.ScopeRepository:
		return filepath.Join(scope.RepoRoot, ".claude")
	case clients.ScopePath:
		return filepath.Join(scope.RepoRoot, scope.Path, ".claude")
	default:
		return filepath.Join(home, ".claude")
	}
}

// ListSkills returns all installed skills for a given scope
func (c *Client) ListSkills(ctx context.Context, scope *clients.InstallScope) ([]clients.InstalledSkill, error) {
	targetBase := c.determineTargetBase(scope)

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
	targetBase := c.determineTargetBase(scope)

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

// EnsureSkillsSupport is a no-op for Claude Code since it loads global rules fine.
// This method exists to satisfy the Client interface.
func (c *Client) EnsureSkillsSupport(ctx context.Context, scope *clients.InstallScope) error {
	// Claude Code loads global rules, so no special setup needed
	return nil
}

// InstallHooks installs Claude Code-specific hooks (auto-update and usage tracking)
func (c *Client) InstallHooks(ctx context.Context) error {
	return installHooks()
}

// UninstallHooks removes Claude Code-specific hooks (SessionStart and PostToolUse)
func (c *Client) UninstallHooks(ctx context.Context) error {
	return uninstallHooks()
}

// ShouldInstall always returns true for Claude Code.
// Claude Code has a SessionStart hook that fires once per session, so no
// deduplication is needed.
func (c *Client) ShouldInstall(ctx context.Context) (bool, error) {
	return true, nil
}

// VerifyArtifacts checks if artifacts are actually installed on the filesystem
func (c *Client) VerifyArtifacts(ctx context.Context, artifacts []*lockfile.Artifact, scope *clients.InstallScope) []clients.VerifyResult {
	targetBase := c.determineTargetBase(scope)
	results := make([]clients.VerifyResult, 0, len(artifacts))

	for _, art := range artifacts {
		result := clients.VerifyResult{
			Artifact: art,
		}

		handler, err := handlers.NewHandler(art.Type, &metadata.Metadata{
			Artifact: metadata.Artifact{
				Name:    art.Name,
				Version: art.Version,
				Type:    art.Type,
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
