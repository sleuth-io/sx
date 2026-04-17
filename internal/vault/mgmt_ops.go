package vault

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/sleuth-io/sx/internal/manifest"
	"github.com/sleuth-io/sx/internal/mgmt"
	"github.com/sleuth-io/sx/internal/scope"
)

// ensureSxDir creates the .sx/ directory under the vault root if needed.
// File locks and the append-only usage/audit JSONL streams live there.
func ensureSxDir(vaultRoot string) error {
	return os.MkdirAll(filepath.Join(vaultRoot, ".sx"), 0755)
}

// loadManifest reads sx.toml at vaultRoot. If only legacy files exist it
// migrates them once, writes sx.toml, and returns the result.
func loadManifest(vaultRoot string) (*manifest.Manifest, error) {
	m, _, err := manifest.LoadOrMigrate(vaultRoot)
	return m, err
}

// withManifest is the transactional wrapper for every manifest mutation.
// fn mutates the manifest in place; the wrapper saves it and appends a
// single audit event on success. A nil *mgmt.AuditEvent from fn means
// "skip audit" (used by no-op paths). fn may return an error to abort.
//
// Atomicity contract: the manifest write and the audit append are
// separate operations. If Save succeeds but AppendAuditEvent fails (disk
// full, permission error, etc.) the mutation is durable but unaudited.
// This is intentional — an already-committed state change cannot be
// rolled back because its audit line failed, and the alternative (roll
// back on audit failure) would be far more disruptive. The vault flock
// keeps concurrent writers from racing; lost audit lines are treated as
// an operational alarm, not a bug.
func withManifest(vaultRoot string, actor mgmt.Actor, fn func(*manifest.Manifest) (*mgmt.AuditEvent, error)) error {
	if err := actor.RequireRealIdentity(); err != nil {
		return fmt.Errorf("vault mutations require a real git identity (set git config user.email): %w", err)
	}
	m, err := loadManifest(vaultRoot)
	if err != nil {
		return err
	}
	event, err := fn(m)
	if err != nil {
		return err
	}
	if err := manifest.Save(vaultRoot, m); err != nil {
		return err
	}
	if event == nil {
		return nil
	}
	event.Actor = actor.Email
	return mgmt.AppendAuditEvent(vaultRoot, *event)
}

// requireTeamAdminInTx looks up teamName in the in-transaction manifest
// and returns an error unless actor is an admin. Called at the top of
// every admin-gated closure so the check happens after the flock + reload
// — the CLI-level pre-check in team.go is only a fast-fail for UX, not a
// security boundary.
func requireTeamAdminInTx(m *manifest.Manifest, teamName string, actor mgmt.Actor) (*manifest.Team, error) {
	team, err := m.FindTeam(teamName)
	if err != nil {
		return nil, err
	}
	if !team.IsAdmin(actor.Email) {
		return nil, fmt.Errorf("%s is not an admin of team %s", actor.Email, teamName)
	}
	return team, nil
}

// commonListTeams returns a copy of every team in the vault's manifest.
func commonListTeams(vaultRoot string) ([]mgmt.Team, error) {
	m, err := loadManifest(vaultRoot)
	if err != nil {
		return nil, err
	}
	out := make([]mgmt.Team, len(m.Teams))
	for i, t := range m.Teams {
		out[i] = manifestTeamToMgmt(t)
	}
	return out, nil
}

// commonGetTeam returns a single team by name.
func commonGetTeam(vaultRoot, name string) (*mgmt.Team, error) {
	m, err := loadManifest(vaultRoot)
	if err != nil {
		return nil, err
	}
	team, err := m.FindTeam(name)
	if err != nil {
		return nil, err
	}
	out := manifestTeamToMgmt(*team)
	return &out, nil
}

