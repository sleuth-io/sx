package openclaw

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/bootstrap"
	"github.com/sleuth-io/sx/internal/cache"
	"github.com/sleuth-io/sx/internal/clients"
	"github.com/sleuth-io/sx/internal/clients/openclaw/handlers"
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/logger"
	"github.com/sleuth-io/sx/internal/metadata"
)

// Client implements the clients.Client interface for OpenClaw
type Client struct {
	clients.BaseClient
}

// NewClient creates a new OpenClaw client
func NewClient() *Client {
	return &Client{
		BaseClient: clients.NewBaseClient(
			clients.ClientIDOpenClaw,
			"OpenClaw",
			[]asset.Type{
				asset.TypeSkill,
			},
		),
	}
}

// IsInstalled checks if OpenClaw is installed by checking for ~/.openclaw directory or openclaw binary in PATH
func (c *Client) IsInstalled() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}

	configDir := filepath.Join(home, handlers.ConfigDir)
	if stat, err := os.Stat(configDir); err == nil && stat.IsDir() {
		return true
	}

	// Also check if openclaw binary is in PATH
	if _, err := exec.LookPath("openclaw"); err == nil {
		return true
	}

	return false
}

// GetVersion returns the OpenClaw version
func (c *Client) GetVersion() string {
	cmd := exec.Command("openclaw", "--version")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// InstallAssets installs assets to OpenClaw using client-specific handlers
func (c *Client) InstallAssets(ctx context.Context, req clients.InstallRequest) (clients.InstallResponse, error) {
	resp := clients.InstallResponse{
		Results: make([]clients.AssetResult, 0, len(req.Assets)),
	}

	for _, bundle := range req.Assets {
		result := clients.AssetResult{
			AssetName: bundle.Asset.Name,
		}

		targetBase, err := c.determineTargetBase(req.Scope, bundle.Metadata.Asset.Type)
		if err != nil {
			result.Status = clients.StatusSkipped
			result.Message = err.Error()
			resp.Results = append(resp.Results, result)
			continue
		}

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

// UninstallAssets removes assets from OpenClaw
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
			result.Status = clients.StatusSkipped
			result.Message = err.Error()
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
// OpenClaw only supports global scope — repo/path scopes are skipped.
func (c *Client) determineTargetBase(scope *clients.InstallScope, _ asset.Type) (string, error) {
	home, _ := os.UserHomeDir()

	switch scope.Type {
	case clients.ScopeGlobal:
		return filepath.Join(home, handlers.ConfigDir), nil
	case clients.ScopeRepository, clients.ScopePath:
		return "", fmt.Errorf("OpenClaw does not support %s-scoped assets (global only)", scope.Type)
	default:
		return filepath.Join(home, handlers.ConfigDir), nil
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

// EnsureAssetSupport is a no-op for OpenClaw since it has native SKILL.md discovery.
func (c *Client) EnsureAssetSupport(_ context.Context, _ *clients.InstallScope) error {
	return nil
}

// GetBootstrapOptions returns bootstrap options for OpenClaw.
// Note: MCP server registration is not yet supported — OpenClaw's config
// validation rejects unknown keys at the root level. We'll add MCP support
// once we confirm the correct config location.
func (c *Client) GetBootstrapOptions(_ context.Context) []bootstrap.Option {
	return []bootstrap.Option{
		bootstrap.SessionHook,
	}
}

// GetBootstrapPath returns the path to OpenClaw's config file.
func (c *Client) GetBootstrapPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, handlers.ConfigDir, "openclaw.json")
}

// InstallBootstrap installs OpenClaw infrastructure (hook, cron, MCP servers)
func (c *Client) InstallBootstrap(ctx context.Context, opts []bootstrap.Option) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}
	openclawDir := filepath.Join(home, handlers.ConfigDir)

	// Install session hook (if enabled)
	if bootstrap.ContainsKey(opts, bootstrap.SessionHookKey) {
		if err := c.installSessionHook(openclawDir); err != nil {
			return err
		}
		if err := c.installCronJob(); err != nil {
			return err
		}
	}

	return nil
}

// installSessionHook installs the TypeScript hook at ~/.openclaw/hooks/sx-install/
func (c *Client) installSessionHook(openclawDir string) error {
	log := logger.Get()

	hookDir := filepath.Join(openclawDir, handlers.DirHooks, "sx-install")
	if err := os.MkdirAll(hookDir, 0755); err != nil {
		return fmt.Errorf("failed to create hook directory: %w", err)
	}

	// Write HOOK.md with frontmatter
	hookMD := `---
events: [before_agent_start]
---

# sx Install Hook

Automatically runs sx install when an agent session begins to ensure
skills are up to date.
`
	hookMDPath := filepath.Join(hookDir, "HOOK.md")
	if err := os.WriteFile(hookMDPath, []byte(hookMD), 0644); err != nil {
		return fmt.Errorf("failed to write HOOK.md: %w", err)
	}

	// Write index.ts handler
	indexTS := `import { execSync } from "child_process";

export default async function handler() {
  try {
    execSync("sx install --hook-mode --client=openclaw", {
      stdio: "inherit",
      timeout: 30000,
    });
  } catch (error) {
    // Don't block agent startup on install failures
    console.error("[sx] install hook failed:", error);
  }
}
`
	indexTSPath := filepath.Join(hookDir, "index.ts")
	if err := os.WriteFile(indexTSPath, []byte(indexTS), 0644); err != nil {
		return fmt.Errorf("failed to write index.ts: %w", err)
	}

	log.Info("hook installed", "hook", "sx-install", "dir", hookDir)
	return nil
}

// installCronJob registers a cron job via openclaw CLI for periodic updates
func (c *Client) installCronJob() error {
	log := logger.Get()

	// Check if openclaw CLI is available
	if _, err := exec.LookPath("openclaw"); err != nil {
		log.Debug("openclaw CLI not in PATH, skipping cron registration")
		return nil
	}

	cmd := exec.Command("openclaw", "cron", "add", "sx-install",
		"--schedule", "*/30 * * * *",
		"--command", "sx install --hook-mode --client=openclaw --scope global --quiet",
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		log.Debug("failed to register cron job", "error", err, "output", string(output))
		// Non-fatal: cron is a nice-to-have, the session hook is primary
	} else {
		log.Info("cron job registered", "name", "sx-install", "schedule", "*/30 * * * *")
	}
	return nil
}

// UninstallBootstrap removes OpenClaw infrastructure
func (c *Client) UninstallBootstrap(ctx context.Context, opts []bootstrap.Option) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}
	openclawDir := filepath.Join(home, handlers.ConfigDir)

	for _, opt := range opts {
		switch opt.Key {
		case bootstrap.SessionHookKey:
			// Remove hook directory
			hookDir := filepath.Join(openclawDir, handlers.DirHooks, "sx-install")
			if err := os.RemoveAll(hookDir); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("failed to remove hook directory: %w", err)
			}
			// Remove cron job
			c.removeCronJob()

		default:
			// No other bootstrap options currently supported
		}
	}
	return nil
}

