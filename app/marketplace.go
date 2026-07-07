package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/config"
	"github.com/sleuth-io/sx/internal/logger"
	"github.com/sleuth-io/sx/internal/utils"
	vaultpkg "github.com/sleuth-io/sx/internal/vault"
)

// The extensions marketplace is nothing more than another sx vault that
// happens to be full of app-plugin assets. Browsing opens it read-only;
// installing republishes the chosen asset into the CURRENT vault through
// the same validated path as "Add extension…", so a marketplace install
// and a hand-published extension are indistinguishable afterwards —
// same policy, same consent gate, same audit trail.

// DefaultMarketplaceURL is the canonical shared extensions repository.
const DefaultMarketplaceURL = "https://github.com/sleuth-io/sx-extensions"

// VaultSupportsExtensions reports whether the current library can STORE
// app-plugin assets. skills.new cloud libraries can't until the server
// side ships the type (the P5 milestone) — without this gate, installing
// an extension there fails mid-publish with the server's raw
// "Invalid type" validation error. File-backed vaults have no type
// registry to disagree with.
func (a *App) VaultSupportsExtensions() bool {
	cfg, err := config.Load()
	if err != nil {
		return false
	}
	return cfg.GetType() != "sleuth"
}

var errExtensionsUnsupported = errors.New(
	"this library's server doesn't support extensions yet — extensions need a git or local library (skills.new support is coming)")

// MarketplaceExtension is one browsable marketplace entry: the asset name
// plus the fields of its plugin.json the install/consent UI needs.
type MarketplaceExtension struct {
	AssetName   string   `json:"assetName"`
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	Author      string   `json:"author"`
	Permissions []string `json:"permissions"`
	// Installed means the current vault already has an app-plugin asset
	// with this name; the UI shows a label instead of an Install button.
	Installed bool `json:"installed"`
}

func (a *App) marketplaceConfigPath() (string, error) {
	dir, err := a.pluginDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "marketplace.json"), nil
}

// GetMarketplaceURL returns the configured marketplace repository, falling
// back to the default. Stored per profile alongside other extension state.
func (a *App) GetMarketplaceURL() string {
	path, err := a.marketplaceConfigPath()
	if err != nil {
		return DefaultMarketplaceURL
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return DefaultMarketplaceURL
	}
	var cfg struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil || strings.TrimSpace(cfg.URL) == "" {
		return DefaultMarketplaceURL
	}
	return cfg.URL
}

// SetMarketplaceURL points browsing at a different repository (a team can
// host its own). Empty resets to the default.
func (a *App) SetMarketplaceURL(url string) error {
	path, err := a.marketplaceConfigPath()
	if err != nil {
		return err
	}
	data, err := json.Marshal(struct {
		URL string `json:"url"`
	}{URL: strings.TrimSpace(url)})
	if err != nil {
		return err
	}
	return atomicWriteFile(path, data)
}

// openMarketplaceVault opens the marketplace repository read-only. Local
// directories (including file:// URLs) open as path vaults so a team can
// point at a synced folder — or a test at a fixture — and anything else
// is treated as a git remote.
func openMarketplaceVault(rawURL string) (vaultpkg.Vault, error) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return nil, errors.New("no marketplace repository configured")
	}
	if strings.HasPrefix(trimmed, "file://") {
		return vaultpkg.NewPathVault(trimmed)
	}
	if info, err := os.Stat(trimmed); err == nil && info.IsDir() {
		abs, err := filepath.Abs(trimmed)
		if err != nil {
			return nil, err
		}
		return vaultpkg.NewPathVault("file://" + abs)
	}
	return vaultpkg.NewGitVault(trimmed)
}

