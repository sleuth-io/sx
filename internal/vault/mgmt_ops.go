package vault

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/sleuth-io/sx/internal/asset"
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
// separate operations. If Save succeeds but audit append fails (disk
// full, permission error, etc.) the mutation is durable but unaudited.
// This is intentional — an already-committed state change cannot be
// rolled back because its audit line failed, and the alternative (roll
// back on audit failure) would be far more disruptive. The vault flock
// keeps concurrent writers from racing; lost audit lines are treated as
// an operational alarm, not a bug.
func withManifest(vaultRoot string, actor mgmt.Actor, fn func(*manifest.Manifest) (*mgmt.AuditEvent, error)) error {
	return withManifestEvents(vaultRoot, actor, func(m *manifest.Manifest) ([]mgmt.AuditEvent, error) {
		event, err := fn(m)
		if err != nil || event == nil {
			return nil, err
		}
		return []mgmt.AuditEvent{*event}, nil
	})
}

// withManifestEvents is the multi-audit-event variant used when one
// manifest mutation should produce related audit rows.
func withManifestEvents(vaultRoot string, actor mgmt.Actor, fn func(*manifest.Manifest) ([]mgmt.AuditEvent, error)) error {
	if err := actor.RequireRealIdentity(); err != nil {
		return fmt.Errorf("vault mutations require a real git identity (set git config user.email): %w", err)
	}
	m, err := loadManifest(vaultRoot)
	if err != nil {
		return err
	}
	events, err := fn(m)
	if err != nil {
		return err
	}
	if err := manifest.Save(vaultRoot, m); err != nil {
		return err
	}
	if len(events) == 0 {
		return nil
	}
	for i := range events {
		events[i].Actor = actor.Email
	}
	return mgmt.AppendAuditEvents(vaultRoot, events)
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
		return nil, fmt.Errorf("you (%s) are not an admin of team %s — only admins can modify a team", actor.Email, teamName)
	}
	return team, nil
}

const defaultListTeamsLimit = 20

