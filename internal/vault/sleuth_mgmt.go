package vault

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/logger"
	"github.com/sleuth-io/sx/internal/mgmt"
	vaultgql "github.com/sleuth-io/sx/internal/vault/graphql"
)

// Sleuth vault management methods. These call the existing pulse GraphQL
// schema — no backend changes required. Operations that don't map cleanly
// to the current schema (most notably team-scoped installations) return a
// descriptive error pointing users at the skills.new web UI.

func (s *SleuthVault) CurrentActor(ctx context.Context) (mgmt.Actor, error) {
	resp, err := vaultgql.GetMe(ctx, s.gqlClient())
	if err != nil {
		return mgmt.Actor{}, err
	}
	if resp.User == nil {
		return mgmt.Actor{}, errors.New("not authenticated")
	}
	name := strings.TrimSpace(resp.User.FirstName + " " + resp.User.LastName)
	if name == "" {
		name = resp.User.Username
	}
	return mgmt.Actor{Email: mgmt.NormalizeEmail(resp.User.Email), Name: name}, nil
}

func (s *SleuthVault) ListTeams(ctx context.Context, opts ListTeamsOptions) (*ListTeamsResult, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = defaultListTeamsLimit
	}
	var term *string
	if opts.Filter != "" {
		term = &opts.Filter
	}
	resp, err := vaultgql.ListTeams(ctx, s.gqlClient(), limit, term, sleuthMemberPageSize)
	if err != nil {
		return nil, err
	}
	conn := resp.Organization.Teams
	teams := make([]mgmt.Team, 0, len(conn.Nodes))
	for _, n := range conn.Nodes {
		teams = append(teams, sleuthTeamToMgmt(gqlTeamNodeToSleuthNode(n)))
	}
	return &ListTeamsResult{
		Teams:      teams,
		TotalCount: conn.TotalCount,
		HasMore:    conn.PageInfo.HasNextPage,
	}, nil
}

const sleuthMemberPageSize = 50

// listTeamNodes fetches raw team nodes for internal lookups (teamGIDByName,
// resolveTeamGIDs). Uses a high limit since these need to search the full list.
func (s *SleuthVault) listTeamNodes(ctx context.Context) ([]sleuthTeamNode, error) {
	return s.listTeamNodesFiltered(ctx, nil, DefaultTeamsLimit)
}

func (s *SleuthVault) listTeamNodesFiltered(ctx context.Context, term *string, first int) ([]sleuthTeamNode, error) {
	resp, err := vaultgql.ListTeams(ctx, s.gqlClient(), first, term, sleuthMemberPageSize)
	if err != nil {
		return nil, err
	}
	gqlNodes := resp.Organization.Teams.Nodes
	nodes := make([]sleuthTeamNode, len(gqlNodes))
	for i, n := range gqlNodes {
		nodes[i] = gqlTeamNodeToSleuthNode(n)
	}
	if resp.Organization.Teams.PageInfo.HasNextPage {
		logger.Get().Warn("ListTeams result has more pages; some teams may be missing",
			"first", first,
			"returned", len(nodes),
			"totalCount", resp.Organization.Teams.TotalCount)
	}
	return nodes, nil
}

func gqlTeamNodeToSleuthNode(n vaultgql.ListTeamsOrganizationOrganizationTypeTeamsTeamsConnectionNodesTeam) sleuthTeamNode {
	node := sleuthTeamNode{
		ID:          n.Id,
		Name:        n.Name,
		MemberCount: n.Members.TotalCount,
	}
	for _, a := range n.AdminMembers {
		node.Admins = append(node.Admins, a.Email)
	}
	for _, m := range n.Members.Nodes {
		node.Members = append(node.Members, sleuthTeamMember{ID: m.Id, Email: m.Email})
	}
	for _, r := range n.SkillsRepositories {
		node.Repositories = append(node.Repositories, r.Owner+"/"+r.Name)
	}
	return node
}

func (s *SleuthVault) GetTeam(ctx context.Context, name string) (*mgmt.Team, error) {
	result, err := s.ListTeams(ctx, ListTeamsOptions{Filter: name, Limit: DefaultTeamsLimit})
	if err != nil {
		return nil, err
	}
	for i := range result.Teams {
		if result.Teams[i].Name == name {
			t := result.Teams[i]
			return &t, nil
		}
	}
	return nil, mgmt.ErrTeamNotFound
}

func (s *SleuthVault) CreateTeam(ctx context.Context, team mgmt.Team) error {
	memberIDs, err := s.resolveUserGIDs(ctx, team.Members)
	if err != nil {
		return err
	}
	resp, err := vaultgql.CreateTeam(ctx, s.gqlClient(), vaultgql.CreateTeamInput{
		Name:    team.Name,
		Members: memberIDs,
	})
	if err != nil {
		return err
	}
	if resp.CreateTeam != nil {
		if err := gqlMutationErrors(resp.CreateTeam.Errors); err != nil {
			return err
		}
	}
	for _, admin := range team.Admins {
		if err := s.SetTeamAdmin(ctx, team.Name, admin, true); err != nil {
			return fmt.Errorf("failed to set admin %s: %w", admin, err)
		}
	}
	return nil
}

func (s *SleuthVault) UpdateTeam(ctx context.Context, team mgmt.Team) error {
	// teamGIDByName already returns mgmt.ErrTeamNotFound if the team is
	// absent, so a preliminary GetTeam check is redundant — one round
	// trip is enough to both verify existence and capture the GID.
	gid, err := s.teamGIDByName(ctx, team.Name)
	if err != nil {
		return err
	}
	memberIDs, err := s.resolveUserGIDs(ctx, team.Members)
	if err != nil {
		return err
	}
	resp, err := vaultgql.UpdateTeam(ctx, s.gqlClient(), vaultgql.UpdateTeamInput{
		Id:      gid,
		Name:    &team.Name,
		Members: memberIDs,
	})
	if err != nil {
		return err
	}
	if resp.UpdateTeam == nil {
		return nil
	}
	return gqlMutationErrors(resp.UpdateTeam.Errors)
}

