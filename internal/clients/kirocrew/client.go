// Package kirocrew implements the clients.Client interface for KiroCrew.
//
// Skills only, global scope only. Dispatched KiroCrew agents load skills from
// KiroCrew's own crew root (~/.kiro/crew/skills/ by default, or
// $KIROCREW_HOME/skills/ when KIROCREW_HOME is set) rather than kiro-cli's
// ~/.kiro/skills/. KiroCrew never reads a repo-local .kiro/crew, so repo/path
// scopes are skipped rather than written into a repo tree.
package kirocrew

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sleuth-io/sx/v2/internal/asset"
	"github.com/sleuth-io/sx/v2/internal/bootstrap"
	"github.com/sleuth-io/sx/v2/internal/clients"
	"github.com/sleuth-io/sx/v2/internal/clients/kirocrew/handlers"
	"github.com/sleuth-io/sx/v2/internal/lockfile"
	"github.com/sleuth-io/sx/v2/internal/metadata"
	"github.com/sleuth-io/sx/v2/internal/utils"
)

// Client implements the clients.Client interface for KiroCrew.
type Client struct {
	clients.BaseClient
}

// errScopeNotGlobal reports a non-global scope reaching a KiroCrew operation;
// KiroCrew reads skills only from its one global crew root.
var errScopeNotGlobal = errors.New("KiroCrew only supports global skill installs; repo/path scopes are not read by KiroCrew")

// skippedResults renders one skipped AssetResult per asset name. Used when the
// requested scope means there is nothing KiroCrew would ever load, so the run
// reports a skip per asset rather than failing the whole install.
func skippedResults(names []string, message string) []clients.AssetResult {
	results := make([]clients.AssetResult, 0, len(names))
	for _, name := range names {
		results = append(results, clients.AssetResult{
			AssetName: name,
			Status:    clients.StatusSkipped,
			Message:   message,
		})
	}
	return results
}

// NewClient creates a new KiroCrew client. It declares support for skills only;
// KiroCrew-native asset types (app, cron) are deferred to a follow-up.
func NewClient() *Client {
	return &Client{
		BaseClient: clients.NewBaseClient(
			clients.ClientIDKirocrew,
			"KiroCrew",
			[]asset.Type{
				asset.TypeSkill,
			},
		),
	}
}

// crewHomeBase returns the base directory the crew config dir resolves under for
// GLOBAL-scope installs. When KIROCREW_HOME is set (non-empty after trimming) it
// already points AT the crew home and is returned directly with fromEnv=true, so
// the caller must NOT append ConfigDir; when unset the base is the user's home
// and the caller joins ConfigDir onto it. A leading ~ is expanded and a relative
// value made absolute, so the result is always absolute.
func crewHomeBase() (base string, fromEnv bool, err error) {
	if raw := strings.TrimSpace(os.Getenv("KIROCREW_HOME")); raw != "" {
		expanded, err := utils.ExpandTilde(raw)
		if err != nil {
			return "", false, fmt.Errorf("cannot expand KIROCREW_HOME %q: %w", raw, err)
		}
		abs, err := filepath.Abs(expanded)
		if err != nil {
			return "", false, fmt.Errorf("cannot resolve KIROCREW_HOME %q: %w", raw, err)
		}
		return abs, true, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false, fmt.Errorf("cannot determine home directory: %w", err)
	}
	return home, false, nil
}

// globalCrewDir resolves the crew home directory for global-scope operations,
// coherently handling the KIROCREW_HOME override so the crew segment is never
// doubled.
//
//   - KIROCREW_HOME set   -> <KIROCREW_HOME>            (already the crew home)
//   - KIROCREW_HOME unset -> <userHome>/.kiro/crew
//
// Skills then land under <crewDir>/skills/<name>/ via the skill handler.
func globalCrewDir() (string, error) {
	base, fromEnv, err := crewHomeBase()
	if err != nil {
		return "", err
	}
	if fromEnv {
		return base, nil
	}
	return filepath.Join(base, handlers.ConfigDir), nil
}

