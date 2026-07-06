package main

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"

	assetspkg "github.com/sleuth-io/sx/internal/assets"
	"github.com/sleuth-io/sx/internal/clients"
	"github.com/sleuth-io/sx/internal/manifest"
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

func globalScope() *clients.InstallScope {
	return &clients.InstallScope{Type: clients.ScopeGlobal}
}

// SetAssetPersonal is the durable form of "Use in my AI tools": it makes
// the asset part of what sx resolves FOR THIS USER (adding a personal user
// scope in the vault when the asset doesn't already reach them), then runs
// the real sync so files land immediately and every future sync agrees.
// Disabling removes only the caller's own user scope — never anyone
// else's sharing — and explains when the asset stays because the library
// still shares it with them.
func (a *App) SetAssetPersonal(name string, enabled bool) (string, error) {
	if err := validateAssetRef(name, ""); err != nil {
		return "", err
	}
	self := manifest.NormalizeEmail(strings.TrimSpace(a.GetVaultInfo().Identity))
	if self == "" {
		return "", errors.New("set your email in Settings first — personal installs are scoped to you")
	}
	r, err := a.sharingVault()
	if err != nil {
		return "", err
	}
	targets, present, err := r.CurrentInstallTargets(a.ctx, name)
	if err != nil {
		return "", friendlyVaultError(err)
	}
	shared, minePersonally := a.assetReachesUser(targets, self)
	if !present {
		// Absent from the manifest (orphaned storage) is NOT library-wide:
		// let enable add the user scope, which also triggers the
		// SetAssetInstallation storage-recovery repair.
		shared = false
	}

	if enabled {
		// A global asset must NOT get a user scope appended — scopes are
		// "who receives this", and narrowing everyone's asset to one
		// person is the opposite of what the button means.
		if !shared && !minePersonally {
			target := vaultpkg.InstallTarget{Kind: vaultpkg.InstallKindUser, User: self}
			if err := r.SetAssetInstallation(a.ctx, name, target); err != nil {
				return "", friendlyVaultError(err)
			}
		}
		return a.SyncAITools()
	}

	if !minePersonally {
		if shared {
			return "", errors.New("this is shared with you by the library, so sync would bring it right back — change its sharing to stop receiving it")
		}
		// Nothing personal to remove and nothing shares it; sync will
		// clean up any stray local copy.
		return a.SyncAITools()
	}
	target := vaultpkg.InstallTarget{Kind: vaultpkg.InstallKindUser, User: self}
	if err := r.RemoveAssetInstallation(a.ctx, name, target); err != nil {
		return "", friendlyVaultError(err)
	}
	summary, err := a.SyncAITools()
	if err != nil {
		return "", err
	}
	if shared {
		return "Removed from your personal picks — but the library still shares it with you, so it stays installed", nil
	}
	return summary, nil
}

// assetReachesUser reports how an asset's install targets relate to the
// user: shared = it reaches them without a personal scope (org-wide, or a
// team they belong to); minePersonally = their own user scope is present.
func (a *App) assetReachesUser(targets []vaultpkg.InstallTarget, self string) (shared, minePersonally bool) {
	var teamNames []string
	for _, t := range targets {
		switch t.Kind {
		case vaultpkg.InstallKindOrg:
			shared = true
		case vaultpkg.InstallKindUser:
			if manifest.NormalizeEmail(t.User) == self {
				minePersonally = true
			}
		case vaultpkg.InstallKindTeam:
			teamNames = append(teamNames, t.Team)
		case vaultpkg.InstallKindRepo, vaultpkg.InstallKindPath, vaultpkg.InstallKindBot:
			// Repo/path scopes apply in repository contexts and bot scopes
			// to bot identities — neither reaches this user's global sync.
		}
	}
	// An asset with no targets at all is library-wide (global).
	if len(targets) == 0 {
		shared = true
	}
	if shared || len(teamNames) == 0 {
		return shared, minePersonally
	}
	teams, err := a.ListTeams()
	if err != nil {
		return shared, minePersonally
	}
	for _, team := range teams {
		if !slices.Contains(teamNames, team.Name) {
			continue
		}
		for _, member := range team.Members {
			if manifest.NormalizeEmail(member) == self {
				return true, minePersonally
			}
		}
	}
	return shared, minePersonally
}

// InstalledAssetInfo describes one asset installed on this machine, in ANY
// scope — whether the app installed it directly or `sx install` (or its
// client hooks) delivered it via an org/team/repo scope.
type InstalledAssetInfo struct {
	Name    string   `json:"name"`
	Version string   `json:"version"`
	Scopes  []string `json:"scopes"` // human-readable, e.g. "Everywhere on this machine", "github.com/acme/repo"
}

// InstalledAssets reports what is installed on this machine, from two
// sources: the shared install tracker (what `sx install` and the app
// recorded, with scope detail) UNION what the detected AI tools actually
// have on disk. The scan makes the answer survive a lost or stale tracker
// and covers assets installed outside sx.
func (a *App) InstalledAssets() ([]InstalledAssetInfo, error) {
	byName := map[string]*InstalledAssetInfo{}

	if tracker, err := assetspkg.LoadTracker(); err == nil {
		for _, item := range tracker.Assets {
			scope := "Everywhere on this machine"
			if item.Repository != "" {
				scope = item.Repository
				if item.Path != "" {
					scope += " (" + item.Path + ")"
				}
			}
			if existing, ok := byName[item.Name]; ok {
				existing.Scopes = append(existing.Scopes, scope)
				continue
			}
			byName[item.Name] = &InstalledAssetInfo{
				Name:    item.Name,
				Version: item.Version,
				Scopes:  []string{scope},
			}
		}
	}

	for _, client := range clients.Global().DetectInstalled() {
		scanned, err := client.ListAssets(a.ctx, globalScope())
		if err != nil {
			continue
		}
		for _, item := range scanned {
			if _, ok := byName[item.Name]; ok {
				continue
			}
			byName[item.Name] = &InstalledAssetInfo{
				Name:    item.Name,
				Version: item.Version,
				Scopes:  []string{"Everywhere on this machine"},
			}
		}
	}

	out := make([]InstalledAssetInfo, 0, len(byName))
	for _, info := range byName {
		sort.Strings(info.Scopes)
		out = append(out, *info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
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
		assets := c.Assets
		if assets == nil {
			assets = []string{} // nil marshals to JSON null and breaks the frontend
		}
		out = append(out, Collection{Name: c.Name, Description: c.Description, Assets: assets})
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
// collectionMembershipMu serializes membership read-modify-writes: the
// mutation spans two vault calls (read the collection, save the whole
// asset list), so concurrent callers — e.g. a bulk drop fanning out —
// would otherwise overwrite each other's additions.
var collectionMembershipMu sync.Mutex

func (a *App) SetCollectionMembership(collection, assetName string, member bool) error {
	collectionMembershipMu.Lock()
	defer collectionMembershipMu.Unlock()
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