// commonCreateTeam adds a new team. The caller is always added to both
// Members and Admins so every team has at least one admin — prevents the
// "delete all admins then take over" attack.
func commonCreateTeam(vaultRoot string, actor mgmt.Actor, team mgmt.Team) error {
	return withManifest(vaultRoot, actor, func(m *manifest.Manifest) (*mgmt.AuditEvent, error) {
		if _, err := m.FindTeam(team.Name); err == nil {
			return nil, fmt.Errorf("%w: %s", manifest.ErrTeamExists, team.Name)
		}
		mt := mgmtTeamToManifest(team)
		if !slices.Contains(mt.Members, actor.Email) {
			mt.Members = append(mt.Members, actor.Email)
		}
		if !slices.Contains(mt.Admins, actor.Email) {
			mt.Admins = append(mt.Admins, actor.Email)
		}
		if _, err := m.UpsertTeam(mt); err != nil {
			return nil, err
		}
		return &mgmt.AuditEvent{
			Event:      mgmt.EventTeamCreated,
			TargetType: mgmt.TargetTypeTeam,
			Target:     team.Name,
			Data: map[string]any{
				"description":  mt.Description,
				"members":      mt.Members,
				"admins":       mt.Admins,
				"repositories": mt.Repositories,
			},
		}, nil
	})
}

// commonUpdateTeam replaces a team's description/members/admins/repos with
// the values from the input. Team must already exist.
func commonUpdateTeam(vaultRoot string, actor mgmt.Actor, team mgmt.Team) error {
	return withManifest(vaultRoot, actor, func(m *manifest.Manifest) (*mgmt.AuditEvent, error) {
		if _, err := requireTeamAdminInTx(m, team.Name, actor); err != nil {
			return nil, err
		}
		if _, err := m.UpsertTeam(mgmtTeamToManifest(team)); err != nil {
			return nil, err
		}
		return &mgmt.AuditEvent{
			Event:      mgmt.EventTeamUpdated,
			TargetType: mgmt.TargetTypeTeam,
			Target:     team.Name,
		}, nil
	})
}

// commonDeleteTeam removes a team and cascades to drop any team-scoped
// asset installations that targeted it. Cascade happens before the team
// deletion so a crash between the two writes leaves a recoverable state:
// the team still exists (retry the delete and the same ordering will
// complete the cleanup), and no orphan team-scoped entries survive to be
// silently inherited by a future team re-created under the same name.
// Since both writes go to the same sx.toml file they are actually atomic
// here, but ordering is kept for defense in depth.
func commonDeleteTeam(vaultRoot string, actor mgmt.Actor, name string) error {
	var clearedAssets []string
	if err := withManifest(vaultRoot, actor, func(m *manifest.Manifest) (*mgmt.AuditEvent, error) {
		if _, err := requireTeamAdminInTx(m, name, actor); err != nil {
			return nil, err
		}
		for i := range m.Assets {
			asset := &m.Assets[i]
			kept := asset.Scopes[:0]
			removed := false
			for _, s := range asset.Scopes {
				if s.Kind == manifest.ScopeKindTeam && s.Team == name {
					removed = true
					continue
				}
				kept = append(kept, s)
			}
			if removed {
				asset.Scopes = kept
				clearedAssets = append(clearedAssets, asset.Name)
			}
		}
		if err := m.DeleteTeam(name); err != nil {
			return nil, err
		}
		return &mgmt.AuditEvent{
			Event:      mgmt.EventTeamDeleted,
			TargetType: mgmt.TargetTypeTeam,
			Target:     name,
		}, nil
	}); err != nil {
		return err
	}

	// Emit per-asset install.cleared audit events so auditors can
	// reconstruct "why did asset X stop installing for team Y". Best
	// effort: the durable mutation is already on disk.
	for _, assetName := range clearedAssets {
		if err := mgmt.AppendAuditEvent(vaultRoot, mgmt.AuditEvent{
			Actor:      actor.Email,
			Event:      mgmt.EventInstallCleared,
			TargetType: mgmt.TargetTypeInstallation,
			Target:     assetName,
			Data: map[string]any{
				"kind":   string(manifest.ScopeKindTeam),
				"team":   name,
				"reason": "team_deleted",
			},
		}); err != nil {
			return err
		}
	}
	return nil
}

