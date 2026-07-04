package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/sleuth-io/sx/internal/config"
	"github.com/sleuth-io/sx/internal/manifest"
	"github.com/sleuth-io/sx/internal/mgmt"
	"github.com/sleuth-io/sx/internal/utils"
	vaultpkg "github.com/sleuth-io/sx/internal/vault"
)

// App is the Wails-bound bridge between the frontend and sx's vault layer.
// It stays a thin translation layer: every operation routes through the same
// internal packages the CLI uses, so the app and CLI can never disagree
// about vault state.
type App struct {
	ctx context.Context

	mu    sync.Mutex
	vault vaultpkg.Vault
}

func NewApp() *App {
	return &App{}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.restoreWindowState(ctx)
}

func (a *App) shutdown(ctx context.Context) {
	a.saveWindowState(ctx)
}

// OpenSettings asks the frontend to show the settings view. Wired to the
// native menu's Settings… item (Cmd+, / Ctrl+,).
func (a *App) OpenSettings() {
	if a.ctx != nil {
		wailsruntime.EventsEmit(a.ctx, "open-settings")
	}
}

// Quit exits the app; wired to the native menu.
func (a *App) Quit() {
	if a.ctx != nil {
		wailsruntime.Quit(a.ctx)
	}
}

// currentVault lazily opens the configured vault, caching it for the
// process. Callers holding no lock may race on first open; the mutex makes
// that safe.
func (a *App) currentVault() (vaultpkg.Vault, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.vault != nil {
		return a.vault, nil
	}
	cfg, err := config.Load()
	if err != nil {
		return nil, errors.New("no vault configured")
	}
	// Mirror the CLI: a profile-configured identity becomes the actor for
	// every vault mutation (mgmt ops require a real, non-synthetic one).
	// Set unconditionally so switching to a profile WITHOUT an identity
	// clears the previous profile's override instead of leaking it.
	mgmt.SetIdentityOverride(cfg.Identity)
	v, err := vaultpkg.NewFromConfig(cfg)
	if err != nil {
		return nil, err
	}
	a.vault = v
	return v, nil
}

// HasIdentity reports whether vault mutations can already be attributed to
// a real person (git config user.email, or a configured profile identity).
// When false, onboarding asks for an email.
func (a *App) HasIdentity() bool {
	if cfg, err := config.Load(); err == nil && cfg.Identity != "" {
		return true
	}
	actor, err := mgmt.CurrentGitActor(a.ctx, "")
	if err != nil {
		return false
	}
	return actor.RequireRealIdentity() == nil
}

// resetVault drops the cached vault (after configuration changes).
func (a *App) resetVault() {
	a.mu.Lock()
	a.vault = nil
	a.mu.Unlock()
}

// VaultInfo describes the currently configured vault for the frontend.
type VaultInfo struct {
	Configured bool   `json:"configured"`
	Type       string `json:"type"`     // "git" | "path" | "sleuth"
	Location   string `json:"location"` // URL or path, display form
}

// GetVaultInfo reports whether a vault is configured and where it lives.
func (a *App) GetVaultInfo() VaultInfo {
	cfg, err := config.Load()
	if err != nil {
		return VaultInfo{Configured: false}
	}
	info := VaultInfo{Configured: true, Type: string(cfg.Type)}
	switch cfg.Type {
	case config.RepositoryTypeSleuth:
		info.Location = cfg.ServerURL
	case config.RepositoryTypeGit, config.RepositoryTypePath:
		info.Location = strings.TrimPrefix(cfg.RepositoryURL, "file://")
	default:
		info.Location = strings.TrimPrefix(cfg.RepositoryURL, "file://")
	}
	return info
}

// SetupLocalVault creates (or adopts) a local path vault at ~/SX Library
// and points the shared sx config at it. Zero-setup "Just me" onboarding.
// identity (an email) is required when the machine has no git identity —
// vault changes are attributed to it.
func (a *App) SetupLocalVault(identity string) (VaultInfo, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return VaultInfo{}, err
	}
	dir := filepath.Join(home, "SX Library")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return VaultInfo{}, err
	}
	cfg := &config.Config{
		Type:          config.RepositoryTypePath,
		RepositoryURL: "file://" + dir,
		Identity:      manifest.NormalizeEmail(identity),
	}
	if err := config.Save(cfg); err != nil {
		return VaultInfo{}, err
	}
	a.resetVault()
	return a.GetVaultInfo(), nil
}

