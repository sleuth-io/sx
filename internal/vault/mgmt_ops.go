package vault

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/sleuth-io/sx/internal/constants"
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/mgmt"
	"github.com/sleuth-io/sx/internal/scope"
)

// lockFilePathForRoot returns the skill.lock path for a vault root.
func lockFilePathForRoot(vaultRoot string) string {
	return filepath.Join(vaultRoot, constants.SkillLockFile)
}

// ensureSxDir creates the .sx/ directory under the vault root if needed.
// File locks placed inside .sx/ require the directory to exist.
func ensureSxDir(vaultRoot string) error {
	return os.MkdirAll(filepath.Join(vaultRoot, ".sx"), 0755)
}

// applyInstallationsOverlay flattens team/user installations from
// .sx/installations.toml onto the given raw skill.lock bytes. If the
// installations file is missing or empty, the raw bytes are returned
// unchanged (fast path — zero overhead for vaults that never use the new
// management features).
func applyInstallationsOverlay(ctx context.Context, vaultRoot string, rawLockFile []byte) ([]byte, error) {
	ifile, ok, err := mgmt.LoadInstallations(vaultRoot)
	if err != nil {
		return nil, err
	}
	if !ok || ifile == nil || len(ifile.Installations) == 0 {
		return rawLockFile, nil
	}

	teams, err := mgmt.LoadTeams(vaultRoot)
	if err != nil {
		return nil, err
	}
	actor, err := mgmt.CurrentGitActor(ctx, vaultRoot)
	if err != nil {
		return nil, err
	}

	lf, err := lockfile.Parse(rawLockFile)
	if err != nil {
		return nil, fmt.Errorf("failed to parse lock file for overlay: %w", err)
	}
	overlaid := mgmt.OverlayInstallations(lf, teams, ifile, actor)
	return lockfile.Marshal(overlaid)
}

// withTeams is the transactional wrapper shared by every team mutation:
// load teams.toml, run fn (which may mutate the file in place), save,
// and append a single audit event on success. The audit event's fields
// are filled in by fn via the returned *mgmt.AuditEvent (Actor is pre-
// populated from the actor argument). fn may return a sentinel error to
// abort without saving. Every caller must provide a non-empty actor
// email so the audit log stays attributable.
func withTeams(vaultRoot string, actor mgmt.Actor, fn func(tf *mgmt.TeamsFile) (*mgmt.AuditEvent, error)) error {
	if actor.Email == "" {
		return errors.New("team mutations require a non-empty actor email (set git config user.email)")
	}
	tf, err := mgmt.LoadTeams(vaultRoot)
	if err != nil {
		return err
	}
	event, err := fn(tf)
	if err != nil {
		return err
	}
	if err := mgmt.SaveTeams(vaultRoot, tf); err != nil {
		return err
	}
	if event == nil {
		return nil
	}
	event.Actor = actor.Email
	event.TargetType = mgmt.TargetTypeTeam
	return mgmt.AppendAuditEvent(vaultRoot, *event)
}

// requireTeamAdminInTx looks up teamName in the in-transaction TeamsFile
// and returns an error unless actor is an admin. Called at the top of
// every admin-gated closure inside withTeams so the check happens after
// the flock + reload — the CLI-level pre-check in team.go is only a
// fast-fail for UX, not a security boundary.
func requireTeamAdminInTx(tf *mgmt.TeamsFile, teamName string, actor mgmt.Actor) (*mgmt.Team, error) {
	team, err := tf.FindTeam(teamName)
	if err != nil {
		return nil, err
	}
	if !team.IsAdmin(actor.Email) {
		return nil, fmt.Errorf("%s is not an admin of team %s", actor.Email, teamName)
	}
	return team, nil
}

// commonListTeams is the shared implementation of Vault.ListTeams for
// file-backed vaults (git, path). It reads .sx/teams.toml from the given
// vault root.
func commonListTeams(vaultRoot string) ([]mgmt.Team, error) {
	tf, err := mgmt.LoadTeams(vaultRoot)
	if err != nil {
		return nil, err
	}
	out := make([]mgmt.Team, len(tf.Teams))
	copy(out, tf.Teams)
	return out, nil
}