// commonAddTeamMember adds an email to a team's member list. If admin is
// true, the member is also added to the admin list. A no-op call
// (member and admin flags already match the current state) returns
// without mutating the manifest or emitting an audit event so repeated
// idempotent retries don't pollute the audit log.
func commonAddTeamMember(vaultRoot string, actor mgmt.Actor, teamName, email string, admin bool) error {
	return withManifest(vaultRoot, actor, func(m *manifest.Manifest) (*mgmt.AuditEvent, error) {
		team, err := requireTeamAdminInTx(m, teamName, actor)
		if err != nil {
			return nil, err
		}
		normalized := manifest.NormalizeEmail(email)
		if normalized == "" {
			return nil, errors.New("cannot add empty email")
		}
		memberAdded := false
		if !slices.Contains(team.Members, normalized) {
			team.Members = append(team.Members, normalized)
			memberAdded = true
		}
		adminAdded := false
		if admin && !slices.Contains(team.Admins, normalized) {
			team.Admins = append(team.Admins, normalized)
			adminAdded = true
		}
		if !memberAdded && !adminAdded {
			return nil, nil
		}
		if _, err := m.UpsertTeam(*team); err != nil {
			return nil, err
		}
		return &mgmt.AuditEvent{
			Event:      mgmt.EventTeamMemberAdded,
			TargetType: mgmt.TargetTypeTeam,
			Target:     teamName,
			Data:       map[string]any{"member": normalized, "admin": admin},
		}, nil
	})
}

// commonRemoveTeamMember removes an email from a team (both members and
// admins lists). Errors if removal would leave the team without any
// admin.
func commonRemoveTeamMember(vaultRoot string, actor mgmt.Actor, teamName, email string) error {
	return withManifest(vaultRoot, actor, func(m *manifest.Manifest) (*mgmt.AuditEvent, error) {
		team, err := requireTeamAdminInTx(m, teamName, actor)
		if err != nil {
			return nil, err
		}
		normalized := manifest.NormalizeEmail(email)
		team.Members = removeString(team.Members, normalized)
		team.Admins = removeString(team.Admins, normalized)
		if len(team.Admins) == 0 {
			return nil, fmt.Errorf("%w: cannot remove %s (last admin of team %s) — add another admin first", mgmt.ErrLastAdmin, normalized, teamName)
		}
		if _, err := m.UpsertTeam(*team); err != nil {
			return nil, err
		}
		return &mgmt.AuditEvent{
			Event:      mgmt.EventTeamMemberRemoved,
			TargetType: mgmt.TargetTypeTeam,
			Target:     teamName,
			Data:       map[string]any{"member": normalized},
		}, nil
	})
}

// commonSetTeamAdmin grants or revokes admin privileges for a member.
// Returns without mutating or emitting an audit event when the request
// matches the current state.
func commonSetTeamAdmin(vaultRoot string, actor mgmt.Actor, teamName, email string, admin bool) error {
	return withManifest(vaultRoot, actor, func(m *manifest.Manifest) (*mgmt.AuditEvent, error) {
		team, err := requireTeamAdminInTx(m, teamName, actor)
		if err != nil {
			return nil, err
		}
		normalized := manifest.NormalizeEmail(email)
		if !team.IsMember(normalized) {
			return nil, fmt.Errorf("%s is not a member of team %s", normalized, teamName)
		}
		event := mgmt.EventTeamAdminSet
		alreadyAdmin := slices.Contains(team.Admins, normalized)
		if admin {
			if alreadyAdmin {
				return nil, nil
			}
			team.Admins = append(team.Admins, normalized)
		} else {
			if !alreadyAdmin {
				return nil, nil
			}
			team.Admins = removeString(team.Admins, normalized)
			event = mgmt.EventTeamAdminUnset
			if len(team.Admins) == 0 {
				return nil, fmt.Errorf("%w: cannot revoke admin from %s (last admin of team %s) — grant another admin first", mgmt.ErrLastAdmin, normalized, teamName)
			}
		}
		if _, err := m.UpsertTeam(*team); err != nil {
			return nil, err
		}
		return &mgmt.AuditEvent{
			Event:      event,
			TargetType: mgmt.TargetTypeTeam,
			Target:     teamName,
			Data:       map[string]any{"member": normalized},
		}, nil
	})
}