// removeCronJob removes the sx-install cron job via openclaw CLI
func (c *Client) removeCronJob() {
	log := logger.Get()

	if _, err := exec.LookPath("openclaw"); err != nil {
		return
	}

	cmd := exec.Command("openclaw", "cron", "remove", "sx-install")
	if output, err := cmd.CombinedOutput(); err != nil {
		log.Debug("failed to remove cron job", "error", err, "output", string(output))
	} else {
		log.Info("cron job removed", "name", "sx-install")
	}
}

// ShouldInstall checks if installation should proceed based on timestamp-based dedup.
// OpenClaw fires before_agent_start per session. We use a session cache with
// timestamp-based dedup to avoid redundant installs within a 1-hour window.
func (c *Client) ShouldInstall(_ context.Context) (bool, error) {
	log := logger.Get()

	sessionCache, err := cache.NewSessionCache(c.ID())
	if err != nil {
		log.Debug("failed to create session cache, proceeding with install", "error", err)
		return true, nil
	}

	// Use a synthetic session ID based on the current hour to dedup within a 1-hour window
	sessionID := time.Now().UTC().Truncate(time.Hour).Format(time.RFC3339)

	if sessionCache.HasSession(sessionID) {
		log.Debug("skipping install, already ran within this hour", "session", sessionID)
		return false, nil
	}

	// Record optimistically before install
	if err := sessionCache.RecordSession(sessionID); err != nil {
		log.Debug("failed to record session", "error", err)
	}

	// Cull entries older than 24 hours
	_ = sessionCache.CullOldEntries(24 * time.Hour)

	return true, nil
}

// VerifyAssets checks if assets are actually installed on the filesystem
func (c *Client) VerifyAssets(_ context.Context, assets []*lockfile.Asset, scope *clients.InstallScope) []clients.VerifyResult {
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
func (c *Client) ScanInstalledAssets(_ context.Context, _ *clients.InstallScope) ([]clients.InstalledAsset, error) {
	return []clients.InstalledAsset{}, nil
}

// GetAssetPath returns the filesystem path to an installed asset
func (c *Client) GetAssetPath(_ context.Context, name string, assetType asset.Type, scope *clients.InstallScope) (string, error) {
	targetBase, err := c.determineTargetBase(scope, assetType)
	if err != nil {
		return "", fmt.Errorf("cannot determine target directory: %w", err)
	}

	switch assetType {
	case asset.TypeSkill:
		return filepath.Join(targetBase, handlers.DirSkills, name), nil
	default:
		return "", fmt.Errorf("path not supported for type: %s", assetType)
	}
}

func init() {
	// Auto-register on package import
	clients.Register(NewClient())
}