func commonListTeams(vaultRoot string, opts ListTeamsOptions) (*ListTeamsResult, error) {
	m, err := loadManifest(vaultRoot)
	if err != nil {
		return nil, err
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = defaultListTeamsLimit
	}
	var matched []mgmt.Team
	for _, t := range m.Teams {
		team := manifestTeamToMgmt(t)
		if opts.Filter != "" && !strings.Contains(strings.ToLower(team.Name), strings.ToLower(opts.Filter)) {
			continue
		}
		matched = append(matched, team)
	}
	total := len(matched)
	hasMore := total > limit
	if hasMore {
		matched = matched[:limit]
	}
	return &ListTeamsResult{
		Teams:      matched,
		TotalCount: total,
		HasMore:    hasMore,
	}, nil
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

// commonCreateTeam adds a new team. If the caller did not specify any
// admins, they are added as both a member and an admin so the team isn't
// born orphaned — the same "≥1 admin" invariant we enforce on member
// removal. When the caller explicitly names another admin, we trust that
// delegation and don't force the caller into the roster: they can always
// remove themselves anyway via team member remove, so the auto-add was
// just an extra step.
func commonCreateTeam(vaultRoot string, actor mgmt.Actor, team mgmt.Team) error {
	return withManifest(vaultRoot, actor, func(m *manifest.Manifest) (*mgmt.AuditEvent, error) {
		if _, err := m.FindTeam(team.Name); err == nil {
			return nil, fmt.Errorf("%w: %s", manifest.ErrTeamExists, team.Name)
		}
		mt := mgmtTeamToManifest(team)
		if len(mt.Admins) == 0 {
			mt.Admins = append(mt.Admins, actor.Email)
		}
		// Admins must be members — being admin of a team you don't belong
		// to is nonsensical and breaks team member listings.
		for _, a := range mt.Admins {
			if !slices.Contains(mt.Members, a) {
				mt.Members = append(mt.Members, a)
			}
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
	var unlinkedBots []string
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
		// Cascade to bot team memberships: every bot that referenced
		// the deleted team must drop it from its Teams slice. Without
		// this, the data invariant "every entry in Bot.Teams references
		// an existing team" would break — and a future team re-created
		// under the same name would silently inherit the orphaned bot
		// memberships, a surprising re-attachment path.
		for i := range m.Bots {
			bot := &m.Bots[i]
			if !slices.Contains(bot.Teams, name) {
				continue
			}
			bot.Teams = removeString(bot.Teams, name)
			unlinkedBots = append(unlinkedBots, bot.Name)
		}
		if err := m.DeleteTeam(name); err != nil {
			return nil, err
		}
		return &mgmt.AuditEvent{
			Event:      mgmt.EventTeamDeleted,
			TargetType: mgmt.TargetTypeTeam,
			Target:     name,
			Data:       map[string]any{"unlinked_bots": unlinkedBots},
		}, nil
	}); err != nil {
		return err
	}

	// Per-bot audit for the cascade, mirroring the per-asset
	// install.cleared events emitted further down. Best effort: the
	// durable mutation is already on disk, so we collect rather than
	// fail-fast — a single audit-disk-full shouldn't suppress every
	// remaining cascade row.
	var auditErrs []error
	for _, botName := range unlinkedBots {
		if err := mgmt.AppendAuditEvent(vaultRoot, mgmt.AuditEvent{
			Actor:      actor.Email,
			Event:      mgmt.EventBotTeamRemoved,
			TargetType: mgmt.TargetTypeBot,
			Target:     botName,
			Data:       map[string]any{"team": name, "reason": "team_deleted"},
		}); err != nil {
			auditErrs = append(auditErrs, fmt.Errorf("bot %s: %w", botName, err))
		}
	}

	// Emit per-asset install.cleared audit events so auditors can
	// reconstruct "why did asset X stop installing for team Y". Best
	// effort: the durable mutation is already on disk, so we collect
	// errors and join rather than abort on the first failure.
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
			auditErrs = append(auditErrs, fmt.Errorf("asset %s: %w", assetName, err))
		}
	}
	return errors.Join(auditErrs...)
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

// commonListBots returns a copy of every bot in the vault's manifest.
func commonListBots(vaultRoot string) ([]mgmt.Bot, error) {
	m, err := loadManifest(vaultRoot)
	if err != nil {
		return nil, err
	}
	out := make([]mgmt.Bot, len(m.Bots))
	for i, b := range m.Bots {
		out[i] = manifestBotToMgmt(b)
		out[i].InstalledSkills = resolvedBotSkills(m, b.Name)
	}
	return out, nil
}

// commonGetBot returns a single bot by name.
func commonGetBot(vaultRoot, name string) (*mgmt.Bot, error) {
	m, err := loadManifest(vaultRoot)
	if err != nil {
		return nil, err
	}
	bot, err := findBotForMgmt(m, name)
	if err != nil {
		return nil, err
	}
	out := manifestBotToMgmt(*bot)
	out.InstalledSkills = resolvedBotSkills(m, bot.Name)
	return &out, nil
}

func resolvedBotSkills(m *manifest.Manifest, botName string) []mgmt.BotSkill {
	lf := manifest.Resolve(m, mgmt.Actor{Email: "bot:" + botName, Bot: botName})
	if lf == nil || len(lf.Assets) == 0 {
		return nil
	}
	direct := directBotSkillNameSet(m, botName)
	byName := make(map[string]mgmt.BotSkill, len(lf.Assets))
	for _, resolvedAsset := range lf.Assets {
		if resolvedAsset.Type.Key != asset.TypeSkill.Key {
			continue
		}
		name := strings.TrimSpace(resolvedAsset.Name)
		if name == "" {
			continue
		}
		if _, ok := byName[name]; ok {
			continue
		}
		_, isDirect := direct[name]
		byName[name] = mgmt.BotSkill{Name: name, IsDirectInstall: isDirect}
	}
	out := make([]mgmt.BotSkill, 0, len(byName))
	for _, skill := range byName {
		out = append(out, skill)
	}
	slices.SortFunc(out, func(a, b mgmt.BotSkill) int {
		return strings.Compare(a.Name, b.Name)
	})
	return out
}

func directBotSkillNameSet(m *manifest.Manifest, botName string) map[string]struct{} {
	out := make(map[string]struct{})
	for _, a := range m.Assets {
		if a.Type.Key != asset.TypeSkill.Key {
			continue
		}
		name := strings.TrimSpace(a.Name)
		if name == "" {
			continue
		}
		for _, s := range a.Scopes {
			if s.Kind == manifest.ScopeKindBot && s.Bot == botName {
				out[name] = struct{}{}
				break
			}
		}
	}
	return out
}

// findBotForMgmt wraps manifest.FindBot and translates manifest's
// not-found sentinel into mgmt.ErrBotNotFound so callers in any vault
// implementation give the same answer to errors.Is regardless of
// backend (file-based vs Sleuth).
func findBotForMgmt(m *manifest.Manifest, name string) (*manifest.Bot, error) {
	bot, err := m.FindBot(name)
	if err != nil {
		if errors.Is(err, manifest.ErrBotNotFound) {
			return nil, mgmt.ErrBotNotFound
		}
		return nil, err
	}
	return bot, nil
}

// commonCreateBot adds a new bot. Bot creation does not require team
// admin rights; the vault's outer write-access control (git push, file
// system permissions) is the gate. Any team listed must already exist.
func commonCreateBot(vaultRoot string, actor mgmt.Actor, bot mgmt.Bot) error {
	return withManifest(vaultRoot, actor, func(m *manifest.Manifest) (*mgmt.AuditEvent, error) {
		if _, err := m.FindBot(bot.Name); err == nil {
			return nil, fmt.Errorf("%w: %s", mgmt.ErrBotExists, bot.Name)
		}
		mb := mgmtBotToManifest(bot)
		// Validate every team listed exists. Mirrors the way teams
		// validate before persisting.
		for _, t := range mb.Teams {
			if _, err := m.FindTeam(t); err != nil {
				return nil, fmt.Errorf("team %q: %w", t, err)
			}
		}
		if _, err := m.UpsertBot(mb); err != nil {
			return nil, err
		}
		return &mgmt.AuditEvent{
			Event:      mgmt.EventBotCreated,
			TargetType: mgmt.TargetTypeBot,
			Target:     bot.Name,
			Data: map[string]any{
				"description": mb.Description,
				"teams":       mb.Teams,
			},
		}, nil
	})
}

// commonUpdateBot replaces a bot's description and (optionally) team
// list. A nil `bot.Teams` slice means "leave existing memberships
// unchanged" — the same semantic the Sleuth UpdateBot uses to skip
// the `teamIds` GraphQL input. Without this distinction, a description-
// only CLI edit (which clears Teams to nil to avoid the read-modify-
// write race documented in newBotUpdateCommand) would wipe every team
// membership the bot had on a path/git vault, since UpsertBot rewrites
// the row in full.
func commonUpdateBot(vaultRoot string, actor mgmt.Actor, bot mgmt.Bot) error {
	return withManifest(vaultRoot, actor, func(m *manifest.Manifest) (*mgmt.AuditEvent, error) {
		existing, err := findBotForMgmt(m, bot.Name)
		if err != nil {
			return nil, err
		}
		mb := mgmtBotToManifest(bot)
		if bot.Teams == nil {
			// Preserve existing memberships when caller indicated
			// "don't touch teams". Cloning the slice avoids aliasing
			// the manifest's storage in the new row.
			mb.Teams = append([]string(nil), existing.Teams...)
		}
		for _, t := range mb.Teams {
			if _, err := m.FindTeam(t); err != nil {
				return nil, fmt.Errorf("team %q: %w", t, err)
			}
		}
		if _, err := m.UpsertBot(mb); err != nil {
			return nil, err
		}
		return &mgmt.AuditEvent{
			Event:      mgmt.EventBotUpdated,
			TargetType: mgmt.TargetTypeBot,
			Target:     bot.Name,
		}, nil
	})
}

// commonDeleteBot removes a bot and cascades to drop any bot-scoped
// asset installations targeting it. Cascade ordering mirrors
// commonDeleteTeam.
func commonDeleteBot(vaultRoot string, actor mgmt.Actor, name string) error {
	var clearedAssets []string
	if err := withManifest(vaultRoot, actor, func(m *manifest.Manifest) (*mgmt.AuditEvent, error) {
		if _, err := findBotForMgmt(m, name); err != nil {
			return nil, err
		}
		assets := m.Assets[:0]
		for i := range m.Assets {
			asset := m.Assets[i]
			kept := asset.Scopes[:0]
			removed := false
			for _, s := range asset.Scopes {
				if s.Kind == manifest.ScopeKindBot && s.Bot == name {
					removed = true
					continue
				}
				kept = append(kept, s)
			}
			if removed {
				asset.Scopes = kept
				clearedAssets = append(clearedAssets, asset.Name)
			}
			if removed && len(asset.Scopes) == 0 {
				// An empty scope list means "global" in the manifest.
				// If this asset was only installed on the deleted bot,
				// drop the manifest entry instead of accidentally
				// promoting it to an org-wide install. The version files
				// remain on disk for history/audit, but are no longer
				// resolvable or installed.
				continue
			}
			assets = append(assets, asset)
		}
		m.Assets = assets
		if err := m.DeleteBot(name); err != nil {
			return nil, err
		}
		// Include cleared_assets on the bot.deleted event so a single
		// audit row tells the full cascade story — symmetric with
		// team.deleted (unlinked_bots) and removes the cross-reference
		// step for auditors querying just bot.* events.
		return &mgmt.AuditEvent{
			Event:      mgmt.EventBotDeleted,
			TargetType: mgmt.TargetTypeBot,
			Target:     name,
			Data:       map[string]any{"cleared_assets": clearedAssets},
		}, nil
	}); err != nil {
		return err
	}

	// Best-effort cascade audit, same pattern as commonDeleteTeam:
	// collect failures and join, since the durable mutation is already
	// on disk.
	var auditErrs []error
	for _, assetName := range clearedAssets {
		if err := mgmt.AppendAuditEvent(vaultRoot, mgmt.AuditEvent{
			Actor:      actor.Email,
			Event:      mgmt.EventInstallCleared,
			TargetType: mgmt.TargetTypeInstallation,
			Target:     assetName,
			Data: map[string]any{
				"kind":   string(manifest.ScopeKindBot),
				"bot":    name,
				"reason": "bot_deleted",
			},
		}); err != nil {
			auditErrs = append(auditErrs, fmt.Errorf("asset %s: %w", assetName, err))
		}
	}
	return errors.Join(auditErrs...)
}

// commonAddBotTeam adds a team to a bot's team list. Both the team and
// bot must exist. Idempotent: an already-present team is a silent
// no-op.
func commonAddBotTeam(vaultRoot string, actor mgmt.Actor, botName, teamName string) error {
	return withManifest(vaultRoot, actor, func(m *manifest.Manifest) (*mgmt.AuditEvent, error) {
		bot, err := findBotForMgmt(m, botName)
		if err != nil {
			return nil, err
		}
		teamName = strings.TrimSpace(teamName)
		if teamName == "" {
			return nil, errors.New("cannot add empty team name")
		}
		if _, err := m.FindTeam(teamName); err != nil {
			return nil, err
		}
		if slices.Contains(bot.Teams, teamName) {
			return nil, nil
		}
		bot.Teams = append(bot.Teams, teamName)
		if _, err := m.UpsertBot(*bot); err != nil {
			return nil, err
		}
		return &mgmt.AuditEvent{
			Event:      mgmt.EventBotTeamAdded,
			TargetType: mgmt.TargetTypeBot,
			Target:     botName,
			Data:       map[string]any{"team": teamName},
		}, nil
	})
}

// commonRemoveBotTeam strips a team from a bot's team list. Idempotent:
// an absent team is a silent no-op.
func commonRemoveBotTeam(vaultRoot string, actor mgmt.Actor, botName, teamName string) error {
	return withManifest(vaultRoot, actor, func(m *manifest.Manifest) (*mgmt.AuditEvent, error) {
		bot, err := findBotForMgmt(m, botName)
		if err != nil {
			return nil, err
		}
		teamName = strings.TrimSpace(teamName)
		if !slices.Contains(bot.Teams, teamName) {
			return nil, nil
		}
		bot.Teams = removeString(bot.Teams, teamName)
		if _, err := m.UpsertBot(*bot); err != nil {
			return nil, err
		}
		return &mgmt.AuditEvent{
			Event:      mgmt.EventBotTeamRemoved,
			TargetType: mgmt.TargetTypeBot,
			Target:     botName,
			Data:       map[string]any{"team": teamName},
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
			Paths: canonicalPaths(target.Paths),
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
	case InstallKindBot:
		if target.Bot == "" {
			return errors.New("bot installation missing bot name")
		}
		s = manifest.Scope{Kind: manifest.ScopeKindBot, Bot: target.Bot}
	default:
		return fmt.Errorf("unknown installation kind: %q", target.Kind)
	}

	return withManifestEvents(vaultRoot, actor, func(m *manifest.Manifest) ([]mgmt.AuditEvent, error) {
		asset := m.FindAsset(assetName)
		var recoveredAuditData map[string]any
		if asset == nil {
			recovered, ok, err := assetFromStorage(vaultRoot, assetName)
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, fmt.Errorf("asset %q not found", assetName)
			}
			recoveredAuditData = map[string]any{
				"recovered_from_storage": true,
				"version":                recovered.Version,
				"type":                   recovered.Type.Key,
				"source":                 "path",
				"path":                   filepath.ToSlash(filepath.Join("assets", recovered.Name, recovered.Version)),
			}
			m.Assets = append(m.Assets, lockfileAssetToManifest(*recovered))
			asset = m.FindAsset(assetName)
			if asset == nil {
				return nil, fmt.Errorf("asset %q not found after manifest repair", assetName)
			}
		}
		// Re-check team admin membership inside the transaction to close
		// the TOCTOU window between CLI pre-check and commit.
		if target.Kind == InstallKindTeam {
			if _, err := requireTeamAdminInTx(m, target.Team, actor); err != nil {
				return nil, err
			}
		}
		if target.Kind == InstallKindBot {
			if _, err := findBotForMgmt(m, target.Bot); err != nil {
				return nil, err
			}
		}
		if target.Kind == InstallKindOrg {
			asset.Scopes = nil
		} else if !scopeExistsOnAsset(asset.Scopes, s) {
			asset.Scopes = append(asset.Scopes, s)
		}
		installEvent := mgmt.AuditEvent{
			Event:      mgmt.EventInstallSet,
			TargetType: mgmt.TargetTypeInstallation,
			Target:     assetName,
			Data:       target.AuditData(),
		}
		if recoveredAuditData != nil {
			return []mgmt.AuditEvent{
				{
					Event:      mgmt.EventAssetRecovered,
					TargetType: mgmt.TargetTypeAsset,
					Target:     assetName,
					Data:       recoveredAuditData,
				},
				installEvent,
			}, nil
		}
		return []mgmt.AuditEvent{installEvent}, nil
	})
}

// commonRemoveAssetInstallation removes a single installation target from
// every manifest row for the named asset. If removing the target would leave
// a row with no scopes, the row is dropped instead of becoming global.
func commonRemoveAssetInstallation(vaultRoot string, actor mgmt.Actor, assetName string, target InstallTarget) error {
	needle, err := installTargetScope(target, actor)
	if err != nil {
		return err
	}
	return withManifest(vaultRoot, actor, func(m *manifest.Manifest) (*mgmt.AuditEvent, error) {
		if target.Kind == InstallKindTeam {
			if _, err := requireTeamAdminInTx(m, target.Team, actor); err != nil {
				return nil, err
			}
		}
		if target.Kind == InstallKindBot {
			if _, err := findBotForMgmt(m, target.Bot); err != nil {
				return nil, err
			}
		}
		// Walk every entry for assetName, not just FindAsset's first match:
		// repo/path scopes are inherited onto each new version row
		// (inheritAssetScopesFromManifest), so the same scope can live on
		// multiple same-name rows and all must be cleared. A row left with
		// no scopes is dropped rather than reinterpreted as global.
		changed := false
		kept := m.Assets[:0]
		for _, a := range m.Assets {
			if a.Name != assetName || len(a.Scopes) == 0 {
				kept = append(kept, a)
				continue
			}
			nextScopes := a.Scopes[:0]
			for _, s := range a.Scopes {
				if installScopeMatches(s, needle) {
					changed = true
					continue
				}
				nextScopes = append(nextScopes, s)
			}
			a.Scopes = nextScopes
			if len(a.Scopes) > 0 {
				kept = append(kept, a)
			}
		}
		if !changed {
			return nil, nil
		}
		m.Assets = kept
		return &mgmt.AuditEvent{
			Event:      mgmt.EventInstallRemoved,
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

// canonicalPaths returns a sorted copy of paths so path-scope rows are
// stored and compared in a canonical order. Set and remove route their
// paths through here on the way in, and installScopeMatches /
// scopeExistsOnAsset canonicalize both sides at comparison time — so a row
// hand-edited in sx.toml or written by an older sx version in unsorted
// order still matches regardless of the order a caller passes. The
// caller's slice is never mutated.
func canonicalPaths(paths []string) []string {
	out := append([]string(nil), paths...)
	slices.Sort(out)
	return out
}

func installTargetScope(target InstallTarget, actor mgmt.Actor) (manifest.Scope, error) {
	switch target.Kind {
	case InstallKindOrg:
		// An org-wide install is stored as an empty scope list, not an org
		// scope row, so there is no row for commonRemoveAssetInstallation to
		// match — it would silently no-op. ClearAssetInstallations can't undo
		// it either (an already-empty scope list is its no-op case); the only
		// way to stop distributing a globally-installed asset is to remove
		// the asset from the vault.
		return manifest.Scope{}, fmt.Errorf("%w: an org-wide install has no scope row to remove; remove the asset from the vault to stop distributing it", ErrNotImplemented)
	case InstallKindRepo:
		if target.Repo == "" {
			return manifest.Scope{}, errors.New("repo installation missing repo URL")
		}
		return manifest.Scope{Kind: manifest.ScopeKindRepo, Repo: scope.NormalizeRepoURL(target.Repo)}, nil
	case InstallKindPath:
		if target.Repo == "" || len(target.Paths) == 0 {
			return manifest.Scope{}, errors.New("path installation requires repo URL and at least one path")
		}
		return manifest.Scope{Kind: manifest.ScopeKindPath, Repo: scope.NormalizeRepoURL(target.Repo), Paths: canonicalPaths(target.Paths)}, nil
	case InstallKindTeam:
		if target.Team == "" {
			return manifest.Scope{}, errors.New("team installation missing team name")
		}
		return manifest.Scope{Kind: manifest.ScopeKindTeam, Team: target.Team}, nil
	case InstallKindUser:
		if target.User == "" {
			return manifest.Scope{}, errors.New("user installation missing email")
		}
		if manifest.NormalizeEmail(target.User) != actor.Email {
			return manifest.Scope{}, fmt.Errorf("user-scoped installs may only target the authenticated caller (got %q, actor %q)", target.User, actor.Email)
		}
		return manifest.Scope{Kind: manifest.ScopeKindUser, User: target.User}, nil
	case InstallKindBot:
		if target.Bot == "" {
			return manifest.Scope{}, errors.New("bot installation missing bot name")
		}
		return manifest.Scope{Kind: manifest.ScopeKindBot, Bot: target.Bot}, nil
	default:
		return manifest.Scope{}, fmt.Errorf("unknown installation kind: %q", target.Kind)
	}
}

func installScopeMatches(scopeRow, needle manifest.Scope) bool {
	if scopeRow.Kind != needle.Kind {
		return false
	}
	switch scopeRow.Kind {
	case manifest.ScopeKindOrg:
		// Unreachable in practice: an org install is stored as an empty
		// scope list, never an org scope row, and installTargetScope rejects
		// InstallKindOrg up front — so an org needle never reaches here. Kept
		// only for switch exhaustiveness; returns false rather than implying
		// org scopes are a meaningful thing to match.
		return false
	case manifest.ScopeKindRepo:
		return scope.NormalizeRepoURL(scopeRow.Repo) == scope.NormalizeRepoURL(needle.Repo)
	case manifest.ScopeKindPath:
		return scope.NormalizeRepoURL(scopeRow.Repo) == scope.NormalizeRepoURL(needle.Repo) && slices.Equal(canonicalPaths(scopeRow.Paths), canonicalPaths(needle.Paths))
	case manifest.ScopeKindTeam:
		return scopeRow.Team == needle.Team
	case manifest.ScopeKindUser:
		return manifest.NormalizeEmail(scopeRow.User) == manifest.NormalizeEmail(needle.User)
	case manifest.ScopeKindBot:
		return scopeRow.Bot == needle.Bot
	default:
		return false
	}
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

// scopeExistsOnAsset returns true when needle is already among scopes. It
// shares installScopeMatches' per-kind comparison rules (repo URL / email
// normalization, canonical path ordering) so set-time dedupe and
// remove-time matching can never drift apart.
func scopeExistsOnAsset(scopes []manifest.Scope, needle manifest.Scope) bool {
	for _, s := range scopes {
		if installScopeMatches(s, needle) {
			return true
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
		MemberCount:  len(t.Members),
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

func manifestBotToMgmt(b manifest.Bot) mgmt.Bot {
	return mgmt.Bot{
		Name:        b.Name,
		Description: b.Description,
		Teams:       append([]string(nil), b.Teams...),
	}
}

func mgmtBotToManifest(b mgmt.Bot) manifest.Bot {
	return manifest.Bot{
		Name:        b.Name,
		Description: b.Description,
		Teams:       append([]string(nil), b.Teams...),
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