// commonGetTeam returns a single team by name from the file-backed vault.
func commonGetTeam(vaultRoot, name string) (*mgmt.Team, error) {
	tf, err := mgmt.LoadTeams(vaultRoot)
	if err != nil {
		return nil, err
	}
	team, err := tf.FindTeam(name)
	if err != nil {
		return nil, err
	}
	out := *team
	return &out, nil
}

// commonCreateTeam adds a new team. The caller is always added to both
// Members and Admins so every team has at least one admin — prevents the
// "delete all admins then take over" attack.
func commonCreateTeam(vaultRoot string, actor mgmt.Actor, team mgmt.Team) error {
	return withTeams(vaultRoot, actor, func(tf *mgmt.TeamsFile) (*mgmt.AuditEvent, error) {
		if _, err := tf.FindTeam(team.Name); err == nil {
			return nil, fmt.Errorf("%w: %s", mgmt.ErrTeamExists, team.Name)
		}
		if actor.Email != "" {
			if !slices.Contains(team.Members, actor.Email) {
				team.Members = append(team.Members, actor.Email)
			}
			if !slices.Contains(team.Admins, actor.Email) {
				team.Admins = append(team.Admins, actor.Email)
			}
		}
		tf.UpsertTeam(team)
		return &mgmt.AuditEvent{
			Event:  mgmt.EventTeamCreated,
			Target: team.Name,
			Data: map[string]any{
				"description":  team.Description,
				"members":      team.Members,
				"admins":       team.Admins,
				"repositories": team.Repositories,
			},
		}, nil
	})
}

// commonUpdateTeam replaces a team's description/name/members/admins/repos
// with the values from the input. Team must already exist.
func commonUpdateTeam(vaultRoot string, actor mgmt.Actor, team mgmt.Team) error {
	return withTeams(vaultRoot, actor, func(tf *mgmt.TeamsFile) (*mgmt.AuditEvent, error) {
		if _, err := requireTeamAdminInTx(tf, team.Name, actor); err != nil {
			return nil, err
		}
		tf.UpsertTeam(team)
		return &mgmt.AuditEvent{Event: mgmt.EventTeamUpdated, Target: team.Name}, nil
	})
}