// commonAddTeamRepository adds a repository URL to a team. A no-op
// (URL already present) returns without mutating or emitting an audit
// event.
func commonAddTeamRepository(vaultRoot string, actor mgmt.Actor, teamName, repoURL string) error {
	return withManifest(vaultRoot, actor, func(m *manifest.Manifest) (*mgmt.AuditEvent, error) {
		team, err := requireTeamAdminInTx(m, teamName, actor)
		if err != nil {
			return nil, err
		}
		normalized := scope.NormalizeRepoURL(strings.TrimSpace(repoURL))
		if normalized == "" {
			return nil, errors.New("cannot add empty repository URL")
		}
		if slices.Contains(team.Repositories, normalized) {
			return nil, nil
		}
		team.Repositories = append(team.Repositories, normalized)
		if _, err := m.UpsertTeam(*team); err != nil {
			return nil, err
		}
		return &mgmt.AuditEvent{
			Event:      mgmt.EventTeamRepoAdded,
			TargetType: mgmt.TargetTypeTeam,
			Target:     teamName,
			Data:       map[string]any{"repository": normalized},
		}, nil
	})
}

// commonRemoveTeamRepository removes a repository URL from a team.
func commonRemoveTeamRepository(vaultRoot string, actor mgmt.Actor, teamName, repoURL string) error {
	return withManifest(vaultRoot, actor, func(m *manifest.Manifest) (*mgmt.AuditEvent, error) {
		team, err := requireTeamAdminInTx(m, teamName, actor)
		if err != nil {
			return nil, err
		}
		normalized := scope.NormalizeRepoURL(strings.TrimSpace(repoURL))
		team.Repositories = removeString(team.Repositories, normalized)
		if _, err := m.UpsertTeam(*team); err != nil {
			return nil, err
		}
		return &mgmt.AuditEvent{
			Event:      mgmt.EventTeamRepoRemoved,
			TargetType: mgmt.TargetTypeTeam,
			Target:     teamName,
			Data:       map[string]any{"repository": normalized},
		}, nil
	})
}

// commonSetAssetInstallation appends or replaces an install scope on the
// named asset. Org kind clears all existing scopes (asset becomes global);
// every other kind appends a scope row, deduped against existing entries.
func commonSetAssetInstallation(vaultRoot string, actor mgmt.Actor, assetName string, target InstallTarget) error {
	var s manifest.Scope

	switch target.Kind {
	case InstallKindOrg:
		// Org install clears all scopes on the asset — the asset becomes
		// globally visible. Handled below by the fn directly.
	case InstallKindRepo:
		if target.Repo == "" {
			return errors.New("repo installation missing repo URL")
		}
		s = manifest.Scope{
			Kind: manifest.ScopeKindRepo,
			Repo: scope.NormalizeRepoURL(target.Repo),
		}
	case InstallKindPath:
		if target.Repo == "" || len(target.Paths) == 0 {
			return errors.New("path installation requires repo URL and at least one path")
		}
		s = manifest.Scope{
			Kind:  manifest.ScopeKindPath,
			Repo:  scope.NormalizeRepoURL(target.Repo),
			Paths: target.Paths,
		}
	case InstallKindTeam:
		if target.Team == "" {
			return errors.New("team installation missing team name")
		}
		s = manifest.Scope{Kind: manifest.ScopeKindTeam, Team: target.Team}
	case InstallKindUser:
		if target.User == "" {
			return errors.New("user installation missing email")
		}
		// User-scoped installs may only target the caller. Any write-
		// access holder could otherwise force an asset to be "global" in
		// another user's resolved lock file via the user-match rule.
		// Mirrors the sleuth vault check in sleuth_mgmt.go to avoid a
		// silent privilege escalation.
		if manifest.NormalizeEmail(target.User) != actor.Email {
			return fmt.Errorf("user-scoped installs may only target the authenticated caller (got %q, actor %q)", target.User, actor.Email)
		}
		s = manifest.Scope{Kind: manifest.ScopeKindUser, User: target.User}
	default:
		return fmt.Errorf("unknown installation kind: %q", target.Kind)
	}

	return withManifest(vaultRoot, actor, func(m *manifest.Manifest) (*mgmt.AuditEvent, error) {
		asset := m.FindAsset(assetName)
		if asset == nil {
			return nil, fmt.Errorf("asset %q not found", assetName)
		}
		// Re-check team admin membership inside the transaction to close
		// the TOCTOU window between CLI pre-check and commit.
		if target.Kind == InstallKindTeam {
			if _, err := requireTeamAdminInTx(m, target.Team, actor); err != nil {
				return nil, err
			}
		}
		if target.Kind == InstallKindOrg {
			asset.Scopes = nil
		} else if !scopeExistsOnAsset(asset.Scopes, s) {
			asset.Scopes = append(asset.Scopes, s)
		}
		return &mgmt.AuditEvent{
			Event:      mgmt.EventInstallSet,
			TargetType: mgmt.TargetTypeInstallation,
			Target:     assetName,
			Data:       target.AuditData(),
		}, nil
	})
}

