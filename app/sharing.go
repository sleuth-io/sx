package main

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/sleuth-io/sx/internal/manifest"
	"github.com/sleuth-io/sx/internal/mgmt"
	vaultpkg "github.com/sleuth-io/sx/internal/vault"
)

// The Manage Installations dialog shows EVERY install row on an asset or
// collection — org, repo, path, team, user, bot — not just the kinds the
// app can create. Scopes written by the CLI or a skills.new server must
// read back faithfully here and be removable, with the vault's RBAC as
// the gate. Every kind except path is addable from the app too; path
// installs (repo + subpaths, the advanced monorepo case) stay CLI-only —
// installationToTarget still validates them so removal of existing path
// rows works.

// AssetInstallation is one install row as the frontend shows and edits
// it. Kind is the manifest scope kind; only the fields for that kind are
// set. EntityID/MonoRepoConfigID ride along from server-read rows so a
// removal can target the exact installation on skills.new vaults.
type AssetInstallation struct {
	Kind             string   `json:"kind"` // org|repo|path|team|user|bot
	Repo             string   `json:"repo,omitempty"`
	Paths            []string `json:"paths,omitempty"`
	Team             string   `json:"team,omitempty"`
	User             string   `json:"user,omitempty"`
	Bot              string   `json:"bot,omitempty"`
	EntityID         string   `json:"entityId,omitempty"`
	MonoRepoConfigID string   `json:"monoRepoConfigId,omitempty"`
}

// InstallationsView is what the dialog renders: Everyone means no rows
// exist and the whole library receives it (the default state).
type InstallationsView struct {
	Everyone      bool                `json:"everyone"`
	Installations []AssetInstallation `json:"installations"`
}

func installationFromTarget(t vaultpkg.InstallTarget) AssetInstallation {
	return AssetInstallation{
		Kind:             string(t.Kind),
		Repo:             t.Repo,
		Paths:            t.Paths,
		Team:             t.Team,
		User:             t.User,
		Bot:              t.Bot,
		EntityID:         t.EntityID,
		MonoRepoConfigID: t.MonoRepoConfigID,
	}
}

// installationToTarget validates a frontend row into a vault target. A
// user row with an empty email means the caller ("Personal" in the UI).
func (a *App) installationToTarget(inst AssetInstallation) (vaultpkg.InstallTarget, error) {
	t := vaultpkg.InstallTarget{
		Kind:             vaultpkg.InstallKind(inst.Kind),
		Repo:             strings.TrimSpace(inst.Repo),
		Paths:            inst.Paths,
		Team:             strings.TrimSpace(inst.Team),
		User:             manifest.NormalizeEmail(strings.TrimSpace(inst.User)),
		Bot:              strings.TrimSpace(inst.Bot),
		EntityID:         inst.EntityID,
		MonoRepoConfigID: inst.MonoRepoConfigID,
	}
	switch t.Kind {
	case vaultpkg.InstallKindOrg:
	case vaultpkg.InstallKindRepo:
		if t.Repo == "" {
			return t, errors.New("pick or enter a repository")
		}
	case vaultpkg.InstallKindPath:
		if t.Repo == "" || len(t.Paths) == 0 {
			return t, errors.New("a path install needs a repository and at least one path")
		}
	case vaultpkg.InstallKindTeam:
		if t.Team == "" {
			return t, errors.New("pick a team")
		}
	case vaultpkg.InstallKindUser:
		if t.User == "" {
			self := manifest.NormalizeEmail(strings.TrimSpace(a.GetVaultInfo().Identity))
			if self == "" {
				return t, errors.New("set your email in Settings first — personal installs are scoped to you")
			}
			t.User = self
		}
	case vaultpkg.InstallKindBot:
		if t.Bot == "" {
			return t, errors.New("pick a bot")
		}
	default:
		return t, fmt.Errorf("unknown installation kind %q", inst.Kind)
	}
	return t, nil
}

// GetAssetInstallations reports every install row on an asset.
func (a *App) GetAssetInstallations(name string) (InstallationsView, error) {
	r, err := a.sharingVault()
	if err != nil {
		return InstallationsView{}, err
	}
	targets, present, err := r.CurrentInstallTargets(a.ctx, name)
	if err != nil {
		return InstallationsView{}, friendlyVaultError(err)
	}
	return installationsView(targets, present), nil
}

// AddAssetInstallation adds one install row. The vault's RBAC gate
// (docs/rbac.md) decides whether the caller may — errors surface as-is.
func (a *App) AddAssetInstallation(name string, inst AssetInstallation) error {
	if err := validateAssetRef(name, ""); err != nil {
		return err
	}
	target, err := a.installationToTarget(inst)
	if err != nil {
		return err
	}
	r, err := a.sharingVault()
	if err != nil {
		return err
	}
	if err := r.SetAssetInstallation(a.ctx, name, target); err != nil {
		return friendlyVaultError(err)
	}
	return nil
}

// RemoveAssetInstallationRow removes one install row.
func (a *App) RemoveAssetInstallationRow(name string, inst AssetInstallation) error {
	if err := validateAssetRef(name, ""); err != nil {
		return err
	}
	target, err := a.installationToTarget(inst)
	if err != nil {
		return err
	}
	r, err := a.sharingVault()
	if err != nil {
		return err
	}
	if err := r.RemoveAssetInstallation(a.ctx, name, target); err != nil {
		return friendlyVaultError(err)
	}
	return nil
}