// commonDeleteTeam removes a team and cleans up any installations that
// targeted it.
func commonDeleteTeam(vaultRoot string, actor mgmt.Actor, name string) error {
	if err := withTeams(vaultRoot, actor, func(tf *mgmt.TeamsFile) (*mgmt.AuditEvent, error) {
		if _, err := requireTeamAdminInTx(tf, name, actor); err != nil {
			return nil, err
		}
		if err := tf.DeleteTeam(name); err != nil {
			return nil, err
		}
		return &mgmt.AuditEvent{Event: mgmt.EventTeamDeleted, Target: name}, nil
	}); err != nil {
		return err
	}

	// Cascade: remove any installation rows that targeted this team. The
	// cascade lives outside withTeams because it touches installations.toml,
	// not teams.toml — a second file, a second audit concern.
	ifile, ok, err := mgmt.LoadInstallations(vaultRoot)
	if err != nil || !ok {
		return err
	}
	// Snapshot the asset names that are about to be cascade-cleared so
	// auditors can reconstruct "why did asset X stop installing for
	// team Y" — a silent bulk delete would leave a dead-end in the log.
	var clearedAssets []string
	for _, ins := range ifile.Installations {
		if ins.Kind == mgmt.InstallKindTeam && ins.Team == name {
			clearedAssets = append(clearedAssets, ins.Asset)
		}
	}
	if ifile.Remove(mgmt.Installation{Kind: mgmt.InstallKindTeam, Team: name}) == 0 {
		return nil
	}
	if err := mgmt.SaveInstallations(vaultRoot, ifile); err != nil {
		return err
	}
	for _, assetName := range clearedAssets {
		if err := mgmt.AppendAuditEvent(vaultRoot, mgmt.AuditEvent{
			Actor:      actor.Email,
			Event:      mgmt.EventInstallCleared,
			TargetType: mgmt.TargetTypeInstallation,
			Target:     assetName,
			Data: map[string]any{
				"kind":   string(mgmt.InstallKindTeam),
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
// true, the member is also added to the admin list.
func commonAddTeamMember(vaultRoot string, actor mgmt.Actor, teamName, email string, admin bool) error {
	return withTeams(vaultRoot, actor, func(tf *mgmt.TeamsFile) (*mgmt.AuditEvent, error) {
		team, err := requireTeamAdminInTx(tf, teamName, actor)
		if err != nil {
			return nil, err
		}
		normalized := mgmt.NormalizeEmail(email)
		if normalized == "" {
			return nil, errors.New("cannot add empty email")
		}
		if !slices.Contains(team.Members, normalized) {
			team.Members = append(team.Members, normalized)
		}
		if admin && !slices.Contains(team.Admins, normalized) {
			team.Admins = append(team.Admins, normalized)
		}
		tf.UpsertTeam(*team)
		return &mgmt.AuditEvent{
			Event:  mgmt.EventTeamMemberAdded,
			Target: teamName,
			Data:   map[string]any{"member": normalized, "admin": admin},
		}, nil
	})
}

// commonRemoveTeamMember removes an email from a team (both members and
// admins lists).
func commonRemoveTeamMember(vaultRoot string, actor mgmt.Actor, teamName, email string) error {
	return withTeams(vaultRoot, actor, func(tf *mgmt.TeamsFile) (*mgmt.AuditEvent, error) {
		team, err := requireTeamAdminInTx(tf, teamName, actor)
		if err != nil {
			return nil, err
		}
		normalized := mgmt.NormalizeEmail(email)
		team.Members = removeString(team.Members, normalized)
		team.Admins = removeString(team.Admins, normalized)
		tf.UpsertTeam(*team)
		return &mgmt.AuditEvent{
			Event:  mgmt.EventTeamMemberRemoved,
			Target: teamName,
			Data:   map[string]any{"member": normalized},
		}, nil
	})
}

// commonSetTeamAdmin grants or revokes admin privileges for a member.
func commonSetTeamAdmin(vaultRoot string, actor mgmt.Actor, teamName, email string, admin bool) error {
	return withTeams(vaultRoot, actor, func(tf *mgmt.TeamsFile) (*mgmt.AuditEvent, error) {
		team, err := requireTeamAdminInTx(tf, teamName, actor)
		if err != nil {
			return nil, err
		}
		normalized := mgmt.NormalizeEmail(email)
		if !team.IsMember(normalized) {
			return nil, fmt.Errorf("%s is not a member of team %s", normalized, teamName)
		}
		event := mgmt.EventTeamAdminSet
		if admin {
			if !slices.Contains(team.Admins, normalized) {
				team.Admins = append(team.Admins, normalized)
			}
		} else {
			team.Admins = removeString(team.Admins, normalized)
			event = mgmt.EventTeamAdminUnset
		}
		tf.UpsertTeam(*team)
		return &mgmt.AuditEvent{
			Event:  event,
			Target: teamName,
			Data:   map[string]any{"member": normalized},
		}, nil
	})
}

// commonAddTeamRepository adds a repository URL to a team.
func commonAddTeamRepository(vaultRoot string, actor mgmt.Actor, teamName, repoURL string) error {
	return withTeams(vaultRoot, actor, func(tf *mgmt.TeamsFile) (*mgmt.AuditEvent, error) {
		team, err := requireTeamAdminInTx(tf, teamName, actor)
		if err != nil {
			return nil, err
		}
		normalized := scope.NormalizeRepoURL(strings.TrimSpace(repoURL))
		if normalized == "" {
			return nil, errors.New("cannot add empty repository URL")
		}
		if !slices.Contains(team.Repositories, normalized) {
			team.Repositories = append(team.Repositories, normalized)
		}
		tf.UpsertTeam(*team)
		return &mgmt.AuditEvent{
			Event:  mgmt.EventTeamRepoAdded,
			Target: teamName,
			Data:   map[string]any{"repository": normalized},
		}, nil
	})
}

// commonRemoveTeamRepository removes a repository URL from a team.
func commonRemoveTeamRepository(vaultRoot string, actor mgmt.Actor, teamName, repoURL string) error {
	return withTeams(vaultRoot, actor, func(tf *mgmt.TeamsFile) (*mgmt.AuditEvent, error) {
		team, err := requireTeamAdminInTx(tf, teamName, actor)
		if err != nil {
			return nil, err
		}
		normalized := scope.NormalizeRepoURL(strings.TrimSpace(repoURL))
		team.Repositories = removeString(team.Repositories, normalized)
		tf.UpsertTeam(*team)
		return &mgmt.AuditEvent{
			Event:  mgmt.EventTeamRepoRemoved,
			Target: teamName,
			Data:   map[string]any{"repository": normalized},
		}, nil
	})
}

// commonSetAssetInstallation routes an installation target to the right
// storage: Org/Repo/Path go into skill.lock as Scopes; Team/User go into
// .sx/installations.toml.
func commonSetAssetInstallation(vaultRoot string, actor mgmt.Actor, assetName string, target InstallTarget) error {
	switch target.Kind {
	case InstallKindOrg:
		if err := setAssetScopesInLockFile(vaultRoot, assetName, nil); err != nil {
			return err
		}
	case InstallKindRepo:
		if target.Repo == "" {
			return errors.New("repo installation missing repo URL")
		}
		normalized := scope.NormalizeRepoURL(target.Repo)
		if err := addScopeToLockFile(vaultRoot, assetName, lockfile.Scope{Repo: normalized}); err != nil {
			return err
		}
	case InstallKindPath:
		if target.Repo == "" || len(target.Paths) == 0 {
			return errors.New("path installation requires repo URL and at least one path")
		}
		normalized := scope.NormalizeRepoURL(target.Repo)
		if err := addScopeToLockFile(vaultRoot, assetName, lockfile.Scope{Repo: normalized, Paths: target.Paths}); err != nil {
			return err
		}
	case InstallKindTeam:
		if target.Team == "" {
			return errors.New("team installation missing team name")
		}
		// Re-check admin membership inside the transaction (after the
		// clone/pull) so we close the TOCTOU window that opens if the
		// CLI's pre-flock pre-check and the actual mutation observed
		// different team states.
		tf, err := mgmt.LoadTeams(vaultRoot)
		if err != nil {
			return err
		}
		if _, err := requireTeamAdminInTx(tf, target.Team, actor); err != nil {
			return err
		}
		if err := addInstallationRow(vaultRoot, mgmt.Installation{
			Asset: assetName,
			Kind:  toMgmtInstallKind(target.Kind),
			Team:  target.Team,
		}); err != nil {
			return err
		}
	case InstallKindUser:
		if target.User == "" {
			return errors.New("user installation missing email")
		}
		// User-scoped installs may only target the caller. Any write-access
		// holder (git push, shared filesystem) could otherwise force an
		// asset to be "global" in another user's scope via the overlay
		// rule that matches on email. Mirrors the sleuth vault check in
		// sleuth_mgmt.go to avoid a silent privilege escalation.
		if mgmt.NormalizeEmail(target.User) != actor.Email {
			return fmt.Errorf("user-scoped installs may only target the authenticated caller (got %q, actor %q)", target.User, actor.Email)
		}
		if err := addInstallationRow(vaultRoot, mgmt.Installation{
			Asset: assetName,
			Kind:  toMgmtInstallKind(target.Kind),
			User:  target.User,
		}); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown installation kind: %q", target.Kind)
	}

	return mgmt.AppendAuditEvent(vaultRoot, mgmt.AuditEvent{
		Actor:      actor.Email,
		Event:      mgmt.EventInstallSet,
		TargetType: mgmt.TargetTypeInstallation,
		Target:     assetName,
		Data:       target.AuditData(),
	})
}

// commonClearAssetInstallations removes every installation for an asset
// from both skill.lock scopes and .sx/installations.toml rows. Missing
// lock file, missing asset entry, and missing installations file are all
// soft no-ops — calling this for an asset that's only installed via team
// or user rows (or an asset that doesn't exist anymore) must succeed so
// admins can clean up orphaned installation rows.
func commonClearAssetInstallations(vaultRoot string, actor mgmt.Actor, assetName string) error {
	scopesCleared, err := clearAssetScopesIfPresent(vaultRoot, assetName)
	if err != nil {
		return err
	}
	mutated := scopesCleared
	ifile, ok, err := mgmt.LoadInstallations(vaultRoot)
	if err != nil {
		return err
	}
	if ok && ifile.RemoveForAsset(assetName) > 0 {
		if err := mgmt.SaveInstallations(vaultRoot, ifile); err != nil {
			return err
		}
		mutated = true
	}
	if !mutated {
		return nil
	}
	return mgmt.AppendAuditEvent(vaultRoot, mgmt.AuditEvent{
		Actor:      actor.Email,
		Event:      mgmt.EventInstallCleared,
		TargetType: mgmt.TargetTypeInstallation,
		Target:     assetName,
	})
}

// clearAssetScopesIfPresent clears an asset's Scopes in skill.lock if the
// asset is present, and is a no-op if the lock file doesn't exist or
// doesn't contain the asset. The stricter setAssetScopesInLockFile is
// used for install-set paths that need the asset to exist. Returns
// whether a write occurred so callers can suppress no-op audit entries.
func clearAssetScopesIfPresent(vaultRoot, assetName string) (bool, error) {
	lockPath := lockFilePathForRoot(vaultRoot)
	if _, err := os.Stat(lockPath); os.IsNotExist(err) {
		return false, nil
	}
	lf, err := lockfile.ParseFile(lockPath)
	if err != nil {
		return false, fmt.Errorf("failed to read lock file: %w", err)
	}
	mutated := false
	for i := range lf.Assets {
		if lf.Assets[i].Name == assetName && len(lf.Assets[i].Scopes) > 0 {
			lf.Assets[i].Scopes = nil
			mutated = true
		}
	}
	if !mutated {
		return false, nil
	}
	if err := lockfile.Write(lf, lockPath); err != nil {
		return false, err
	}
	return true, nil
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

// addInstallationRow loads, upserts, and saves a single installation row.
func addInstallationRow(vaultRoot string, ins mgmt.Installation) error {
	ifile, _, err := mgmt.LoadInstallations(vaultRoot)
	if err != nil {
		return err
	}
	if ifile == nil {
		ifile = &mgmt.InstallationsFile{}
	}
	if err := ins.Validate(); err != nil {
		return err
	}
	ifile.Upsert(ins)
	return mgmt.SaveInstallations(vaultRoot, ifile)
}

// setAssetScopesInLockFile replaces the Scopes slice for the named asset in
// skill.lock. A nil scopes slice clears the field (org-wide).
func setAssetScopesInLockFile(vaultRoot, assetName string, scopes []lockfile.Scope) error {
	lockPath := lockFilePathForRoot(vaultRoot)
	lf, err := lockfile.ParseFile(lockPath)
	if err != nil {
		return fmt.Errorf("failed to read lock file: %w", err)
	}
	found := false
	for i := range lf.Assets {
		if lf.Assets[i].Name == assetName {
			lf.Assets[i].Scopes = scopes
			found = true
		}
	}
	if !found {
		return fmt.Errorf("asset %q not found in lock file", assetName)
	}
	return lockfile.Write(lf, lockPath)
}

// addScopeToLockFile appends a single scope entry to the named asset in
// skill.lock, deduping by repo URL + paths tuple.
func addScopeToLockFile(vaultRoot, assetName string, newScope lockfile.Scope) error {
	lockPath := lockFilePathForRoot(vaultRoot)
	lf, err := lockfile.ParseFile(lockPath)
	if err != nil {
		return fmt.Errorf("failed to read lock file: %w", err)
	}
	found := false
	for i := range lf.Assets {
		if lf.Assets[i].Name != assetName {
			continue
		}
		found = true
		a := &lf.Assets[i]
		if scopeExists(a.Scopes, newScope) {
			return nil
		}
		a.Scopes = append(a.Scopes, newScope)
	}
	if !found {
		return fmt.Errorf("asset %q not found in lock file", assetName)
	}
	return lockfile.Write(lf, lockPath)
}

func scopeExists(scopes []lockfile.Scope, needle lockfile.Scope) bool {
	needleRepo := scope.NormalizeRepoURL(needle.Repo)
	for _, s := range scopes {
		if scope.NormalizeRepoURL(s.Repo) != needleRepo {
			continue
		}
		if slices.Equal(s.Paths, needle.Paths) {
			return true
		}
	}
	return false
}

// toMgmtInstallKind maps the CLI-facing vault.InstallKind (org/repo/path/
// team/user) onto the narrower mgmt.InstallKind (team/user). The two enums
// share string values for the overlapping cases but are distinct types —
// this helper keeps the mapping explicit at the boundary so a future
// renaming on either side will force a compile error rather than silently
// mis-routing an installation row.
func toMgmtInstallKind(k InstallKind) mgmt.InstallKind {
	switch k {
	case InstallKindTeam:
		return mgmt.InstallKindTeam
	case InstallKindUser:
		return mgmt.InstallKindUser
	}
	return mgmt.InstallKind(k)
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