func (s *SleuthVault) DeleteTeam(ctx context.Context, name string) error {
	gid, err := s.teamGIDByName(ctx, name)
	if err != nil {
		return err
	}
	resp, err := vaultgql.DeleteTeam(ctx, s.gqlClient(), gid)
	if err != nil {
		return err
	}
	if resp.DeleteTeam == nil {
		return nil
	}
	return gqlMutationErrors(resp.DeleteTeam.Errors)
}

// AddTeamMember adds a user to a team via the UpdateTeam mutation. Known
// limitation: this is a read-modify-write against the server's member
// list, so two concurrent AddTeamMember calls against the same team can
// interleave their reads and lose one of the additions. The GraphQL
// schema does not currently expose an additive "append member" mutation;
// when that becomes available we should switch to it. Sequential usage
// (the typical interactive CLI case) is safe.
func (s *SleuthVault) AddTeamMember(ctx context.Context, team, email string, admin bool) error {
	existing, err := s.GetTeam(ctx, team)
	if err != nil {
		return err
	}
	normalized := mgmt.NormalizeEmail(email)
	merged := append([]string(nil), existing.Members...)
	if !slices.Contains(merged, normalized) {
		merged = append(merged, normalized)
	}
	if err := s.UpdateTeam(ctx, mgmt.Team{Name: team, Members: merged}); err != nil {
		return err
	}
	if admin {
		return s.SetTeamAdmin(ctx, team, email, true)
	}
	return nil
}

func (s *SleuthVault) RemoveTeamMember(ctx context.Context, team, email string) error {
	teamGID, err := s.teamGIDByName(ctx, team)
	if err != nil {
		return err
	}
	memberGID, err := s.userGIDByEmail(ctx, email)
	if err != nil {
		return err
	}
	resp, err := vaultgql.RemoveTeamMember(ctx, s.gqlClient(), vaultgql.RemoveTeamMemberInput{
		TeamId:   teamGID,
		MemberId: memberGID,
	})
	if err != nil {
		return err
	}
	if resp.RemoveTeamMember == nil {
		return nil
	}
	return gqlMutationErrors(resp.RemoveTeamMember.Errors)
}

func (s *SleuthVault) SetTeamAdmin(ctx context.Context, team, email string, admin bool) error {
	teamGID, err := s.teamGIDByName(ctx, team)
	if err != nil {
		return err
	}
	userGID, err := s.userGIDByEmail(ctx, email)
	if err != nil {
		return err
	}
	resp, err := vaultgql.SetTeamAdmin(ctx, s.gqlClient(), vaultgql.SetTeamAdminInput{
		TeamId:  teamGID,
		UserId:  userGID,
		IsAdmin: admin,
	})
	if err != nil {
		return err
	}
	if resp.SetTeamAdmin == nil {
		return nil
	}
	return gqlMutationErrors(resp.SetTeamAdmin.Errors)
}

func (s *SleuthVault) AddTeamRepository(ctx context.Context, team, repoURL string) error {
	return fmt.Errorf("%w: add team repository on sleuth vaults (use the skills.new web UI)", ErrNotImplemented)
}

func (s *SleuthVault) RemoveTeamRepository(ctx context.Context, team, repoURL string) error {
	return fmt.Errorf("%w: remove team repository on sleuth vaults (use the skills.new web UI)", ErrNotImplemented)
}

func (s *SleuthVault) SetAssetInstallation(ctx context.Context, assetName string, target InstallTarget) error {
	switch target.Kind {
	case InstallKindOrg:
		return s.setAssetInstallationsGraphQL(ctx, assetName, nil, false)
	case InstallKindRepo:
		return s.setAssetInstallationsGraphQL(ctx, assetName, []vaultgql.RepositoryInstallationInput{{Url: target.Repo}}, false)
	case InstallKindPath:
		return s.setAssetInstallationsGraphQL(ctx, assetName, []vaultgql.RepositoryInstallationInput{{Url: target.Repo, Paths: target.Paths}}, false)
	case InstallKindUser:
		// Sleuth's setAssetInstallations GraphQL mutation supports exactly
		// one user-scoped shape: personalOnly=true with empty repositories,
		// which installs the asset for the authenticated caller ONLY.
		// See sleuth/apps/skills/graphql/mutations.py:SetAssetInstallationsMutation.
		// We must never pass repositories=[] with personalOnly=false — that
		// would be interpreted as an org-wide install on the server, which
		// is a silent privilege escalation. Reject if the target user does
		// not match the caller so the intent is unambiguous.
		actor, err := s.CurrentActor(ctx)
		if err != nil {
			return err
		}
		if mgmt.NormalizeEmail(target.User) != actor.Email {
			return fmt.Errorf("%w: user-scoped installs on sleuth vaults can only target the authenticated caller (personalOnly)", ErrNotImplemented)
		}
		return s.setAssetInstallationsGraphQL(ctx, assetName, nil, true)
	case InstallKindTeam:
		return fmt.Errorf("%w: team-scoped installs on sleuth vaults (the existing GraphQL setAssetInstallations mutation does not expose team targets; use the skills.new web UI)", ErrNotImplemented)
	case InstallKindBot:
		// Bot installs go through the dedicated installSkillToBot
		// mutation, not setAssetInstallations — the latter does not yet
		// accept bot targets. The server-side mutation is idempotent, so
		// repeated calls for the same (asset, bot) pair are safe.
		return s.installSkillToBot(ctx, assetName, target.Bot)
	}
	return fmt.Errorf("unknown install kind: %q", target.Kind)
}

func (s *SleuthVault) RemoveAssetInstallation(ctx context.Context, assetName string, target InstallTarget) error {
	if target.Kind != InstallKindBot {
		return fmt.Errorf("%w: removing %s installations is not supported by skills.new", ErrNotImplemented, target.Kind)
	}
	botGID, err := s.botGIDByName(ctx, target.Bot)
	if err != nil {
		return err
	}
	skillGID, err := s.assetGIDByName(ctx, assetName)
	if err != nil {
		return err
	}
	return s.uninstallSkillFromBot(ctx, skillGID, botGID, false)
}

// ---- Bot management (via the existing skills.new GraphQL surface) ----

// sleuthBotNode is the in-memory shape used by listBotNodes consumers
// (ListBots, GetBot, botGIDByName, botSlugByName, botsWithAssetInstalled).
// Populated from the generated ListBots response — see listBotNodes for
// the conversion. Only the fields actually read by callers are kept;
// status and apiKeys were dead fields under the old hand-rolled struct.
type sleuthBotNode struct {
	ID              string
	Name            string
	Slug            string
	Description     string
	Teams           []string // team names
	InstalledSkills []mgmt.BotSkill
}

