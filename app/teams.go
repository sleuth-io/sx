package main

import (
	"context"
	"errors"
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
}

// teamManager is the slice of the vault interface team operations need.
type teamManager interface {
	ListTeams(ctx context.Context, opts vaultpkg.ListTeamsOptions) (*vaultpkg.ListTeamsResult, error)
	CreateTeam(ctx context.Context, team mgmt.Team) error
	AddTeamMember(ctx context.Context, team, email string, admin bool) error
	RemoveTeamMember(ctx context.Context, team, email string) error
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
			Name:        t.Name,
			Description: t.Description,
			Members:     append([]string{}, t.Members...),
			Admins:      append([]string{}, t.Admins...),
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