// commonClearAssetInstallations removes every scope from the named asset.
// Missing asset is a soft no-op so admins can clean up orphaned entries
// safely.
func commonClearAssetInstallations(vaultRoot string, actor mgmt.Actor, assetName string) error {
	return withManifest(vaultRoot, actor, func(m *manifest.Manifest) (*mgmt.AuditEvent, error) {
		asset := m.FindAsset(assetName)
		if asset == nil || len(asset.Scopes) == 0 {
			return nil, nil
		}
		asset.Scopes = nil
		return &mgmt.AuditEvent{
			Event:      mgmt.EventInstallCleared,
			TargetType: mgmt.TargetTypeInstallation,
			Target:     assetName,
		}, nil
	})
}

// commonRecordUsageEvents persists a batch of usage events to
// .sx/usage/YYYY-MM.jsonl, enriching each with the actor's email if the
// caller didn't set it.
func commonRecordUsageEvents(vaultRoot string, actor mgmt.Actor, events []mgmt.UsageEvent) error {
	if len(events) == 0 {
		return nil
	}
	for i := range events {
		if events[i].Actor == "" {
			events[i].Actor = actor.Email
		}
	}
	return mgmt.AppendUsageEvents(vaultRoot, events)
}

// scopeExistsOnAsset returns true when needle is already among scopes,
// after normalizing the repo URL for repo/path kinds.
func scopeExistsOnAsset(scopes []manifest.Scope, needle manifest.Scope) bool {
	needleRepo := scope.NormalizeRepoURL(needle.Repo)
	for _, s := range scopes {
		if s.Kind != needle.Kind {
			continue
		}
		switch s.Kind {
		case manifest.ScopeKindOrg:
			return true
		case manifest.ScopeKindRepo:
			if scope.NormalizeRepoURL(s.Repo) == needleRepo {
				return true
			}
		case manifest.ScopeKindPath:
			if scope.NormalizeRepoURL(s.Repo) == needleRepo && slices.Equal(s.Paths, needle.Paths) {
				return true
			}
		case manifest.ScopeKindTeam:
			if s.Team == needle.Team {
				return true
			}
		case manifest.ScopeKindUser:
			if manifest.NormalizeEmail(s.User) == manifest.NormalizeEmail(needle.User) {
				return true
			}
		}
	}
	return false
}

// manifestTeamToMgmt converts a manifest.Team to the mgmt.Team view used
// by the Vault interface. The two types carry the same fields; the helper
// keeps the boundary explicit so a future rename on either side breaks
// compilation.
func manifestTeamToMgmt(t manifest.Team) mgmt.Team {
	return mgmt.Team{
		Name:         t.Name,
		Description:  t.Description,
		Members:      append([]string(nil), t.Members...),
		Admins:       append([]string(nil), t.Admins...),
		Repositories: append([]string(nil), t.Repositories...),
	}
}

func mgmtTeamToManifest(t mgmt.Team) manifest.Team {
	return manifest.Team{
		Name:         t.Name,
		Description:  t.Description,
		Members:      append([]string(nil), t.Members...),
		Admins:       append([]string(nil), t.Admins...),
		Repositories: append([]string(nil), t.Repositories...),
	}
}

func removeString(list []string, needle string) []string {
	out := make([]string, 0, len(list))
	for _, s := range list {
		if s != needle {
			out = append(out, s)
		}
	}
	return out
}