func sleuthBotToMgmt(node sleuthBotNode) mgmt.Bot {
	return mgmt.Bot{
		Name:            node.Name,
		Slug:            node.Slug,
		Description:     node.Description,
		Teams:           append([]string(nil), node.Teams...),
		InstalledSkills: append([]mgmt.BotSkill(nil), node.InstalledSkills...),
	}
}

// sleuthBotListSoftCap is the count at which we warn about possible
// truncation. The server's `bots` field is currently a plain GraphQL
// list (not a connection) and returns every row in one response, but
// if a future server version adds an undeclared cap we want to surface
// it before it becomes a silent correctness bug — e.g.
// ClearAssetInstallations could otherwise skip uninstalls for bots past
// the cap. This mirrors the saturation warning on listTeamNodes.
const sleuthBotListSoftCap = 1000

// listBotNodes fetches the raw bot nodes from the server. ListBots
// projects these to mgmt.Bot; helpers like botGIDByName scan for a
// single row.
func (s *SleuthVault) listBotNodes(ctx context.Context) ([]sleuthBotNode, error) {
	resp, err := vaultgql.ListBots(ctx, s.gqlClient())
	if err != nil {
		return nil, err
	}
	nodes := make([]sleuthBotNode, len(resp.Bots))
	for i, b := range resp.Bots {
		teams := make([]string, len(b.Teams))
		for j, t := range b.Teams {
			teams[j] = t.Name
		}
		installedSkills := cleanBotSkills(b.InstalledSkills)
		nodes[i] = sleuthBotNode{
			ID:              b.Id,
			Name:            b.Name,
			Slug:            b.Slug,
			Description:     b.Description,
			Teams:           teams,
			InstalledSkills: installedSkills,
		}
	}
	warnIfBotListSoftCap(nodes)
	return nodes, nil
}

// cleanBotSkills dedupes a bot's installed skill assets by name, OR-ing the
// IsDirectInstall flag across duplicate entries, and returns them sorted by
// name. The server field is historically named installedSkills but may carry
// non-skill assets; keep this projection skill-only so consumers don't render
// installed agent prompt files as installed skills.
func cleanBotSkills(skills []vaultgql.ListBotsBotsManagedBotInstalledSkillsBotInstalledSkill) []mgmt.BotSkill {
	byName := make(map[string]bool, len(skills))
	for _, skill := range skills {
		if !isSleuthAssetType(skill.AssetType, asset.TypeSkill.Key) {
			continue
		}
		name := strings.TrimSpace(skill.Name)
		if name == "" {
			continue
		}
		byName[name] = byName[name] || skill.IsDirectInstall
	}
	out := make([]mgmt.BotSkill, 0, len(byName))
	for name, isDirect := range byName {
		out = append(out, mgmt.BotSkill{Name: name, IsDirectInstall: isDirect})
	}
	slices.SortFunc(out, func(a, b mgmt.BotSkill) int {
		return strings.Compare(a.Name, b.Name)
	})
	return out
}

func isSleuthAssetType(got, want string) bool {
	return strings.EqualFold(strings.TrimSpace(got), strings.TrimSpace(want))
}

func warnIfBotListSoftCap(nodes []sleuthBotNode) {
	if len(nodes) >= sleuthBotListSoftCap {
		logger.Get().Warn("ListBots result reached the soft cap; some bots may be truncated by an undeclared server-side limit",
			"soft_cap", sleuthBotListSoftCap,
			"returned", len(nodes))
	}
}

func (s *SleuthVault) ListBots(ctx context.Context) ([]mgmt.Bot, error) {
	nodes, err := s.listBotNodes(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]mgmt.Bot, 0, len(nodes))
	for _, node := range nodes {
		out = append(out, sleuthBotToMgmt(node))
	}
	return out, nil
}

func (s *SleuthVault) GetBot(ctx context.Context, name string) (*mgmt.Bot, error) {
	nodes, err := s.listBotNodes(ctx)
	if err != nil {
		return nil, err
	}
	for _, n := range nodes {
		if n.Name == name {
			b := sleuthBotToMgmt(n)
			return &b, nil
		}
	}
	return nil, mgmt.ErrBotNotFound
}

// botGIDByName resolves a bot name to its server GID. Reuses listBotNodes
// rather than introducing a second round-trip schema.
func (s *SleuthVault) botGIDByName(ctx context.Context, name string) (string, error) {
	nodes, err := s.listBotNodes(ctx)
	if err != nil {
		return "", err
	}
	for _, n := range nodes {
		if n.Name == name {
			return n.ID, nil
		}
	}
	return "", mgmt.ErrBotNotFound
}

// botSlugByName resolves a bot's display name to its server-side slug.
// The skills.new bot model auto-generates slug from name (lowercased,
// hyphenated), so a bot named "Python Backend" lives under slug
// "python-backend". Any GraphQL field that accepts a `slug:` argument
// (notably `bot(slug:)`) MUST be called with the slug — passing the
// raw name silently returns null, which would surface as "no API keys"
// rather than an error.
func (s *SleuthVault) botSlugByName(ctx context.Context, name string) (string, error) {
	nodes, err := s.listBotNodes(ctx)
	if err != nil {
		return "", err
	}
	for _, n := range nodes {
		if n.Name == name {
			return n.Slug, nil
		}
	}
	return "", mgmt.ErrBotNotFound
}

func (s *SleuthVault) CreateBot(ctx context.Context, bot mgmt.Bot) (string, error) {
	teamIDs, err := s.resolveTeamGIDs(ctx, bot.Teams)
	if err != nil {
		return "", err
	}
	input := vaultgql.CreateBotInput{Name: bot.Name}
	if bot.Description != "" {
		input.Description = &bot.Description
	}
	if len(teamIDs) > 0 {
		input.TeamIds = teamIDs
	}
	// createBot auto-issues a default API key on the server and returns
	// the raw token under `botKey` (only available on this single
	// response — there is no follow-up API to fetch it again). Capture
	// it so the CLI can print it once; otherwise the auto-issued key
	// would be wasted and the user would have to immediately call
	// `sx bot key create` to get a usable token.
	resp, err := vaultgql.CreateBot(ctx, s.gqlClient(), input)
	if err != nil {
		return "", err
	}
	if resp.CreateBot == nil {
		return "", errors.New("missing createBot payload in response")
	}
	return resp.CreateBot.BotKey, nil
}