// SearchMarketplace lists the marketplace's extensions matching query
// (empty lists everything), newest metadata included, flagged with
// whether each is already installed in the current vault. Entries whose
// bundles are malformed are skipped with a log — one bad asset must not
// blank the marketplace.
func (a *App) SearchMarketplace(query string) ([]MarketplaceExtension, error) {
	out := []MarketplaceExtension{}
	mkt, err := openMarketplaceVault(a.GetMarketplaceURL())
	if err != nil {
		return out, err
	}
	res, err := mkt.ListAssets(a.ctx, vaultpkg.ListAssetsOptions{
		Type: asset.TypeAppPlugin.Key, Search: strings.TrimSpace(query), Limit: 100,
	})
	if err != nil {
		return out, fmt.Errorf("couldn't reach the marketplace: %w", err)
	}

	installed := map[string]bool{}
	if v, err := a.currentVault(); err == nil {
		if mine, err := v.ListAssets(a.ctx, vaultpkg.ListAssetsOptions{Type: asset.TypeAppPlugin.Key, Limit: 200}); err == nil {
			for _, s := range mine.Assets {
				installed[s.Name] = true
			}
		}
	}

	for _, summary := range res.Assets {
		entry, err := marketplaceEntry(a, mkt, summary.Name)
		if err != nil {
			logger.Get().Warn("skipping malformed marketplace extension", "asset", summary.Name, "error", err)
			continue
		}
		// Installed matches on the plugin ID: install republishes under
		// pm.ID (addExtensionFrom forces the name), so the current vault's
		// asset names are always ids — the MARKETPLACE's asset name is
		// whatever its publisher chose and may diverge.
		entry.Installed = installed[entry.ID]
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func marketplaceEntry(a *App, mkt vaultpkg.Vault, name string) (MarketplaceExtension, error) {
	zipData, err := latestZipFromVault(a.ctx, mkt, name)
	if err != nil {
		return MarketplaceExtension{}, err
	}
	manifestBytes, err := utils.ReadZipFile(zipData, "plugin.json")
	if err != nil {
		return MarketplaceExtension{}, errors.New("no plugin.json in the bundle")
	}
	var pm struct {
		ID          string   `json:"id"`
		Name        string   `json:"name"`
		Version     string   `json:"version"`
		Description string   `json:"description"`
		Author      string   `json:"author"`
		Permissions []string `json:"permissions"`
	}
	if err := json.Unmarshal(manifestBytes, &pm); err != nil {
		return MarketplaceExtension{}, errors.New("plugin.json is not valid JSON")
	}
	perms := pm.Permissions
	if perms == nil {
		perms = []string{}
	}
	return MarketplaceExtension{
		AssetName:   name,
		ID:          pm.ID,
		Name:        pm.Name,
		Version:     pm.Version,
		Description: pm.Description,
		Author:      pm.Author,
		Permissions: perms,
	}, nil
}

// InstallMarketplaceExtension copies one extension from the marketplace
// into the current vault: fetch the bundle, unpack it, and push it through
// the exact same validate-and-publish path as "Add extension…". The asset
// lands disabled at this layer; the frontend then enables it for the
// installing user (the permission list on the marketplace card is the
// consent) unless org policy blocks it. For everyone ELSE in the library
// it appears disabled, gated by their own consent.
func (a *App) InstallMarketplaceExtension(assetName string) (string, error) {
	if err := validateAssetRef(assetName, ""); err != nil {
		return "", err
	}
	if !a.VaultSupportsExtensions() {
		return "", errExtensionsUnsupported
	}
	mkt, err := openMarketplaceVault(a.GetMarketplaceURL())
	if err != nil {
		return "", err
	}
	zipData, err := latestZipFromVault(a.ctx, mkt, assetName)
	if err != nil {
		return "", fmt.Errorf("couldn't fetch %s from the marketplace: %w", assetName, err)
	}
	dir, err := os.MkdirTemp("", "sx-marketplace-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	if err := utils.ExtractZip(zipData, dir); err != nil {
		return "", fmt.Errorf("marketplace bundle for %s is not a valid archive", assetName)
	}
	// Publish regenerates metadata from plugin.json (the authoring path);
	// the marketplace copy's metadata.toml would only fight it.
	_ = os.Remove(filepath.Join(dir, "metadata.toml"))
	return a.addExtensionFrom(dir)
}