// SetupGitVault points the shared sx config at a team git vault. The
// repository is validated BEFORE the config is saved so a typo'd URL leaves
// the app in onboarding rather than persisting a broken configuration.
func (a *App) SetupGitVault(url, identity string) (VaultInfo, error) {
	url = strings.TrimSpace(url)
	if url == "" {
		return VaultInfo{}, errors.New("enter the git repository URL your team shares")
	}
	cfg := &config.Config{
		Type:          config.RepositoryTypeGit,
		RepositoryURL: url,
		Identity:      manifest.NormalizeEmail(identity),
	}
	v, err := vaultpkg.NewFromConfig(cfg)
	if err != nil {
		return VaultInfo{}, friendlyVaultError(err)
	}
	if _, err := v.ListAssets(a.ctx, vaultpkg.ListAssetsOptions{Limit: 1}); err != nil {
		return VaultInfo{}, friendlyVaultError(err)
	}
	if err := config.Save(cfg); err != nil {
		return VaultInfo{}, err
	}
	a.resetVault()
	return a.GetVaultInfo(), nil
}

// friendlyVaultError compresses the vault layer's CLI-oriented, multi-line
// error text into a short message for the UI: first line only, wrapper
// prefixes trimmed, capped in length. CLI remediation steps (flags, git
// commands) stay out of the app.
func friendlyVaultError(err error) error {
	msg := err.Error()
	if i := strings.IndexByte(msg, '\n'); i > 0 {
		msg = msg[:i]
	}
	for _, prefix := range []string{
		"failed to clone/update repository: ",
		"failed to create vault: ",
	} {
		msg = strings.TrimPrefix(msg, prefix)
	}
	msg = strings.TrimSpace(msg)
	if len(msg) > 160 {
		msg = msg[:157] + "…"
	}
	if msg == "" {
		return errors.New("couldn't reach that repository — check the URL and your access")
	}
	return fmt.Errorf("couldn't connect: %s — check the URL and that you have access", msg)
}

// AssetCard is one library entry.
type AssetCard struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        string `json:"type"`
	TypeLabel   string `json:"typeLabel"`
	Version     string `json:"version"`
	Versions    int    `json:"versions"`
	UpdatedAt   string `json:"updatedAt"` // RFC3339, may be empty
}

// ListAssets returns every asset in the vault for the library view.
func (a *App) ListAssets() ([]AssetCard, error) {
	v, err := a.currentVault()
	if err != nil {
		return nil, err
	}
	result, err := v.ListAssets(a.ctx, vaultpkg.ListAssetsOptions{})
	if err != nil {
		return nil, friendlyVaultError(err)
	}
	cards := make([]AssetCard, 0, len(result.Assets))
	for _, item := range result.Assets {
		card := AssetCard{
			Name:        item.Name,
			Description: item.Description,
			Type:        item.Type.Key,
			TypeLabel:   item.Type.Label,
			Version:     item.LatestVersion,
			Versions:    item.VersionsCount,
		}
		if !item.UpdatedAt.IsZero() {
			card.UpdatedAt = item.UpdatedAt.Format(time.RFC3339)
		}
		cards = append(cards, card)
	}
	sort.Slice(cards, func(i, j int) bool { return cards[i].Name < cards[j].Name })
	return cards, nil
}

// AssetFile is one file inside an asset, for the detail view.
type AssetFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// AssetDetail is the full view of one asset at a version.
type AssetDetail struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Type        string      `json:"type"`
	TypeLabel   string      `json:"typeLabel"`
	Version     string      `json:"version"`
	Versions    []string    `json:"versions"`
	Files       []AssetFile `json:"files"`
}

// GetAsset loads an asset's files at the given version (empty = latest).
func (a *App) GetAsset(name, version string) (AssetDetail, error) {
	v, err := a.currentVault()
	if err != nil {
		return AssetDetail{}, err
	}
	versions, err := v.GetVersionList(a.ctx, name)
	if err != nil {
		return AssetDetail{}, err
	}
	if len(versions) == 0 {
		return AssetDetail{}, fmt.Errorf("%s has no versions", name)
	}
	if version == "" {
		version = versions[len(versions)-1]
	}

	detail := AssetDetail{Name: name, Version: version, Versions: versions}

	if meta, err := v.GetMetadata(a.ctx, name, version); err == nil {
		detail.Description = meta.Asset.Description
		detail.Type = meta.Asset.Type.Key
		detail.TypeLabel = meta.Asset.Type.Label
	}

	zipData, err := v.GetAssetByVersion(a.ctx, name, version)
	if err != nil {
		return AssetDetail{}, err
	}
	entries, err := utils.ListZipFiles(zipData)
	if err != nil {
		return AssetDetail{}, err
	}
	sort.Strings(entries)
	for _, entry := range entries {
		if strings.HasSuffix(entry, "/") {
			continue
		}
		content, err := utils.ReadZipFile(zipData, entry)
		if err != nil {
			continue
		}
		detail.Files = append(detail.Files, AssetFile{Path: entry, Content: string(content)})
	}
	return detail, nil
}