func (s *SleuthVault) UpdateBot(ctx context.Context, bot mgmt.Bot) error {
	gid, err := s.botGIDByName(ctx, bot.Name)
	if err != nil {
		return err
	}
	teamIDs, err := s.resolveTeamGIDs(ctx, bot.Teams)
	if err != nil {
		return err
	}
	// Always include description in the input so an empty value clears
	// the field server-side. Skipping it on `bot.Description == ""`
	// silently dropped `sx bot update <name> --description ""` — the
	// CLI's PreRun guard already distinguishes "flag absent" from
	// "flag set to empty", so by the time we get here the empty value
	// is intentional.
	input := vaultgql.UpdateBotInput{
		Id:          gid,
		Name:        &bot.Name,
		Description: &bot.Description,
	}
	if bot.Teams != nil {
		if teamIDs == nil {
			teamIDs = []string{}
		}
		input.TeamIds = teamIDs
	}
	resp, err := vaultgql.UpdateBot(ctx, s.gqlClient(), input)
	if err != nil {
		return err
	}
	if resp.UpdateBot == nil {
		return nil
	}
	return gqlMutationErrors(resp.UpdateBot.Errors)
}

func (s *SleuthVault) DeleteBot(ctx context.Context, name string) error {
	gid, err := s.botGIDByName(ctx, name)
	if err != nil {
		return err
	}
	resp, err := vaultgql.DeleteBot(ctx, s.gqlClient(), gid)
	if err != nil {
		return err
	}
	if resp.DeleteBot == nil {
		return nil
	}
	return gqlMutationErrors(resp.DeleteBot.Errors)
}

// AddBotTeam appends a team to a bot's team set via UpdateBot. Sleuth's
// updateBot expects the full team list, so we read-modify-write — same
// limitation as AddTeamMember.
func (s *SleuthVault) AddBotTeam(ctx context.Context, botName, teamName string) error {
	bot, err := s.GetBot(ctx, botName)
	if err != nil {
		return err
	}
	if slices.Contains(bot.Teams, teamName) {
		return nil
	}
	bot.Teams = append(bot.Teams, teamName)
	return s.UpdateBot(ctx, *bot)
}

func (s *SleuthVault) RemoveBotTeam(ctx context.Context, botName, teamName string) error {
	bot, err := s.GetBot(ctx, botName)
	if err != nil {
		return err
	}
	if !slices.Contains(bot.Teams, teamName) {
		return nil
	}
	// Build a fresh slice rather than reslicing bot.Teams in place, so we
	// never mutate the backing array GetBot handed us (which a future cached
	// GetBot could share). Stays non-nil empty when the last team is removed,
	// so UpdateBot still emits teamIds: [].
	remaining := make([]string, 0, len(bot.Teams))
	for _, t := range bot.Teams {
		if t != teamName {
			remaining = append(remaining, t)
		}
	}
	bot.Teams = remaining
	return s.UpdateBot(ctx, *bot)
}

// resolveTeamGIDs maps a list of team names to GIDs in a single pass over
// the team list.
func (s *SleuthVault) resolveTeamGIDs(ctx context.Context, names []string) ([]string, error) {
	if len(names) == 0 {
		return nil, nil
	}
	nodes, err := s.listTeamNodes(ctx)
	if err != nil {
		return nil, err
	}
	idsByName := make(map[string]string, len(nodes))
	for _, n := range nodes {
		idsByName[n.Name] = n.ID
	}
	out := make([]string, 0, len(names))
	for _, name := range names {
		gid, ok := idsByName[name]
		if !ok {
			return nil, fmt.Errorf("%w: %s", mgmt.ErrTeamNotFound, name)
		}
		out = append(out, gid)
	}
	return out, nil
}

// assetGIDByName resolves an asset display name or slug to its server GID via
// the vault assets search. ListAssets exposes slugs for Sleuth assets, while
// older callers may still pass display names; both are accepted.
//
// This resolver backs every install path that takes an asset reference
// (SetAssetInstallation for org/repo/path/team/user/bot, plus uninstall), not
// just bot installs — a regression here mis-targets all of them, so keep the
// matching rules conservative.
//
// Slugs are unique and stable, so an exact slug match wins over display-name
// matches from other assets. ListAssets and upload responses both return
// slugs, so callers using the public API must be able to pass that value
// back here without being blocked by another asset's display name.
func (s *SleuthVault) assetGIDByName(ctx context.Context, name string) (string, error) {
	info, err := s.assetInfoByName(ctx, name)
	if err != nil {
		return "", err
	}
	return info.id, nil
}

func (s *SleuthVault) assetInfoByName(ctx context.Context, name string) (assetIDMatch, error) {
	resp, err := vaultgql.AssetGID(ctx, s.gqlClient(), name)
	if err != nil {
		return assetIDMatch{}, err
	}
	// VaultAsset is a GraphQL interface with concrete subtypes
	// (Skill, MCP, Agent, ClaudeCodePlugin, ...). Use the interface
	// getters to avoid switching on every variant.
	name = strings.TrimSpace(name)
	var slugMatch assetIDMatch
	var nameMatches []assetIDMatch
	for _, n := range resp.Vault.Assets.Nodes {
		match := assetIDMatch{id: n.GetId(), slug: n.GetSlug(), typ: string(n.GetType())}
		if n.GetSlug() == name && slugMatch.id == "" {
			slugMatch = match
		}
		if n.GetName() == name && n.GetSlug() != name {
			nameMatches = appendDistinctAssetMatch(nameMatches, match)
		}
	}
	// An exact slug match is unambiguous — slugs are unique and are what
	// ListAssets / upload responses hand back — so it always wins.
	if slugMatch.id != "" {
		return slugMatch, nil
	}
	switch len(nameMatches) {
	case 0:
		return assetIDMatch{}, fmt.Errorf("%w: asset %q", ErrAssetNotFound, name)
	case 1:
		return nameMatches[0], nil
	default:
		// Several distinct assets share this display name and none matched
		// it as a slug. Surface the ambiguity (with the candidate slugs)
		// rather than silently installing whichever the server listed first.
		slugs := make([]string, 0, len(nameMatches))
		for _, m := range nameMatches {
			slugs = append(slugs, m.slug)
		}
		return assetIDMatch{}, fmt.Errorf("asset name %q is ambiguous; multiple assets share it (slugs: %s) — install by slug instead", name, strings.Join(slugs, ", "))
	}
}

