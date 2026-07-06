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

// Teams group people so assets can be shared with exactly the right set of
// teammates. The bridge reuses the same vault team management the CLI's
// `sx team` commands use.

// TeamInfo is the frontend view of a team.
type TeamInfo struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Members     []string `json:"members"`
	Admins      []string `json:"admins"`
	// Repositories drive team-scope resolution: team-scoped assets flatten
	// to these repos at install time (empty = members get them globally).
	Repositories []string `json:"repositories"`
}

// teamManager is the slice of the vault interface team operations need.
type teamManager interface {
	ListTeams(ctx context.Context, opts vaultpkg.ListTeamsOptions) (*vaultpkg.ListTeamsResult, error)
	CreateTeam(ctx context.Context, team mgmt.Team) error
	AddTeamMember(ctx context.Context, team, email string, admin bool) error
	RemoveTeamMember(ctx context.Context, team, email string) error
	SetTeamAdmin(ctx context.Context, team, email string, admin bool) error
}

func (a *App) teamVault() (teamManager, error) {
	v, err := a.currentVault()
	if err != nil {
		return nil, err
	}
	tm, ok := v.(teamManager)
	if !ok {
		return nil, errors.New("this library doesn't support teams")
	}
	return tm, nil
}

// ListTeams returns the vault's teams with members.
func (a *App) ListTeams() ([]TeamInfo, error) {
	tm, err := a.teamVault()
	if err != nil {
		return nil, err
	}
	result, err := tm.ListTeams(a.ctx, vaultpkg.ListTeamsOptions{Limit: 200})
	if err != nil {
		return nil, friendlyVaultError(err)
	}
	out := make([]TeamInfo, 0, len(result.Teams))
	for _, t := range result.Teams {
		out = append(out, TeamInfo{
			Name:         t.Name,
			Description:  t.Description,
			Members:      append([]string{}, t.Members...),
			Admins:       append([]string{}, t.Admins...),
			Repositories: append([]string{}, t.Repositories...),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// CreateTeam makes a new team with the current user as its first member and
// admin (every team needs at least one admin).
func (a *App) CreateTeam(name string) (TeamInfo, error) {
	name = slugify(name)
	if name == "" {
		return TeamInfo{}, errors.New("give the team a name")
	}
	tm, err := a.teamVault()
	if err != nil {
		return TeamInfo{}, err
	}
	self := strings.TrimSpace(a.GetVaultInfo().Identity)
	if self == "" {
		return TeamInfo{}, errors.New("set your email in Settings first — a team needs an admin")
	}
	team := mgmt.Team{
		Name:    name,
		Members: []string{self},
		Admins:  []string{self},
	}
	if err := tm.CreateTeam(a.ctx, team); err != nil {
		return TeamInfo{}, friendlyVaultError(err)
	}
	return TeamInfo{Name: name, Members: []string{self}, Admins: []string{self}}, nil
}

// AddTeamMember adds an email to a team (optionally as admin).
func (a *App) AddTeamMember(team, email string, admin bool) error {
	email = manifest.NormalizeEmail(email)
	if email == "" || !strings.Contains(email, "@") {
		return errors.New("enter the teammate's email")
	}
	tm, err := a.teamVault()
	if err != nil {
		return err
	}
	if err := tm.AddTeamMember(a.ctx, team, email, admin); err != nil {
		return friendlyVaultError(err)
	}
	return nil
}

// SetTeamAdmin grants or revokes a member's team-admin role.
func (a *App) SetTeamAdmin(team, email string, admin bool) error {
	tm, err := a.teamVault()
	if err != nil {
		return err
	}
	if err := tm.SetTeamAdmin(a.ctx, team, email, admin); err != nil {
		return friendlyVaultError(err)
	}
	return nil
}

// RemoveTeamMember removes an email from a team.
func (a *App) RemoveTeamMember(team, email string) error {
	tm, err := a.teamVault()
	if err != nil {
		return err
	}
	if err := tm.RemoveTeamMember(a.ctx, team, email); err != nil {
		return friendlyVaultError(err)
	}
	return nil
}

// teamRepoManager is the vault capability behind the team-repositories
// editor (Settings gates it on repository tracking).
type teamRepoManager interface {
	AddTeamRepository(ctx context.Context, team, repoURL string) error
	RemoveTeamRepository(ctx context.Context, team, repoURL string) error
}

// SetTeamRepository adds or removes one repository on a team. Team scopes
// flatten to these repositories at install time.
func (a *App) SetTeamRepository(team, repoURL string, member bool) error {
	repoURL = strings.TrimSpace(repoURL)
	if repoURL == "" {
		return errors.New("enter a repository URL")
	}
	v, err := a.currentVault()
	if err != nil {
		return err
	}
	m, ok := v.(teamRepoManager)
	if !ok {
		return errors.New("this library doesn't support team repositories")
	}
	if member {
		err = m.AddTeamRepository(a.ctx, team, repoURL)
	} else {
		err = m.RemoveTeamRepository(a.ctx, team, repoURL)
	}
	if err != nil {
		return friendlyVaultError(err)
	}
	return nil
}

// AddAssetRepoScope scopes an asset to a repository (the sidebar
// drag-onto-repo gesture): it installs into that repo's checkouts.
func (a *App) AddAssetRepoScope(name, repoURL string) error {
	if err := validateAssetRef(name, ""); err != nil {
		return err
	}
	r, err := a.sharingVault()
	if err != nil {
		return err
	}
	target := vaultpkg.InstallTarget{Kind: vaultpkg.InstallKindRepo, Repo: repoURL}
	if err := r.SetAssetInstallation(a.ctx, name, target); err != nil {
		return friendlyVaultError(err)
	}
	return nil
}

// AddCollectionRepoScope scopes every asset in a collection to a
// repository (dragging a collection onto a repo row). Continues past
// per-asset failures and reports them together.
func (a *App) AddCollectionRepoScope(collection, repoURL string) error {
	c, err := a.findCollection(collection)
	if err != nil {
		return err
	}
	if len(c.Assets) == 0 {
		return fmt.Errorf("%s has no assets yet", collection)
	}
	r, err := a.sharingVault()
	if err != nil {
		return err
	}
	target := vaultpkg.InstallTarget{Kind: vaultpkg.InstallKindRepo, Repo: repoURL}
	var failed []string
	for _, assetName := range c.Assets {
		if err := r.SetAssetInstallation(a.ctx, assetName, target); err != nil {
			failed = append(failed, assetName)
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("some assets could not be scoped: %s", strings.Join(failed, ", "))
	}
	return nil
}

// RenameTeam renames a team; team scopes and bot memberships follow.
func (a *App) RenameTeam(oldName, newName string) error {
	v, err := a.currentVault()
	if err != nil {
		return err
	}
	r, ok := v.(vaultpkg.TeamRenamer)
	if !ok {
		return errors.New("this library doesn't support renaming teams")
	}
	if err := r.RenameTeam(a.ctx, oldName, strings.TrimSpace(newName)); err != nil {
		return friendlyVaultError(err)
	}
	return nil
}

// RenameCollection renames a collection; membership follows.
func (a *App) RenameCollection(oldName, newName string) error {
	v, err := a.currentVault()
	if err != nil {
		return err
	}
	r, ok := v.(vaultpkg.CollectionRenamer)
	if !ok {
		return errors.New("this library doesn't support renaming collections")
	}
	if err := r.RenameCollection(a.ctx, oldName, strings.TrimSpace(newName)); err != nil {
		return friendlyVaultError(err)
	}
	return nil
}

// TeamAssets maps team name → asset names shared with that team, for the
// sidebar counts and the team detail view. Vaults that can't report this
// return an empty map rather than an error.
func (a *App) TeamAssets() (map[string][]string, error) {
	v, err := a.currentVault()
	if err != nil {
		return nil, err
	}
	lister, ok := v.(vaultpkg.TeamAssetLister)
	if !ok {
		return map[string][]string{}, nil
	}
	out, err := lister.ListTeamAssets(a.ctx)
	if err != nil {
		return nil, friendlyVaultError(err)
	}
	if out == nil {
		out = map[string][]string{}
	}
	for team := range out {
		sort.Strings(out[team])
	}
	return out, nil
}

// AssetSharing reports who an asset is currently shared with.
type AssetSharing struct {
	// Everyone is true when the asset has no scopes — the whole library
	// receives it.
	Everyone bool     `json:"everyone"`
	Teams    []string `json:"teams"`
	// Other counts scopes the app doesn't manage (repos, paths, users,
	// bots) so the UI can hint they exist without editing them.
	Other int `json:"other"`
}

type installTargetReader interface {
	CurrentInstallTargets(ctx context.Context, name string) ([]vaultpkg.InstallTarget, bool, error)
	SetAssetInstallation(ctx context.Context, assetName string, target vaultpkg.InstallTarget) error
	RemoveAssetInstallation(ctx context.Context, assetName string, target vaultpkg.InstallTarget) error
}

func (a *App) sharingVault() (installTargetReader, error) {
	v, err := a.currentVault()
	if err != nil {
		return nil, err
	}
	r, ok := v.(installTargetReader)
	if !ok {
		return nil, errors.New("this library doesn't support sharing controls")
	}
	return r, nil
}

// GetAssetSharing reports an asset's current sharing.
func (a *App) GetAssetSharing(name string) (AssetSharing, error) {
	r, err := a.sharingVault()
	if err != nil {
		return AssetSharing{}, err
	}
	targets, present, err := r.CurrentInstallTargets(a.ctx, name)
	if err != nil {
		return AssetSharing{}, friendlyVaultError(err)
	}
	sharing := AssetSharing{Teams: []string{}}
	if !present || len(targets) == 0 {
		sharing.Everyone = true
		return sharing, nil
	}
	for _, t := range targets {
		if t.Kind == vaultpkg.InstallKindTeam {
			sharing.Teams = append(sharing.Teams, t.Team)
		} else {
			sharing.Other++
		}
	}
	sort.Strings(sharing.Teams)
	return sharing, nil
}

// SetAssetTeamSharing shares an asset with a team, or stops sharing it.
func (a *App) SetAssetTeamSharing(name, team string, shared bool) error {
	r, err := a.sharingVault()
	if err != nil {
		return err
	}
	target := vaultpkg.InstallTarget{Kind: vaultpkg.InstallKindTeam, Team: team}
	if shared {
		err = r.SetAssetInstallation(a.ctx, name, target)
	} else {
		err = r.RemoveAssetInstallation(a.ctx, name, target)
	}
	if err != nil {
		return friendlyVaultError(err)
	}
	return nil
}

// ShareAssetWithEveryone returns an asset to library-wide sharing.
func (a *App) ShareAssetWithEveryone(name string) error {
	r, err := a.sharingVault()
	if err != nil {
		return err
	}
	if err := r.SetAssetInstallation(a.ctx, name, vaultpkg.InstallTarget{Kind: vaultpkg.InstallKindOrg}); err != nil {
		return friendlyVaultError(err)
	}
	return nil
}

// GetCollectionSharing reports who receives the whole collection: Everyone
// when every asset is library-wide, Teams that receive ALL of its assets.
// A collection has no scope of its own — sharing is applied per asset.
func (a *App) GetCollectionSharing(name string) (AssetSharing, error) {
	c, err := a.findCollection(name)
	if err != nil {
		return AssetSharing{}, err
	}
	if len(c.Assets) == 0 {
		return AssetSharing{Everyone: true, Teams: []string{}}, nil
	}
	everyone := true
	var common map[string]bool
	for _, assetName := range c.Assets {
		sharing, err := a.GetAssetSharing(assetName)
		if err != nil {
			return AssetSharing{}, err
		}
		if !sharing.Everyone {
			everyone = false
		}
		teams := make(map[string]bool, len(sharing.Teams))
		for _, t := range sharing.Teams {
			teams[t] = true
		}
		if common == nil {
			common = teams
			continue
		}
		for t := range common {
			if !teams[t] {
				delete(common, t)
			}
		}
	}
	out := AssetSharing{Everyone: everyone, Teams: []string{}}
	for t := range common {
		out.Teams = append(out.Teams, t)
	}
	sort.Strings(out.Teams)
	return out, nil
}

// SetCollectionTeamSharing shares (or stops sharing) every asset in a
// collection with a team. Best-effort per asset: one failure doesn't stop
// the rest, and the error names the assets that weren't updated.
func (a *App) SetCollectionTeamSharing(name, team string, shared bool) error {
	c, err := a.findCollection(name)
	if err != nil {
		return err
	}
	var failed []string
	for _, assetName := range c.Assets {
		if err := a.SetAssetTeamSharing(assetName, team, shared); err != nil {
			failed = append(failed, assetName)
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("sharing not updated for: %s", strings.Join(failed, ", "))
	}
	return nil
}

// ShareCollectionWithEveryone returns every asset in a collection to
// library-wide sharing. Best-effort per asset, like SetCollectionTeamSharing.
func (a *App) ShareCollectionWithEveryone(name string) error {
	c, err := a.findCollection(name)
	if err != nil {
		return err
	}
	var failed []string
	for _, assetName := range c.Assets {
		if err := a.ShareAssetWithEveryone(assetName); err != nil {
			failed = append(failed, assetName)
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("sharing not updated for: %s", strings.Join(failed, ", "))
	}
	return nil
}