// GetCollectionInstallations reports every install row on a collection.
func (a *App) GetCollectionInstallations(name string) (InstallationsView, error) {
	if _, err := a.findCollection(name); err != nil {
		return InstallationsView{}, err
	}
	r, err := a.collectionInstallVault()
	if err != nil {
		return InstallationsView{}, err
	}
	targets, present, err := r.CurrentCollectionInstallTargets(a.ctx, name)
	if err != nil {
		return InstallationsView{}, friendlyVaultError(err)
	}
	return installationsView(targets, present), nil
}

// AddCollectionInstallation adds one install row to a collection, with
// the same narrowing semantics assets get from the vault layer: an org
// row REPLACES the other rows, and a narrower row replaces an org row.
// Asset org installs clear scopes in the vault; collection org installs
// are an explicit row that would coexist, so both replacements are done
// here — set the new row first, then drop the contradicted rows
// best-effort.
func (a *App) AddCollectionInstallation(name string, inst AssetInstallation) error {
	if _, err := a.findCollection(name); err != nil {
		return err
	}
	target, err := a.installationToTarget(inst)
	if err != nil {
		return err
	}
	r, err := a.collectionInstallVault()
	if err != nil {
		return err
	}
	previous, _, err := r.CurrentCollectionInstallTargets(a.ctx, name)
	if err != nil {
		return friendlyVaultError(err)
	}
	if err := r.SetCollectionInstallation(a.ctx, name, target); err != nil {
		return friendlyVaultError(err)
	}
	var failed int
	for _, t := range previous {
		contradicted := t.Kind != vaultpkg.InstallKindOrg
		if target.Kind != vaultpkg.InstallKindOrg {
			// Narrow add: only an org row contradicts it — everyone
			// would keep receiving the collection despite the narrower
			// intent.
			contradicted = t.Kind == vaultpkg.InstallKindOrg
		}
		if !contradicted {
			continue
		}
		if err := r.RemoveCollectionInstallation(a.ctx, name, t); err != nil {
			failed++
		}
	}
	if failed > 0 {
		return fmt.Errorf("installed, but %d contradicted row(s) could not be removed", failed)
	}
	return nil
}

// RemoveCollectionInstallationRow removes one install row from a
// collection.
func (a *App) RemoveCollectionInstallationRow(name string, inst AssetInstallation) error {
	if _, err := a.findCollection(name); err != nil {
		return err
	}
	target, err := a.installationToTarget(inst)
	if err != nil {
		return err
	}
	r, err := a.collectionInstallVault()
	if err != nil {
		return err
	}
	if err := r.RemoveCollectionInstallation(a.ctx, name, target); err != nil {
		return friendlyVaultError(err)
	}
	return nil
}

func installationsView(targets []vaultpkg.InstallTarget, present bool) InstallationsView {
	view := InstallationsView{Installations: []AssetInstallation{}}
	if !present || len(targets) == 0 {
		view.Everyone = true
		return view
	}
	for _, t := range targets {
		view.Installations = append(view.Installations, installationFromTarget(t))
	}
	// Stable order for the dialog: grouped by kind, then by name.
	kindRank := map[string]int{"org": 0, "team": 1, "repo": 2, "path": 3, "user": 4, "bot": 5}
	sort.SliceStable(view.Installations, func(i, j int) bool {
		a, b := view.Installations[i], view.Installations[j]
		if a.Kind != b.Kind {
			return kindRank[a.Kind] < kindRank[b.Kind]
		}
		return a.Repo+a.Team+a.User+a.Bot < b.Repo+b.Team+b.User+b.Bot
	})
	return view
}

// ListVaultBots returns the vault's bot names, for the Bot picker.
// Vaults without bots (or without the capability) list none.
func (a *App) ListVaultBots() ([]string, error) {
	out := []string{}
	v, err := a.currentVault()
	if err != nil {
		return out, nil
	}
	lister, ok := v.(interface {
		ListBots(ctx context.Context) ([]mgmt.Bot, error)
	})
	if !ok {
		return out, nil
	}
	bots, err := lister.ListBots(a.ctx)
	if err != nil {
		return out, nil
	}
	for _, b := range bots {
		out = append(out, b.Name)
	}
	sort.Strings(out)
	return out, nil
}

// ListKnownRepos returns repository URLs the vault already knows about —
// team repositories plus repos already carrying install scopes — as
// suggestions for the Repo picker. The dialog hides the Repo option
// entirely when this is empty (the app stays simple until repositories
// actually exist); free-text URLs are still accepted by
// AddAssetInstallation when it shows.
func (a *App) ListKnownRepos() ([]string, error) {
	seen := map[string]bool{}
	out := []string{}
	add := func(r string) {
		if r = strings.TrimSpace(r); r != "" && !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	if teams, err := a.ListTeams(); err == nil {
		for _, t := range teams {
			for _, r := range t.Repositories {
				add(r)
			}
		}
	}
	if repoAssets, err := a.RepoAssets(); err == nil {
		for r := range repoAssets {
			add(r)
		}
	}
	sort.Strings(out)
	return out, nil
}