type assetIDMatch struct {
	id   string
	slug string
	typ  string
}

// appendDistinctAssetMatch appends m unless an entry with the same id is
// already present, so a duplicated search node doesn't read as ambiguity.
func appendDistinctAssetMatch(matches []assetIDMatch, m assetIDMatch) []assetIDMatch {
	for _, existing := range matches {
		if existing.id == m.id {
			return matches
		}
	}
	return append(matches, m)
}

// installSkillToBot installs an asset directly to a bot via the existing
// pair of mutations. setAssetInstallations does NOT yet support bot
// targets — installSkillToBot is the dedicated endpoint. The mutation
// returns both `errors` and `success`; checkSuccessMutation requires
// success=true so a server returning {success:false, errors:[]} surfaces
// as an error rather than silently passing.
func (s *SleuthVault) installSkillToBot(ctx context.Context, assetName, botName string) error {
	botGID, err := s.botGIDByName(ctx, botName)
	if err != nil {
		return err
	}
	skillGID, err := s.assetGIDByName(ctx, assetName)
	if err != nil {
		return err
	}
	resp, err := vaultgql.InstallSkillToBot(ctx, s.gqlClient(), botGID, skillGID)
	if err != nil {
		return err
	}
	if resp.InstallSkillToBot == nil {
		return errors.New("missing installSkillToBot payload in response")
	}
	if err := gqlMutationErrors(resp.InstallSkillToBot.Errors); err != nil {
		return err
	}
	if !resp.InstallSkillToBot.Success {
		return errors.New("installSkillToBot reported success=false with no errors — possible schema drift")
	}
	return nil
}

// uninstallSkillFromBot is the inverse of installSkillToBot. When
// missingOK is true, a false/no-errors response is treated as an
// idempotent no-op: Pulse reports success=false when no bot installation
// row was deleted, which is acceptable during cleanup flows such as
// ClearAssetInstallations. Explicit RemoveAssetInstallation calls pass
// missingOK=false so unexpected no-op removals still surface.
func (s *SleuthVault) uninstallSkillFromBot(ctx context.Context, skillGID, botGID string, missingOK bool) error {
	resp, err := vaultgql.UninstallSkillFromBot(ctx, s.gqlClient(), botGID, skillGID)
	if err != nil {
		return err
	}
	if resp.UninstallSkillFromBot == nil {
		return errors.New("missing uninstallSkillFromBot payload in response")
	}
	if err := gqlMutationErrors(resp.UninstallSkillFromBot.Errors); err != nil {
		return err
	}
	if !resp.UninstallSkillFromBot.Success {
		if missingOK {
			return nil
		}
		return errors.New("uninstallSkillFromBot reported success=false with no errors — possible schema drift")
	}
	return nil
}

// botsInstalledLookupConcurrency caps how many `bot(slug:){installedSkills}`
// requests we issue in parallel. The Sleuth GraphQL endpoint handles a
// modest amount of concurrency comfortably; 8 is a balance between
// shaving wall-clock latency for orgs with many bots and not stampeding
// the API. Adjust if production observability suggests otherwise.
const botsInstalledLookupConcurrency = 8

// botsWithAssetInstalled queries every bot in the org and returns the
// GIDs of those whose installedSkills list contains assetName as a direct
// bot install. Inherited org/team/repo skills are intentionally ignored:
// ClearAssetInstallations is looking only for bot-scoped rows that require
// the dedicated uninstallSkillFromBot mutation. Non-bot scopes are handled
// later by removeAssetInstallations.
//
// Performance: the per-bot fan-out is bounded by botsInstalledLookupConcurrency
// goroutines. The first error returned by any worker cancels its peers via
// the local context; the function returns that error immediately so
// ClearAssetInstallations doesn't continue against a possibly-inconsistent
// view of the org.
func (s *SleuthVault) botsWithAssetInstalled(ctx context.Context, assetName, assetType string) ([]string, error) {
	nodes, err := s.listBotNodes(ctx)
	if err != nil {
		return nil, err
	}
	if len(nodes) == 0 {
		return nil, nil
	}

	type result struct {
		idx int
		gid string
	}
	jobs := make(chan int, len(nodes))
	results := make(chan result, len(nodes))
	errs := make(chan error, botsInstalledLookupConcurrency)

	innerCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	worker := func() {
		for idx := range jobs {
			n := nodes[idx]
			resp, err := vaultgql.BotInstalled(innerCtx, s.gqlClient(), n.Slug)
			if err != nil {
				errs <- err
				cancel()
				return
			}
			for _, sk := range resp.Bot.InstalledSkills {
				if sk.Name == assetName && sk.IsDirectInstall && isSleuthAssetType(sk.AssetType, assetType) {
					results <- result{idx: idx, gid: n.ID}
					break
				}
			}
		}
	}

	workerCount := min(botsInstalledLookupConcurrency, len(nodes))
	var wg sync.WaitGroup
	wg.Add(workerCount)
	for range workerCount {
		go func() { defer wg.Done(); worker() }()
	}
	for idx := range nodes {
		jobs <- idx
	}
	close(jobs)
	wg.Wait()
	close(results)
	close(errs)

	for err := range errs {
		if err != nil {
			return nil, err
		}
	}

	// Re-order matches by original bot list index so output is stable
	// across parallel runs — easier to reason about in tests and logs.
	collected := make([]result, 0, len(nodes))
	for r := range results {
		collected = append(collected, r)
	}
	slices.SortFunc(collected, func(a, b result) int { return cmp.Compare(a.idx, b.idx) })
	gids := make([]string, 0, len(collected))
	for _, r := range collected {
		gids = append(gids, r.gid)
	}
	return gids, nil
}

// ---- Bot API key management (Sleuth-only — implements BotApiKeyManager) ----