// IsInstalled checks if KiroCrew is installed.
//  1. The kirocrew binary in PATH (most reliable).
//  2. The crew home directory exists (honoring KIROCREW_HOME).
func (c *Client) IsInstalled() bool {
	if _, err := exec.LookPath("kirocrew"); err == nil {
		return true
	}

	crewDir, err := globalCrewDir()
	if err == nil {
		if stat, err := os.Stat(crewDir); err == nil && stat.IsDir() {
			return true
		}
	}

	return false
}

// GetVersion returns the KiroCrew CLI version, or empty if unavailable.
func (c *Client) GetVersion() string {
	cmd := exec.Command("kirocrew", "--version")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// InstallAssets installs skills to KiroCrew. Non-skill asset types are skipped
// as unsupported, and every asset is skipped when the scope is not global.
func (c *Client) InstallAssets(ctx context.Context, req clients.InstallRequest) (clients.InstallResponse, error) {
	resp := clients.InstallResponse{
		Results: make([]clients.AssetResult, 0, len(req.Assets)),
	}

	targetBase, err := c.determineTargetBase(req.Scope)
	if err != nil {
		if errors.Is(err, errScopeNotGlobal) {
			names := make([]string, 0, len(req.Assets))
			for _, bundle := range req.Assets {
				names = append(names, bundle.Asset.Name)
			}
			resp.Results = skippedResults(names, err.Error())
			return resp, nil
		}
		return resp, fmt.Errorf("cannot determine installation directory: %w", err)
	}

	if err := os.MkdirAll(targetBase, 0755); err != nil {
		return resp, fmt.Errorf("failed to create target directory: %w", err)
	}

	for _, bundle := range req.Assets {
		result := clients.AssetResult{
			AssetName: bundle.Asset.Name,
		}

		switch bundle.Metadata.Asset.Type {
		case asset.TypeSkill:
			handler := handlers.NewSkillHandler(bundle.Metadata)
			err := handler.Install(ctx, bundle.ZipData, targetBase)
			result.Status, result.Message, result.Error = clients.TranslateInstallError(err, "Installed to "+targetBase)
		default:
			result.Status = clients.StatusSkipped
			result.Message = "Unsupported asset type: " + bundle.Metadata.Asset.Type.Key
		}

		resp.Results = append(resp.Results, result)
	}

	return resp, nil
}

// UninstallAssets removes skills from KiroCrew. Non-skill asset types are
// skipped as unsupported, and every asset is skipped when the scope is not
// global — a non-global scope was never a location this client installed to.
func (c *Client) UninstallAssets(ctx context.Context, req clients.UninstallRequest) (clients.UninstallResponse, error) {
	resp := clients.UninstallResponse{
		Results: make([]clients.AssetResult, 0, len(req.Assets)),
	}

	targetBase, err := c.determineTargetBase(req.Scope)
	if err != nil {
		if errors.Is(err, errScopeNotGlobal) {
			names := make([]string, 0, len(req.Assets))
			for _, a := range req.Assets {
				names = append(names, a.Name)
			}
			resp.Results = skippedResults(names, err.Error())
			return resp, nil
		}
		return resp, fmt.Errorf("cannot determine uninstall directory: %w", err)
	}

	for _, a := range req.Assets {
		result := clients.AssetResult{
			AssetName: a.Name,
		}

		switch a.Type {
		case asset.TypeSkill:
			meta := &metadata.Metadata{
				Asset: metadata.Asset{
					Name: a.Name,
					Type: a.Type,
				},
			}
			handler := handlers.NewSkillHandler(meta)
			if err := handler.Remove(ctx, targetBase); err != nil {
				result.Status = clients.StatusFailed
				result.Error = err
			} else {
				result.Status = clients.StatusSuccess
				result.Message = "Uninstalled successfully"
			}
		default:
			result.Status = clients.StatusSkipped
			result.Message = "Unsupported asset type: " + a.Type.Key
		}

		resp.Results = append(resp.Results, result)
	}

	return resp, nil
}

// determineTargetBase returns the installation directory based on scope. Only
// global scope resolves (to the crew home, honoring KIROCREW_HOME); repository
// and path scopes return errScopeNotGlobal.
func (c *Client) determineTargetBase(scope *clients.InstallScope) (string, error) {
	switch scope.Type {
	case clients.ScopeGlobal:
		return globalCrewDir()
	case clients.ScopeRepository, clients.ScopePath:
		return "", fmt.Errorf("%w (requested scope: %s)", errScopeNotGlobal, scope.Type)
	default:
		return globalCrewDir()
	}
}

// EnsureAssetSupport is a no-op for KiroCrew, which auto-discovers skills from
// its crew skills root and has no MCP server or steering file to manage.
func (c *Client) EnsureAssetSupport(ctx context.Context, scope *clients.InstallScope) error {
	return nil
}

// ListAssets returns all installed skills for a given scope.
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

// ReadSkill reads the content of a specific skill by name.
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

// GetBootstrapOptions returns no bootstrap options for KiroCrew (skills only).
func (c *Client) GetBootstrapOptions(ctx context.Context) []bootstrap.Option {
	return nil
}

// GetBootstrapPath returns an empty string; KiroCrew has no bootstrap infra.
func (c *Client) GetBootstrapPath() string {
	return ""
}

// InstallBootstrap is a no-op for KiroCrew (no hooks or MCP servers).
func (c *Client) InstallBootstrap(ctx context.Context, opts []bootstrap.Option) error {
	return nil
}

// UninstallBootstrap is a no-op for KiroCrew (no hooks or MCP servers).
func (c *Client) UninstallBootstrap(ctx context.Context, opts []bootstrap.Option) error {
	return nil
}

// ShouldInstall always returns true for KiroCrew.
func (c *Client) ShouldInstall(ctx context.Context) (bool, error) {
	return true, nil
}

// VerifyAssets checks if skills are actually installed on the filesystem. A
// non-global scope reports every asset as not installed with the global-only
// explanation.
func (c *Client) VerifyAssets(ctx context.Context, assets []*lockfile.Asset, scope *clients.InstallScope) []clients.VerifyResult {
	results := make([]clients.VerifyResult, 0, len(assets))

	targetBase, err := c.determineTargetBase(scope)
	if err != nil {
		message := fmt.Sprintf("cannot determine target directory: %v", err)
		if errors.Is(err, errScopeNotGlobal) {
			message = err.Error()
		}
		for _, a := range assets {
			results = append(results, clients.VerifyResult{
				Asset:     a,
				Installed: false,
				Message:   message,
			})
		}
		return results
	}

	for _, a := range assets {
		result := clients.VerifyResult{
			Asset: a,
		}

		if a.Type != asset.TypeSkill {
			result.Message = "unsupported asset type: " + a.Type.Key
			results = append(results, result)
			continue
		}

		handler := handlers.NewSkillHandler(&metadata.Metadata{
			Asset: metadata.Asset{
				Name:    a.Name,
				Version: a.Version,
				Type:    a.Type,
			},
		})
		result.Installed, result.Message = handler.VerifyInstalled(targetBase)
		results = append(results, result)
	}

	return results
}

// ScanInstalledAssets returns an empty list for KiroCrew (import not supported).
func (c *Client) ScanInstalledAssets(ctx context.Context, scope *clients.InstallScope) ([]clients.InstalledAsset, error) {
	return []clients.InstalledAsset{}, nil
}

// GetAssetPath returns an error for KiroCrew (asset import not supported).
func (c *Client) GetAssetPath(ctx context.Context, name string, assetType asset.Type, scope *clients.InstallScope) (string, error) {
	return "", errors.New("asset import not supported for KiroCrew")
}

func init() {
	// Auto-register on package import.
	clients.Register(NewClient())
}
