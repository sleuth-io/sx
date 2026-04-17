package vault

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/sleuth-io/sx/internal/logger"
	"github.com/sleuth-io/sx/internal/mgmt"
)

// Sleuth vault management methods. These call the existing pulse GraphQL
// schema — no backend changes required. Operations that don't map cleanly
// to the current schema (most notably team-scoped installations) return a
// descriptive error pointing users at the skills.new web UI.

func (s *SleuthVault) CurrentActor(ctx context.Context) (mgmt.Actor, error) {
	query := `query { user { id username email firstName lastName } }`
	var resp struct {
		Data struct {
			User *struct {
				ID        string `json:"id"`
				Username  string `json:"username"`
				Email     string `json:"email"`
				FirstName string `json:"firstName"`
				LastName  string `json:"lastName"`
			} `json:"user"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := s.executeGraphQLQuery(ctx, query, nil, &resp); err != nil {
		return mgmt.Actor{}, err
	}
	if len(resp.Errors) > 0 {
		return mgmt.Actor{}, fmt.Errorf("graphql: %s", resp.Errors[0].Message)
	}
	if resp.Data.User == nil {
		return mgmt.Actor{}, errors.New("not authenticated")
	}
	name := strings.TrimSpace(resp.Data.User.FirstName + " " + resp.Data.User.LastName)
	if name == "" {
		name = resp.Data.User.Username
	}
	return mgmt.Actor{Email: mgmt.NormalizeEmail(resp.Data.User.Email), Name: name}, nil
}

func (s *SleuthVault) ListTeams(ctx context.Context) ([]mgmt.Team, error) {
	nodes, err := s.listTeamNodes(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]mgmt.Team, 0, len(nodes))
	for _, node := range nodes {
		out = append(out, sleuthTeamToMgmt(node))
	}
	return out, nil
}

// sleuthTeamListPageSize caps the per-page team and per-team-member fetch
// counts. Set high enough to cover every realistic org in a single call
// (the Sleuth API's internal default is ~200, and the largest observed
// deployments have well under 1000 teams). If a response saturates the
// cap we emit a warning so we notice truncation before users do; we do
// not silently paginate via cursor because the GraphQL schema does not
// currently expose pageInfo for these connections and blindly paging
// could infinite-loop against older server versions.
const sleuthTeamListPageSize = 1000

// listTeamNodes fetches the raw team nodes (including GIDs) from the
// server. ListTeams projects these to mgmt.Team; teamGIDByName scans for
// a single row — keeping a single query text in one place.
func (s *SleuthVault) listTeamNodes(ctx context.Context) ([]sleuthTeamNode, error) {
	query := `query ListTeams($first: Int!) {
		teams(first: $first) {
			nodes {
				id
				name
				adminMemberIds
				members(first: $first) { nodes { id email } }
				skillsRepositories { repositoryId }
			}
		}
	}`
	vars := map[string]any{"first": sleuthTeamListPageSize}
	var resp struct {
		Data struct {
			Teams struct {
				Nodes []sleuthTeamNode `json:"nodes"`
			} `json:"teams"`
		} `json:"data"`
		Errors []sleuthGraphQLError `json:"errors"`
	}
	if err := s.executeGraphQLQuery(ctx, query, vars, &resp); err != nil {
		return nil, err
	}
	if err := sleuthErrorsToErr(resp.Errors); err != nil {
		return nil, err
	}
	if len(resp.Data.Teams.Nodes) >= sleuthTeamListPageSize {
		logger.Get().Warn("ListTeams result saturated page size; some teams may be truncated",
			"page_size", sleuthTeamListPageSize,
			"returned", len(resp.Data.Teams.Nodes))
	}
	for _, node := range resp.Data.Teams.Nodes {
		if len(node.Members.Nodes) >= sleuthTeamListPageSize {
			logger.Get().Warn("team member list saturated page size; some members may be truncated",
				"team", node.Name,
				"page_size", sleuthTeamListPageSize,
				"returned", len(node.Members.Nodes))
		}
	}
	return resp.Data.Teams.Nodes, nil
}

func (s *SleuthVault) GetTeam(ctx context.Context, name string) (*mgmt.Team, error) {
	// GraphQL's team() takes an ID, not a name, so we fetch the full list
	// and filter. This is fine for typical orgs (tens of teams).
	teams, err := s.ListTeams(ctx)
	if err != nil {
		return nil, err
	}
	for i := range teams {
		if teams[i].Name == name {
			t := teams[i]
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
	mutation := `mutation CreateTeam($input: CreateTeamInput!) {
		createTeam(input: $input) { team { id name } errors { field messages } }
	}`
	vars := map[string]any{
		"input": map[string]any{
			"name":    team.Name,
			"members": memberIDs,
		},
	}
	var resp struct {
		Data struct {
			CreateTeam struct {
				Team   *sleuthTeamNode       `json:"team"`
				Errors []sleuthMutationError `json:"errors"`
			} `json:"createTeam"`
		} `json:"data"`
		Errors []sleuthGraphQLError `json:"errors"`
	}
	if err := s.executeGraphQLQuery(ctx, mutation, vars, &resp); err != nil {
		return err
	}
	if err := sleuthErrorsToErr(resp.Errors); err != nil {
		return err
	}
	return sleuthMutationErrorsToErr(resp.Data.CreateTeam.Errors)
}

func (s *SleuthVault) UpdateTeam(ctx context.Context, team mgmt.Team) error {
	existing, err := s.GetTeam(ctx, team.Name)
	if err != nil {
		return err
	}
	// We need to look up the Team by GID; the existing team has the GID as
	// the literal ID stored in mgmt.Team via lookups. Since mgmt.Team only
	// carries a name, not a GID, we fetch it via a second query that
	// returns the full node.
	gid, err := s.teamGIDByName(ctx, existing.Name)
	if err != nil {
		return err
	}
	memberIDs, err := s.resolveUserGIDs(ctx, team.Members)
	if err != nil {
		return err
	}
	mutation := `mutation UpdateTeam($input: UpdateTeamInput!) {
		updateTeam(input: $input) { team { id name } errors { field messages } }
	}`
	vars := map[string]any{
		"input": map[string]any{
			"id":      gid,
			"name":    team.Name,
			"members": memberIDs,
		},
	}
	return s.runMutation(ctx, mutation, vars, "updateTeam")
}

func (s *SleuthVault) DeleteTeam(ctx context.Context, name string) error {
	gid, err := s.teamGIDByName(ctx, name)
	if err != nil {
		return err
	}
	mutation := `mutation DeleteTeam($id: ID!) { deleteTeam(id: $id) { errors { field messages } } }`
	vars := map[string]any{"id": gid}
	return s.runMutation(ctx, mutation, vars, "deleteTeam")
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
	mutation := `mutation RemoveTeamMember($input: RemoveTeamMemberInput!) {
		removeTeamMember(input: $input) { errors { field messages } }
	}`
	vars := map[string]any{
		"input": map[string]any{"teamId": teamGID, "memberId": memberGID},
	}
	return s.runMutation(ctx, mutation, vars, "removeTeamMember")
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
	mutation := `mutation SetTeamAdmin($input: SetTeamAdminInput!) {
		setTeamAdmin(input: $input) { errors { field messages } }
	}`
	vars := map[string]any{
		"input": map[string]any{"teamId": teamGID, "userId": userGID, "isAdmin": admin},
	}
	return s.runMutation(ctx, mutation, vars, "setTeamAdmin")
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
		return s.setAssetInstallationsGraphQL(ctx, assetName, []map[string]any{{"url": target.Repo}}, false)
	case InstallKindPath:
		return s.setAssetInstallationsGraphQL(ctx, assetName, []map[string]any{{"url": target.Repo, "paths": target.Paths}}, false)
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
	}
	return fmt.Errorf("unknown install kind: %q", target.Kind)
}

func (s *SleuthVault) ClearAssetInstallations(ctx context.Context, assetName string) error {
	mutation := `mutation RemoveAssetInstallations($input: RemoveAssetInstallationsInput!) {
		removeAssetInstallations(input: $input) { success errors { field messages } }
	}`
	vars := map[string]any{"input": map[string]any{"assetName": assetName}}
	return s.runMutation(ctx, mutation, vars, "removeAssetInstallations")
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
	query := `query AssetAuditLog($first: Int) {
		assetAuditLog(first: $first) {
			nodes {
				id
				date
				actorEmail
				actorName
				event
				targetType
				targetName
				data
			}
		}
	}`
	// If the caller specified a limit, honor it directly; otherwise use
	// the wide default so client-side filters don't silently drop rows
	// that existed just beyond a small server page.
	userLimit := filter.Limit
	first := userLimit
	if first <= 0 {
		first = sleuthAuditDefaultPageSize
	}
	vars := map[string]any{"first": first}
	var resp struct {
		Data struct {
			AssetAuditLog struct {
				Nodes []struct {
					ID         string `json:"id"`
					Date       string `json:"date"`
					ActorEmail string `json:"actorEmail"`
					ActorName  string `json:"actorName"`
					Event      string `json:"event"`
					TargetType string `json:"targetType"`
					TargetName string `json:"targetName"`
					Data       any    `json:"data"`
				} `json:"nodes"`
			} `json:"assetAuditLog"`
		} `json:"data"`
		Errors []sleuthGraphQLError `json:"errors"`
	}
	if err := s.executeGraphQLQuery(ctx, query, vars, &resp); err != nil {
		return nil, err
	}
	if err := sleuthErrorsToErr(resp.Errors); err != nil {
		return nil, err
	}
	// Warn when the server returned exactly the page we asked for and the
	// caller didn't choose that limit themselves — a saturation signal
	// that older entries may have been dropped before client-side
	// filtering ran.
	if userLimit == 0 && len(resp.Data.AssetAuditLog.Nodes) >= first {
		logger.Get().Warn("QueryAuditEvents result saturated page size; older events may be truncated",
			"page_size", first,
			"returned", len(resp.Data.AssetAuditLog.Nodes))
	}
	var out []mgmt.AuditEvent
	for _, node := range resp.Data.AssetAuditLog.Nodes {
		ts, _ := time.Parse(time.RFC3339, node.Date)
		ev := mgmt.AuditEvent{
			Timestamp:  ts,
			Actor:      node.ActorEmail,
			Event:      node.Event,
			TargetType: node.TargetType,
			Target:     node.TargetName,
		}
		if m, ok := node.Data.(map[string]any); ok {
			ev.Data = m
		}
		if !sleuthAuditMatches(ev, filter) {
			continue
		}
		out = append(out, ev)
	}
	return out, nil
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

type sleuthTeamNode struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	AdminMemberIDs []string `json:"adminMemberIds"`
	Members        struct {
		Nodes []struct {
			ID    string `json:"id"`
			Email string `json:"email"`
		} `json:"nodes"`
	} `json:"members"`
	SkillsRepositories []struct {
		RepositoryID string `json:"repositoryId"`
	} `json:"skillsRepositories"`
}

type sleuthGraphQLError struct {
	Message string `json:"message"`
}

type sleuthMutationError struct {
	Field    string   `json:"field"`
	Messages []string `json:"messages"`
}

func sleuthTeamToMgmt(node sleuthTeamNode) mgmt.Team {
	team := mgmt.Team{Name: node.Name}
	for _, m := range node.Members.Nodes {
		team.Members = append(team.Members, mgmt.NormalizeEmail(m.Email))
	}
	adminIDs := make(map[string]struct{}, len(node.AdminMemberIDs))
	for _, id := range node.AdminMemberIDs {
		adminIDs[id] = struct{}{}
	}
	for _, m := range node.Members.Nodes {
		if _, ok := adminIDs[m.ID]; ok {
			team.Admins = append(team.Admins, mgmt.NormalizeEmail(m.Email))
		}
	}
	for _, r := range node.SkillsRepositories {
		team.Repositories = append(team.Repositories, r.RepositoryID)
	}
	return team
}

func sleuthErrorsToErr(errs []sleuthGraphQLError) error {
	if len(errs) == 0 {
		return nil
	}
	msgs := make([]string, len(errs))
	for i, e := range errs {
		msgs[i] = e.Message
	}
	return fmt.Errorf("graphql: %s", strings.Join(msgs, "; "))
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

// runMutation executes a mutation that has a {errors { field messages }}
// payload under the named root field, decoding and surfacing errors.
// A payload shape that no longer matches the expected schema (e.g. the
// server renamed the errors field) is propagated as an error rather
// than silently swallowed — silently dropping schema drift here would
// turn a failed mutation into an apparent success.
func (s *SleuthVault) runMutation(ctx context.Context, mutation string, vars map[string]any, rootField string) error {
	raw := json.RawMessage{}
	resp := struct {
		Data   map[string]json.RawMessage `json:"data"`
		Errors []sleuthGraphQLError       `json:"errors"`
	}{Data: map[string]json.RawMessage{rootField: raw}}
	if err := s.executeGraphQLQuery(ctx, mutation, vars, &resp); err != nil {
		return err
	}
	if err := sleuthErrorsToErr(resp.Errors); err != nil {
		return err
	}
	payload, ok := resp.Data[rootField]
	if !ok || len(payload) == 0 {
		return nil
	}
	var errEnv struct {
		Errors []sleuthMutationError `json:"errors"`
	}
	if err := json.Unmarshal(payload, &errEnv); err != nil {
		return fmt.Errorf("unexpected %s mutation response shape: %w", rootField, err)
	}
	return sleuthMutationErrorsToErr(errEnv.Errors)
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
	query := `query FindUser($term: String!) {
		users(term: $term, first: 25) { nodes { id email } }
	}`
	normalized := mgmt.NormalizeEmail(email)
	vars := map[string]any{"term": normalized}
	var resp struct {
		Data struct {
			Users struct {
				Nodes []struct {
					ID    string `json:"id"`
					Email string `json:"email"`
				} `json:"nodes"`
			} `json:"users"`
		} `json:"data"`
		Errors []sleuthGraphQLError `json:"errors"`
	}
	if err := s.executeGraphQLQuery(ctx, query, vars, &resp); err != nil {
		return "", err
	}
	if err := sleuthErrorsToErr(resp.Errors); err != nil {
		return "", err
	}
	for _, u := range resp.Data.Users.Nodes {
		if mgmt.NormalizeEmail(u.Email) == normalized {
			return u.ID, nil
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
func (s *SleuthVault) setAssetInstallationsGraphQL(ctx context.Context, assetName string, repositories []map[string]any, personalOnly bool) error {
	mutation := `mutation SetAssetInstallations($input: SetAssetInstallationsInput!) {
		setAssetInstallations(input: $input) {
			asset { name }
			errors { field messages }
		}
	}`
	if repositories == nil {
		repositories = []map[string]any{}
	}
	vars := map[string]any{
		"input": map[string]any{
			"assetName":    assetName,
			"repositories": repositories,
			"personalOnly": personalOnly,
		},
	}
	return s.runMutation(ctx, mutation, vars, "setAssetInstallations")
}

func timeOrNow(t time.Time) time.Time {
	if t.IsZero() {
		return time.Now().UTC()
	}
	return t
}