func (s *SleuthVault) CreateBotApiKey(ctx context.Context, botName, label string) (string, mgmt.BotApiKey, error) {
	gid, err := s.botGIDByName(ctx, botName)
	if err != nil {
		return "", mgmt.BotApiKey{}, err
	}
	resp, err := vaultgql.CreateBotApiKey(ctx, s.gqlClient(), gid, label)
	if err != nil {
		return "", mgmt.BotApiKey{}, err
	}
	if resp.CreateBotApiKey == nil {
		return "", mgmt.BotApiKey{}, errors.New("missing createBotApiKey payload in response")
	}
	// The GraphQL response omits the per-key metadata (id/maskedToken/
	// createdAt) since the raw token is the only useful payload at
	// creation time. Callers that need to display the masked form should
	// follow up with ListBotApiKeys.
	return resp.CreateBotApiKey.BotKey, mgmt.BotApiKey{Label: label}, nil
}

func (s *SleuthVault) CreateBotRuntimeToken(ctx context.Context, botName, label string, ttlSeconds int) (string, time.Time, error) {
	gid, err := s.botGIDByName(ctx, botName)
	if err != nil {
		return "", time.Time{}, err
	}
	label = strings.TrimSpace(label)
	var labelPtr *string
	if label != "" {
		labelPtr = &label
	}
	var ttl *int
	if ttlSeconds != 0 {
		ttl = &ttlSeconds
	}
	resp, err := vaultgql.CreateBotRuntimeToken(ctx, s.gqlClient(), gid, labelPtr, ttl)
	if err != nil {
		return "", time.Time{}, err
	}
	if resp.CreateBotRuntimeToken == nil {
		return "", time.Time{}, errors.New("missing createBotRuntimeToken payload in response")
	}
	return resp.CreateBotRuntimeToken.BotKey, resp.CreateBotRuntimeToken.ExpiresAt, nil
}

func (s *SleuthVault) RevokeBotRuntimeTokens(ctx context.Context, botName string) (int, error) {
	gid, err := s.botGIDByName(ctx, botName)
	if err != nil {
		return 0, err
	}
	resp, err := vaultgql.RevokeBotRuntimeTokens(ctx, s.gqlClient(), gid)
	if err != nil {
		return 0, err
	}
	if resp.RevokeBotRuntimeTokens == nil {
		return 0, errors.New("missing revokeBotRuntimeTokens payload in response")
	}
	return resp.RevokeBotRuntimeTokens.RevokedCount, nil
}

func (s *SleuthVault) ListBotApiKeys(ctx context.Context, botName string) ([]mgmt.BotApiKey, error) {
	slug, err := s.botSlugByName(ctx, botName)
	if err != nil {
		return nil, err
	}
	resp, err := vaultgql.BotApiKeys(ctx, s.gqlClient(), slug)
	if err != nil {
		return nil, err
	}
	out := make([]mgmt.BotApiKey, 0, len(resp.Bot.ApiKeys))
	for _, k := range resp.Bot.ApiKeys {
		out = append(out, mgmt.BotApiKey{
			ID:          k.Id,
			Label:       k.Label,
			MaskedToken: k.MaskedToken,
			CreatedAt:   k.CreatedAt,
		})
	}
	return out, nil
}

func (s *SleuthVault) DeleteBotApiKey(ctx context.Context, botName, keyID string) error {
	// Verify the key actually belongs to the named bot before issuing
	// the mutation — the GraphQL endpoint takes a global keyId and
	// would otherwise happily delete another bot's key if the user
	// passed mismatched arguments (e.g. typo on the bot name with a
	// real keyId from a different bot in scope).
	keys, err := s.ListBotApiKeys(ctx, botName)
	if err != nil {
		return err
	}
	matched := false
	for _, k := range keys {
		if k.ID == keyID {
			matched = true
			break
		}
	}
	if !matched {
		return fmt.Errorf("api key %q is not owned by bot %q", keyID, botName)
	}
	resp, err := vaultgql.DeleteBotApiKey(ctx, s.gqlClient(), keyID)
	if err != nil {
		return err
	}
	if resp.DeleteBotApiKey == nil {
		return errors.New("missing deleteBotApiKey payload in response")
	}
	if err := gqlMutationErrors(resp.DeleteBotApiKey.Errors); err != nil {
		return err
	}
	if !resp.DeleteBotApiKey.Success {
		return errors.New("deleteBotApiKey reported success=false with no errors — possible schema drift")
	}
	return nil
}

func (s *SleuthVault) CreateAgentAsset(ctx context.Context, name, description, rawContent, botName string) (AddAssetResult, error) {
	name = strings.TrimSpace(name)
	rawContent = strings.TrimSpace(rawContent)
	if name == "" || rawContent == "" {
		return AddAssetResult{}, errors.New("agent name and raw content are required")
	}
	botGID, err := s.botGIDByName(ctx, botName)
	if err != nil {
		return AddAssetResult{}, fmt.Errorf("resolve bot for agent install: %w", err)
	}
	input := vaultgql.CreateAssetInput{
		Name:      name,
		AssetType: "agent",
		Installations: []vaultgql.AssetInstallationInput{
			{
				EntityType: vaultgql.VaultAssetInstallationEntityTypeBot,
				EntityId:   &botGID,
			},
		},
		RawContent: &rawContent,
	}
	if description = strings.TrimSpace(description); description != "" {
		input.Description = &description
	}
	resp, err := vaultgql.CreateAgentAsset(ctx, s.gqlClient(), input)
	if err != nil {
		return AddAssetResult{}, fmt.Errorf("create agent asset: %w", err)
	}
	if resp.CreateAsset == nil {
		return AddAssetResult{}, errors.New("missing createAsset payload in response")
	}
	if err := gqlMutationErrors(resp.CreateAsset.Errors); err != nil {
		return AddAssetResult{}, err
	}
	if resp.CreateAsset.Asset == nil {
		return AddAssetResult{}, errors.New("createAsset returned no asset")
	}
	asset := *resp.CreateAsset.Asset
	result := AddAssetResult{
		Name:           strings.TrimSpace(asset.GetSlug()),
		Version:        strings.TrimSpace(asset.GetLatestVersion()),
		IsFirstVersion: true,
	}
	if result.Name == "" {
		result.Name = name
	}
	if result.Version == "" {
		result.Version = "1"
	}
	return result, nil
}

