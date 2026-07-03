package main

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/clients"
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/manifest"
	"github.com/sleuth-io/sx/internal/metadata"
	"github.com/sleuth-io/sx/internal/utils"
	vaultpkg "github.com/sleuth-io/sx/internal/vault"
)

// "Use in my AI tools": installing an asset delivers its latest revision to
// every detected AI client (Claude Code, Cursor, …) at user-global scope,
// through the exact same client implementations `sx install` uses.

// AIClient describes one detected AI tool for the frontend.
type AIClient struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ListAIClients reports the AI tools detected on this machine.
func (a *App) ListAIClients() []AIClient {
	detected := clients.Global().DetectInstalled()
	out := make([]AIClient, 0, len(detected))
	for _, c := range detected {
		out = append(out, AIClient{ID: c.ID(), Name: c.DisplayName()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// InstallResult summarizes an install/uninstall for the frontend.
type InstallResult struct {
	Clients []string `json:"clients"` // display names that received it
}

// bundleForLatest builds the client install bundle for an asset's latest
// revision.
func (a *App) bundleForLatest(name string) (*clients.AssetBundle, error) {
	v, err := a.currentVault()
	if err != nil {
		return nil, err
	}
	versions, err := v.GetVersionList(a.ctx, name)
	if err != nil || len(versions) == 0 {
		return nil, fmt.Errorf("%s has no published revisions", name)
	}
	latest := versions[len(versions)-1]
	zipData, err := v.GetAssetByVersion(a.ctx, name, latest)
	if err != nil {
		return nil, friendlyVaultError(err)
	}
	metaBytes, err := utils.ReadZipFile(zipData, "metadata.toml")
	if err != nil {
		return nil, fmt.Errorf("%s is missing its metadata", name)
	}
	meta, err := metadata.Parse(metaBytes)
	if err != nil {
		return nil, err
	}
	return &clients.AssetBundle{
		Asset: &lockfile.Asset{
			Name:    name,
			Version: latest,
			Type:    meta.Asset.Type,
			Clients: append([]string(nil), meta.Asset.Clients...),
		},
		Metadata: meta,
		ZipData:  zipData,
	}, nil
}

func globalScope() *clients.InstallScope {
	return &clients.InstallScope{Type: clients.ScopeGlobal}
}

// InstallAsset delivers an asset to every detected AI client.
func (a *App) InstallAsset(name string) (InstallResult, error) {
	bundle, err := a.bundleForLatest(name)
	if err != nil {
		return InstallResult{}, err
	}
	return a.installBundles([]*clients.AssetBundle{bundle})
}

// InstallCollection delivers every asset in a collection.
func (a *App) InstallCollection(name string) (InstallResult, error) {
	c, err := a.findCollection(name)
	if err != nil {
		return InstallResult{}, err
	}
	if len(c.Assets) == 0 {
		return InstallResult{}, fmt.Errorf("%s has no assets yet", name)
	}
	bundles := make([]*clients.AssetBundle, 0, len(c.Assets))
	var skipped []string
	for _, assetName := range c.Assets {
		bundle, err := a.bundleForLatest(assetName)
		if err != nil {
			skipped = append(skipped, assetName)
			continue
		}
		bundles = append(bundles, bundle)
	}
	if len(bundles) == 0 {
		return InstallResult{}, fmt.Errorf("none of the assets in %s could be loaded", name)
	}
	result, err := a.installBundles(bundles)
	if err != nil {
		return result, err
	}
	if len(skipped) > 0 {
		return result, fmt.Errorf("installed, but skipped: %s", strings.Join(skipped, ", "))
	}
	return result, nil
}

func (a *App) installBundles(bundles []*clients.AssetBundle) (InstallResult, error) {
	registry := clients.Global()
	orchestrator := clients.NewOrchestrator(registry)
	results := orchestrator.InstallToAll(a.ctx, bundles, globalScope(), clients.InstallOptions{})

	var succeeded []string
	var failures []string
	for id, resp := range results {
		client, err := registry.Get(id)
		displayName := id
		if err == nil {
			displayName = client.DisplayName()
		}
		ok := false
		for _, r := range resp.Results {
			switch r.Status {
			case clients.StatusSuccess:
				ok = true
			case clients.StatusFailed:
				failures = append(failures, displayName)
			case clients.StatusSkipped:
				// not compatible with this client — fine
			}
		}
		if ok {
			succeeded = append(succeeded, displayName)
		}
	}
	sort.Strings(succeeded)
	if len(succeeded) == 0 {
		if len(failures) > 0 {
			return InstallResult{}, fmt.Errorf("installation failed for %s", strings.Join(failures, ", "))
		}
		return InstallResult{}, errors.New("no AI tools on this machine can use this asset")
	}
	return InstallResult{Clients: succeeded}, nil
}

// UninstallAsset removes an asset from every detected AI client.
func (a *App) UninstallAsset(name string) error {
	bundle, err := a.bundleForLatest(name)
	if err != nil {
		return err
	}
	req := clients.UninstallRequest{
		Assets: []asset.Asset{{
			Name:    bundle.Asset.Name,
			Version: bundle.Asset.Version,
			Type:    bundle.Asset.Type,
		}},
		Scope: globalScope(),
	}
	for _, client := range clients.Global().DetectInstalled() {
		if !client.SupportsAssetType(bundle.Asset.Type) {
			continue
		}
		if _, err := client.UninstallAssets(a.ctx, req); err != nil {
			return fmt.Errorf("couldn't remove from %s: %w", client.DisplayName(), err)
		}
	}
	return nil
}

// Collection is the frontend view of a manifest collection.
type Collection struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Assets      []string `json:"assets"`
}

func (a *App) collectionStore() (vaultpkg.CollectionStore, error) {
	v, err := a.currentVault()
	if err != nil {
		return nil, err
	}
	store, ok := v.(vaultpkg.CollectionStore)
	if !ok {
		return nil, errors.New("this library doesn't support collections yet")
	}
	return store, nil
}

func (a *App) findCollection(name string) (manifest.Collection, error) {
	store, err := a.collectionStore()
	if err != nil {
		return manifest.Collection{}, err
	}
	all, err := store.ListCollections(a.ctx)
	if err != nil {
		return manifest.Collection{}, friendlyVaultError(err)
	}
	for _, c := range all {
		if c.Name == name {
			return c, nil
		}
	}
	return manifest.Collection{}, fmt.Errorf("collection %s not found", name)
}

// ListCollections returns the vault's collections.
func (a *App) ListCollections() ([]Collection, error) {
	store, err := a.collectionStore()
	if err != nil {
		return nil, err
	}
	all, err := store.ListCollections(a.ctx)
	if err != nil {
		return nil, friendlyVaultError(err)
	}
	out := make([]Collection, 0, len(all))
	for _, c := range all {
		out = append(out, Collection{Name: c.Name, Description: c.Description, Assets: c.Assets})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// CreateCollection makes a new, empty collection.
func (a *App) CreateCollection(name string) (Collection, error) {
	name = slugify(name)
	if name == "" {
		return Collection{}, errors.New("give the collection a name")
	}
	store, err := a.collectionStore()
	if err != nil {
		return Collection{}, err
	}
	if err := store.SaveCollection(a.ctx, manifest.Collection{Name: name}); err != nil {
		return Collection{}, friendlyVaultError(err)
	}
	return Collection{Name: name, Assets: []string{}}, nil
}

// SetCollectionMembership adds or removes an asset from a collection.
func (a *App) SetCollectionMembership(collection, assetName string, member bool) error {
	c, err := a.findCollection(collection)
	if err != nil {
		return err
	}
	assets := make([]string, 0, len(c.Assets)+1)
	for _, existing := range c.Assets {
		if existing != assetName {
			assets = append(assets, existing)
		}
	}
	if member {
		assets = append(assets, assetName)
	}
	c.Assets = assets
	store, err := a.collectionStore()
	if err != nil {
		return err
	}
	if err := store.SaveCollection(a.ctx, c); err != nil {
		return friendlyVaultError(err)
	}
	return nil
}

// DeleteCollection removes a collection (assets stay in the library).
func (a *App) DeleteCollection(name string) error {
	store, err := a.collectionStore()
	if err != nil {
		return err
	}
	if err := store.DeleteCollection(a.ctx, name); err != nil {
		return friendlyVaultError(err)
	}
	return nil
}