func (s *SleuthVault) ClearAssetInstallations(ctx context.Context, assetName string) error {
	// Two-step clear: removeAssetInstallations handles repo/team/user/
	// org scopes but does NOT touch bot installs (those live in a
	// separate AssetInstallation row keyed by bot_id, mutated via
	// installSkillToBot/uninstallSkillFromBot). Walk the bot list
	// first; a partial failure here would leave us with neither the
	// bot installs cleared nor the non-bot ones, so do bots first and
	// only call the main clear if every bot uninstall succeeds.
	info, err := s.assetInfoByName(ctx, assetName)
	if err != nil {
		return err
	}
	botGIDs, err := s.botsWithAssetInstalled(ctx, assetName, info.typ)
	if err != nil {
		return fmt.Errorf("listing bots for asset %q: %w", assetName, err)
	}
	for _, botGID := range botGIDs {
		if err := s.uninstallSkillFromBot(ctx, info.id, botGID, true); err != nil {
			return fmt.Errorf("uninstalling %q from bot %s: %w", assetName, botGID, err)
		}
	}

	resp, err := vaultgql.RemoveAssetInstallations(ctx, s.gqlClient(), vaultgql.RemoveAssetInstallationsInput{
		AssetName: assetName,
	})
	if err != nil {
		return err
	}
	if resp.RemoveAssetInstallations == nil {
		return nil
	}
	return gqlMutationErrors(resp.RemoveAssetInstallations.Errors)
}

func (s *SleuthVault) RecordUsageEvents(ctx context.Context, events []mgmt.UsageEvent) error {
	// Usage events go through the existing PostUsageStats HTTP path for
	// sleuth vaults — it talks to /api/usage, not GraphQL. Marshal each
	// event to the legacy wire format and delegate.
	//
	// Note: ev.Actor is intentionally dropped from the wire payload — the
	// server always attributes events to the bearer-token holder. Any
	// caller that sets Actor to another user will see it silently
	// rewritten to the authenticated caller on ingestion.
	if len(events) == 0 {
		return nil
	}
	var b strings.Builder
	for i, ev := range events {
		if i > 0 {
			b.WriteByte('\n')
		}
		line, err := json.Marshal(map[string]any{
			"asset_name":    ev.AssetName,
			"asset_version": ev.AssetVersion,
			"asset_type":    ev.AssetType,
			"timestamp":     timeOrNow(ev.Timestamp).Format(time.RFC3339),
		})
		if err != nil {
			return err
		}
		b.Write(line)
	}
	return s.PostUsageStats(ctx, b.String())
}

func (s *SleuthVault) GetUsageStats(ctx context.Context, filter mgmt.UsageFilter) (*mgmt.UsageSummary, error) {
	// Sleuth usage stats come from the server's PQL fragments
	// (asset.usage). Rather than reimplementing the dashboard math, surface
	// a clear ErrNotImplemented so callers know to use the web UI.
	return nil, fmt.Errorf("%w: sleuth vault usage stats (use the skills.new web UI dashboards)", ErrNotImplemented)
}

// sleuthAuditDefaultPageSize is the server-side cap we ask for when the
// caller didn't set filter.Limit. The server-side query plus client-side
// filtering means a small server page paired with selective filters can
// return zero rows even though matches exist further back in the log.
// Setting this high (1000) makes truncation very unlikely; a warning
// fires if we saturate so operators notice when they need a higher cap
// or server-side filter support.
const sleuthAuditDefaultPageSize = 1000

func (s *SleuthVault) QueryAuditEvents(ctx context.Context, filter mgmt.AuditFilter) ([]mgmt.AuditEvent, error) {
	// If the caller specified a limit, honor it directly; otherwise use
	// the wide default so client-side filters don't silently drop rows
	// that existed just beyond a small server page.
	userLimit := filter.Limit
	first := userLimit
	if first <= 0 {
		first = sleuthAuditDefaultPageSize
	}
	resp, err := vaultgql.AssetAuditLog(ctx, s.gqlClient(), &first)
	if err != nil {
		return nil, err
	}
	// Warn when the server returned exactly the page we asked for and the
	// caller didn't choose that limit themselves — a saturation signal
	// that older entries may have been dropped before client-side
	// filtering ran.
	if userLimit == 0 && len(resp.AssetAuditLog.Nodes) >= first {
		logger.Get().Warn("QueryAuditEvents result saturated page size; older events may be truncated",
			"page_size", first,
			"returned", len(resp.AssetAuditLog.Nodes))
	}
	var out []mgmt.AuditEvent
	for _, node := range resp.AssetAuditLog.Nodes {
		ev := mgmt.AuditEvent{
			Timestamp:  node.Date,
			Actor:      derefStr(node.ActorEmail),
			Event:      node.Event,
			TargetType: node.TargetType,
			Target:     derefStr(node.TargetName),
		}
		// node.Data is a JSONString scalar that normally wire-encodes as a
		// JSON-encoded string ("{\"foo\":...}"), but older payloads may
		// wire the object directly. Dispatch on the first non-whitespace
		// byte so both shapes are handled explicitly.
		if node.Data != nil {
			raw := bytes.TrimSpace(*node.Data)
			switch {
			case len(raw) == 0, bytes.Equal(raw, []byte("null")):
				// empty payload — nothing to decode
			case raw[0] == '"':
				var inner string
				if err := json.Unmarshal(raw, &inner); err != nil || inner == "" {
					if err != nil {
						logger.Get().Warn("audit log: malformed JSONString Data", "err", err)
					}
					break
				}
				if err := json.Unmarshal([]byte(inner), &ev.Data); err != nil {
					logger.Get().Warn("audit log: failed to decode inner Data object", "err", err)
				}
			default:
				if err := json.Unmarshal(raw, &ev.Data); err != nil {
					logger.Get().Warn("audit log: failed to decode Data object", "err", err)
				}
			}
		}
		if !sleuthAuditMatches(ev, filter) {
			continue
		}
		out = append(out, ev)
	}
	return out, nil
}

// derefStr returns the pointee or "" when nil. Used for nullable string
// fields in genqlient responses where the call site previously typed the
// field as plain string (silently coercing null to empty).
func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func sleuthAuditMatches(ev mgmt.AuditEvent, f mgmt.AuditFilter) bool {
	if f.Actor != "" && !strings.EqualFold(ev.Actor, f.Actor) {
		return false
	}
	if f.EventPrefix != "" && !strings.HasPrefix(ev.Event, f.EventPrefix) {
		return false
	}
	if f.Target != "" && ev.Target != f.Target {
		return false
	}
	if !f.Since.IsZero() && ev.Timestamp.Before(f.Since) {
		return false
	}
	if !f.Until.IsZero() && ev.Timestamp.After(f.Until) {
		return false
	}
	return true
}

// ---- helpers ----

type sleuthTeamMember struct {
	ID    string
	Email string
}

// sleuthTeamNode is the in-memory shape used by listTeamNodes consumers
// (ListTeams, GetTeam, teamGIDByName, etc.). Populated from the generated
// ListTeams response — see listTeamNodes for the conversion.
type sleuthTeamNode struct {
	ID           string
	Name         string
	Admins       []string // emails from adminMembers
	Members      []sleuthTeamMember
	MemberCount  int
	Repositories []string // owner/name slugs
}

type sleuthMutationError struct {
	Field    string   `json:"field"`
	Messages []string `json:"messages"`
}

func sleuthTeamToMgmt(node sleuthTeamNode) mgmt.Team {
	team := mgmt.Team{Name: node.Name, MemberCount: node.MemberCount}
	for _, m := range node.Members {
		team.Members = append(team.Members, mgmt.NormalizeEmail(m.Email))
	}
	for _, email := range node.Admins {
		team.Admins = append(team.Admins, mgmt.NormalizeEmail(email))
	}
	team.Repositories = append(team.Repositories, node.Repositories...)
	return team
}

// gqlErrItemPtr is the constraint over generated *ErrorType pointer types
// every mutation response returns. Together with gqlMutationErrors, it lets
// migration call sites convert generated error slices into sleuthMutationError
// in one line, despite each mutation having its own distinct ErrorType.
type gqlErrItemPtr[T any] interface {
	*T
	GetField() string
	GetMessages() []string
}

// gqlMutationErrors converts a generated mutation's `errors { field messages }`
// slice into the existing sleuthMutationError shape so all call sites can keep
// using sleuthMutationErrorsToErr for consistent error formatting.
//
// Shape assumption: this helper only works for mutations whose payload type
// exposes `errors: [ErrorType!]!` where ErrorType has `field: String` and
// `messages: [String!]!`. If a future mutation deviates (e.g. a non-list
// `error: ErrorType` or a renamed field), the call site simply won't satisfy
// gqlErrItemPtr and won't compile — by design. Either reshape the operation
// to match, or write a dedicated handler at the call site.
func gqlMutationErrors[T any, PT gqlErrItemPtr[T]](errs []T) error {
	if len(errs) == 0 {
		return nil
	}
	out := make([]sleuthMutationError, len(errs))
	for i := range errs {
		var p PT = &errs[i]
		out[i] = sleuthMutationError{Field: p.GetField(), Messages: p.GetMessages()}
	}
	return sleuthMutationErrorsToErr(out)
}

func sleuthMutationErrorsToErr(errs []sleuthMutationError) error {
	if len(errs) == 0 {
		return nil
	}
	parts := make([]string, 0, len(errs))
	for _, e := range errs {
		parts = append(parts, fmt.Sprintf("%s: %s", e.Field, strings.Join(e.Messages, ", ")))
	}
	return errors.New(strings.Join(parts, "; "))
}

// teamGIDByName looks up a team's GID by name. Reuses the same query as
// ListTeams — no second round-trip schema.
func (s *SleuthVault) teamGIDByName(ctx context.Context, name string) (string, error) {
	nodes, err := s.listTeamNodes(ctx)
	if err != nil {
		return "", err
	}
	for _, n := range nodes {
		if n.Name == name {
			return n.ID, nil
		}
	}
	return "", mgmt.ErrTeamNotFound
}

// userGIDByEmail finds an org user's GID by email, via the users() search.
func (s *SleuthVault) userGIDByEmail(ctx context.Context, email string) (string, error) {
	normalized := mgmt.NormalizeEmail(email)
	resp, err := vaultgql.FindUser(ctx, s.gqlClient(), normalized)
	if err != nil {
		return "", err
	}
	for _, u := range resp.Organization.Users.Nodes {
		if mgmt.NormalizeEmail(u.Email) == normalized {
			return u.Id, nil
		}
	}
	return "", fmt.Errorf("user %q not found in org", email)
}

func (s *SleuthVault) resolveUserGIDs(ctx context.Context, emails []string) ([]string, error) {
	ids := make([]string, 0, len(emails))
	for _, e := range emails {
		gid, err := s.userGIDByEmail(ctx, e)
		if err != nil {
			return nil, err
		}
		ids = append(ids, gid)
	}
	return ids, nil
}

// setAssetInstallationsGraphQL calls the existing setAssetInstallations
// mutation. The server interprets inputs like this:
//   - personalOnly=true, repositories=[]  → USER (current caller only)
//   - personalOnly=false, repositories=[] → ORGANIZATION (global)
//   - personalOnly=false, repositories=[…] → REPOSITORY (scoped)
//
// WARNING: do not accidentally pass {repositories: [], personalOnly:
// false} from a caller that means "repo-scoped install but the slice is
// empty". Callers must route through SetAssetInstallation, which picks
// the right arguments per InstallKind and never collapses an intended
// repo/path/user install into the empty-empty shape.
func (s *SleuthVault) setAssetInstallationsGraphQL(ctx context.Context, assetName string, repositories []vaultgql.RepositoryInstallationInput, personalOnly bool) error {
	if repositories == nil {
		repositories = []vaultgql.RepositoryInstallationInput{}
	}
	resp, err := vaultgql.SetAssetInstallations(ctx, s.gqlClient(), vaultgql.SetAssetInstallationsInput{
		AssetName:    assetName,
		Repositories: repositories,
		PersonalOnly: &personalOnly,
	})
	if err != nil {
		return err
	}
	if resp.SetAssetInstallations == nil {
		return nil
	}
	return gqlMutationErrors(resp.SetAssetInstallations.Errors)
}

func timeOrNow(t time.Time) time.Time {
	if t.IsZero() {
		return time.Now().UTC()
	}
	return t
}
