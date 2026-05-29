package vault

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/manifest"
	"github.com/sleuth-io/sx/internal/mgmt"
)

// TestPathVault_TeamUserLifecycleE2E exercises the full management flow
// on a path vault: seed sx.toml, create a team, add members, install an
// asset to both a team and a user, record usage events, then assert the
// queried state. Mirrors the real-world CLI flow without spawning `sx`.
func TestPathVault_TeamUserLifecycleE2E(t *testing.T) {
	mgmt.ResetActorCache()
	dir := t.TempDir()

	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "alice@example.com")
	runGit(t, dir, "config", "user.name", "Alice Admin")

	if err := manifest.Save(dir, &manifest.Manifest{
		SchemaVersion: manifest.CurrentSchemaVersion,
		Assets: []manifest.Asset{
			{
				Name:       "my-skill",
				Version:    "1.0.0",
				Type:       asset.TypeSkill,
				SourceHTTP: &manifest.SourceHTTP{URL: "https://example.com/my-skill.zip"},
				Scopes: []manifest.Scope{
					{Kind: manifest.ScopeKindRepo, Repo: "github.com/acme/baseline"},
				},
			},
			{
				Name:       "other-skill",
				Version:    "1.0.0",
				Type:       asset.TypeRule,
				SourceHTTP: &manifest.SourceHTTP{URL: "https://example.com/other.zip"},
			},
		},
	}); err != nil {
		t.Fatalf("seed manifest: %v", err)
	}

	v, err := NewPathVault("file://" + dir)
	if err != nil {
		t.Fatalf("NewPathVault failed: %v", err)
	}

	ctx := context.Background()

	// 1. Create a team with two members, one admin (alice).
	team := mgmt.Team{
		Name:         "platform",
		Description:  "Platform eng",
		Members:      []string{"alice@example.com", "bob@example.com"},
		Admins:       []string{"alice@example.com"},
		Repositories: []string{"https://github.com/acme/infra.git"},
	}
	if err := v.CreateTeam(ctx, team); err != nil {
		t.Fatalf("CreateTeam failed: %v", err)
	}

	// 2. List teams and verify.
	teamResult, err := v.ListTeams(ctx, ListTeamsOptions{})
	if err != nil {
		t.Fatalf("ListTeams failed: %v", err)
	}
	if len(teamResult.Teams) != 1 || teamResult.Teams[0].Name != "platform" {
		t.Fatalf("expected 1 team 'platform', got %+v", teamResult.Teams)
	}
	if len(teamResult.Teams[0].Members) != 2 {
		t.Errorf("expected 2 members, got %d", len(teamResult.Teams[0].Members))
	}

	// 3. Add a second repository via AddTeamRepository.
	if err := v.AddTeamRepository(ctx, "platform", "https://github.com/acme/tools.git"); err != nil {
		t.Fatalf("AddTeamRepository failed: %v", err)
	}

	// 4. Install the asset to the team and to alice.
	if err := v.SetAssetInstallation(ctx, "my-skill", InstallTarget{Kind: InstallKindTeam, Team: "platform"}); err != nil {
		t.Fatalf("SetAssetInstallation (team) failed: %v", err)
	}
	if err := v.SetAssetInstallation(ctx, "my-skill", InstallTarget{Kind: InstallKindUser, User: "alice@example.com"}); err != nil {
		t.Fatalf("SetAssetInstallation (user) failed: %v", err)
	}

	// 5. Fetch the lock file and verify resolution: alice is the user-
	// scoped target, which marks the asset global (empty Scopes wins
	// over team expansion).
	raw, _, _, err := v.GetLockFile(ctx, "")
	if err != nil {
		t.Fatalf("GetLockFile failed: %v", err)
	}
	lf, err := lockfile.Parse(raw)
	if err != nil {
		t.Fatalf("parse lock: %v", err)
	}
	if mySkill := findLockAsset(lf, "my-skill"); mySkill == nil {
		t.Fatal("my-skill not found in resolved lock file")
	} else if len(mySkill.Scopes) != 0 {
		t.Errorf("expected my-skill to be global (user scope wins), got scopes %+v", mySkill.Scopes)
	}

	// 6. Clear and re-install to just the team — alice should now see
	// the team repos in her lock file.
	if err := v.ClearAssetInstallations(ctx, "my-skill"); err != nil {
		t.Fatalf("ClearAssetInstallations failed: %v", err)
	}
	// Restore a baseline repo scope so the team overlay lands on top of
	// an existing non-empty scope list.
	m, _, err := manifest.LoadOrMigrate(dir)
	if err != nil {
		t.Fatalf("reload manifest: %v", err)
	}
	if asset := m.FindAsset("my-skill"); asset != nil {
		asset.Scopes = []manifest.Scope{
			{Kind: manifest.ScopeKindRepo, Repo: "github.com/acme/baseline"},
		}
	}
	if err := manifest.Save(dir, m); err != nil {
		t.Fatalf("save manifest after reseed: %v", err)
	}
	if err := v.SetAssetInstallation(ctx, "my-skill", InstallTarget{Kind: InstallKindTeam, Team: "platform"}); err != nil {
		t.Fatalf("re-install team failed: %v", err)
	}

	raw, _, _, err = v.GetLockFile(ctx, "")
	if err != nil {
		t.Fatalf("GetLockFile failed: %v", err)
	}
	lf, err = lockfile.Parse(raw)
	if err != nil {
		t.Fatalf("parse lock: %v", err)
	}
	if mySkill := findLockAsset(lf, "my-skill"); mySkill == nil {
		t.Fatal("my-skill missing from re-installed lock")
	} else if len(mySkill.Scopes) < 2 {
		t.Errorf("expected resolved scopes to include team repos, got %+v", mySkill.Scopes)
	}

	// 7. Record usage events.
	events := []mgmt.UsageEvent{
		{Actor: "alice@example.com", AssetName: "my-skill", AssetVersion: "1.0.0", AssetType: "skill"},
		{Actor: "bob@example.com", AssetName: "my-skill", AssetVersion: "1.0.0", AssetType: "skill"},
		{Actor: "alice@example.com", AssetName: "other-skill", AssetVersion: "1.0.0", AssetType: "rule"},
	}
	if err := v.RecordUsageEvents(ctx, events); err != nil {
		t.Fatalf("RecordUsageEvents failed: %v", err)
	}

	summary, err := v.GetUsageStats(ctx, mgmt.UsageFilter{})
	if err != nil {
		t.Fatalf("GetUsageStats failed: %v", err)
	}
	if summary.TotalEvents != 3 {
		t.Errorf("expected 3 events, got %d", summary.TotalEvents)
	}
	if len(summary.PerAsset) < 1 || summary.PerAsset[0].AssetName != "my-skill" {
		t.Errorf("expected my-skill first, got %+v", summary.PerAsset)
	}
	if summary.PerAsset[0].UniqueActors != 2 {
		t.Errorf("expected 2 unique actors for my-skill, got %d", summary.PerAsset[0].UniqueActors)
	}

	// 8. Audit events should include team create, repo add, install set,
	// and install cleared.
	events2, err := v.QueryAuditEvents(ctx, mgmt.AuditFilter{})
	if err != nil {
		t.Fatalf("QueryAuditEvents failed: %v", err)
	}
	wantEvents := []string{
		mgmt.EventInstallSet,
		mgmt.EventInstallCleared,
		mgmt.EventTeamRepoAdded,
		mgmt.EventTeamCreated,
	}
	for _, want := range wantEvents {
		if !anyAuditEvent(events2, want) {
			t.Errorf("expected audit event %q not found", want)
		}
	}

	// 9. Delete the team and verify cascade removal of team-scoped
	// installs from the manifest.
	if err := v.DeleteTeam(ctx, "platform"); err != nil {
		t.Fatalf("DeleteTeam failed: %v", err)
	}
	teamResult, err = v.ListTeams(ctx, ListTeamsOptions{})
	if err != nil {
		t.Fatalf("ListTeams after delete failed: %v", err)
	}
	if len(teamResult.Teams) != 0 {
		t.Errorf("expected no teams, got %d", len(teamResult.Teams))
	}

	m, _, err = manifest.LoadOrMigrate(dir)
	if err != nil {
		t.Fatalf("reload manifest after delete: %v", err)
	}
	for _, asset := range m.Assets {
		for _, s := range asset.Scopes {
			if s.Kind == manifest.ScopeKindTeam && s.Team == "platform" {
				t.Errorf("expected team scope to be cascade-deleted from %s, still found: %+v", asset.Name, s)
			}
		}
	}

	// 10. Cascade delete should emit install.cleared audit events per
	// asset so auditors can reconstruct why an asset stopped installing.
	events3, err := v.QueryAuditEvents(ctx, mgmt.AuditFilter{})
	if err != nil {
		t.Fatalf("QueryAuditEvents after delete failed: %v", err)
	}
	foundCascade := false
	for _, ev := range events3 {
		if ev.Event != mgmt.EventInstallCleared {
			continue
		}
		if ev.Target != "my-skill" {
			continue
		}
		if reason, _ := ev.Data["reason"].(string); reason == "team_deleted" {
			foundCascade = true
			break
		}
	}
	if !foundCascade {
		t.Error("expected install.cleared audit event with reason=team_deleted for my-skill after team delete")
	}
}

// TestPathVault_PathInstall_OrderInsensitive verifies that a path-scoped
// install can be removed regardless of the order the caller lists the
// paths in. Set and remove both canonicalize (sort) the path list, so
// Set(["docs","api"]) is removed by Remove(["api","docs"]).
func TestPathVault_PathInstall_OrderInsensitive(t *testing.T) {
	mgmt.ResetActorCache()
	dir := t.TempDir()

	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "alice@example.com")
	runGit(t, dir, "config", "user.name", "Alice")

	if err := manifest.Save(dir, &manifest.Manifest{
		SchemaVersion: manifest.CurrentSchemaVersion,
		Assets: []manifest.Asset{
			{
				Name:       "my-skill",
				Version:    "1.0.0",
				Type:       asset.TypeSkill,
				SourceHTTP: &manifest.SourceHTTP{URL: "https://example.com/my-skill.zip"},
				// A baseline repo scope so the asset row survives after the
				// path scope is removed (a row left with zero scopes is dropped).
				Scopes: []manifest.Scope{
					{Kind: manifest.ScopeKindRepo, Repo: "github.com/acme/baseline"},
				},
			},
		},
	}); err != nil {
		t.Fatalf("seed manifest: %v", err)
	}
	v, err := NewPathVault("file://" + dir)
	if err != nil {
		t.Fatalf("NewPathVault: %v", err)
	}
	ctx := context.Background()
	repo := "github.com/acme/infra"

	if err := v.SetAssetInstallation(ctx, "my-skill", InstallTarget{
		Kind:  InstallKindPath,
		Repo:  repo,
		Paths: []string{"docs", "api"},
	}); err != nil {
		t.Fatalf("SetAssetInstallation: %v", err)
	}

	// Remove with the paths in the opposite order — must still match the
	// stored row and drop the scope.
	if err := v.RemoveAssetInstallation(ctx, "my-skill", InstallTarget{
		Kind:  InstallKindPath,
		Repo:  repo,
		Paths: []string{"api", "docs"},
	}); err != nil {
		t.Fatalf("RemoveAssetInstallation: %v", err)
	}

	m, _, err := manifest.LoadOrMigrate(dir)
	if err != nil {
		t.Fatalf("reload manifest: %v", err)
	}
	found := m.FindAsset("my-skill")
	if found == nil {
		t.Fatal("my-skill missing from manifest")
	}
	for _, s := range found.Scopes {
		if s.Kind == manifest.ScopeKindPath {
			t.Fatalf("path scope was not removed despite reordered paths: %+v", s)
		}
	}
	// The baseline repo scope must remain — only the path scope is removed.
	if len(found.Scopes) != 1 || found.Scopes[0].Kind != manifest.ScopeKindRepo {
		t.Fatalf("expected only the baseline repo scope to remain, got %+v", found.Scopes)
	}
}

// TestPathVault_PathInstall_LegacyUnsortedRow verifies a path scope stored
// in unsorted order — as an older sx version or a hand edit of sx.toml
// would leave it — is still matched on both set (no duplicate appended) and
// remove. This exercises the comparison-site canonicalization rather than
// the in-version round trip.
func TestPathVault_PathInstall_LegacyUnsortedRow(t *testing.T) {
	mgmt.ResetActorCache()
	dir := t.TempDir()

	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "alice@example.com")
	runGit(t, dir, "config", "user.name", "Alice")

	repo := "github.com/acme/infra"
	if err := manifest.Save(dir, &manifest.Manifest{
		SchemaVersion: manifest.CurrentSchemaVersion,
		Assets: []manifest.Asset{
			{
				Name:       "my-skill",
				Version:    "1.0.0",
				Type:       asset.TypeSkill,
				SourceHTTP: &manifest.SourceHTTP{URL: "https://example.com/my-skill.zip"},
				// A path scope stored in unsorted order, as a legacy/hand
				// edit would leave it, plus a baseline so the row survives.
				Scopes: []manifest.Scope{
					{Kind: manifest.ScopeKindRepo, Repo: "github.com/acme/baseline"},
					{Kind: manifest.ScopeKindPath, Repo: repo, Paths: []string{"docs", "api"}},
				},
			},
		},
	}); err != nil {
		t.Fatalf("seed manifest: %v", err)
	}
	v, err := NewPathVault("file://" + dir)
	if err != nil {
		t.Fatalf("NewPathVault: %v", err)
	}
	ctx := context.Background()

	// Set with the same paths sorted differently must NOT append a second
	// row — the legacy unsorted row should be recognized as already present.
	if err := v.SetAssetInstallation(ctx, "my-skill", InstallTarget{
		Kind:  InstallKindPath,
		Repo:  repo,
		Paths: []string{"api", "docs"},
	}); err != nil {
		t.Fatalf("SetAssetInstallation: %v", err)
	}
	m, _, err := manifest.LoadOrMigrate(dir)
	if err != nil {
		t.Fatalf("reload manifest after set: %v", err)
	}
	if pathScopes := countPathScopes(m.FindAsset("my-skill")); pathScopes != 1 {
		t.Fatalf("expected the legacy row to be deduped (1 path scope), got %d", pathScopes)
	}

	// Remove with yet another order must match the legacy row and drop it.
	if err := v.RemoveAssetInstallation(ctx, "my-skill", InstallTarget{
		Kind:  InstallKindPath,
		Repo:  repo,
		Paths: []string{"api", "docs"},
	}); err != nil {
		t.Fatalf("RemoveAssetInstallation: %v", err)
	}
	m, _, err = manifest.LoadOrMigrate(dir)
	if err != nil {
		t.Fatalf("reload manifest after remove: %v", err)
	}
	if pathScopes := countPathScopes(m.FindAsset("my-skill")); pathScopes != 0 {
		t.Fatalf("legacy unsorted path scope was not removed, %d remain", pathScopes)
	}
}

// TestPathVault_RemoveOrgInstall_Rejected verifies removing an org-wide
// install returns ErrNotImplemented rather than silently no-opping — an
// org install is stored as an empty scope list, so there is no row to
// match and callers must use ClearAssetInstallations instead.
func TestPathVault_RemoveOrgInstall_Rejected(t *testing.T) {
	mgmt.ResetActorCache()
	dir := t.TempDir()

	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "alice@example.com")
	runGit(t, dir, "config", "user.name", "Alice")

	if err := manifest.Save(dir, &manifest.Manifest{
		SchemaVersion: manifest.CurrentSchemaVersion,
		Assets: []manifest.Asset{
			{
				Name:       "my-skill",
				Version:    "1.0.0",
				Type:       asset.TypeSkill,
				SourceHTTP: &manifest.SourceHTTP{URL: "https://example.com/my-skill.zip"},
			},
		},
	}); err != nil {
		t.Fatalf("seed manifest: %v", err)
	}
	v, err := NewPathVault("file://" + dir)
	if err != nil {
		t.Fatalf("NewPathVault: %v", err)
	}
	ctx := context.Background()

	err = v.RemoveAssetInstallation(ctx, "my-skill", InstallTarget{Kind: InstallKindOrg})
	if !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("RemoveAssetInstallation org = %v, want ErrNotImplemented", err)
	}
	// The message must keep explaining how to actually stop distributing a
	// globally-installed asset, not just return a bare sentinel.
	if !strings.Contains(err.Error(), "remove the asset") {
		t.Fatalf("error %q should tell the caller to remove the asset from the vault", err)
	}
}

// TestPathVault_RemoveTeamInstall_RequiresAdminEvenOnNoOp pins the
// documented contract that the team-admin precondition in
// RemoveAssetInstallation runs unconditionally — before the scope walk
// decides nothing changed. A non-admin removing a team install that
// doesn't even exist still gets the admin error, not a silent no-op.
func TestPathVault_RemoveTeamInstall_RequiresAdminEvenOnNoOp(t *testing.T) {
	mgmt.ResetActorCache()
	dir := t.TempDir()

	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "alice@example.com")
	runGit(t, dir, "config", "user.name", "Alice")

	if err := manifest.Save(dir, &manifest.Manifest{
		SchemaVersion: manifest.CurrentSchemaVersion,
		Assets: []manifest.Asset{
			{
				Name:       "my-skill",
				Version:    "1.0.0",
				Type:       asset.TypeSkill,
				SourceHTTP: &manifest.SourceHTTP{URL: "https://example.com/my-skill.zip"},
			},
		},
	}); err != nil {
		t.Fatalf("seed manifest: %v", err)
	}
	v, err := NewPathVault("file://" + dir)
	if err != nil {
		t.Fatalf("NewPathVault: %v", err)
	}
	ctx := context.Background()

	// Team whose only admin is bob — the caller (alice) is not an admin.
	if err := v.CreateTeam(ctx, mgmt.Team{
		Name:    "platform",
		Members: []string{"bob@example.com"},
		Admins:  []string{"bob@example.com"},
	}); err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}

	// my-skill has no team install, so the scope walk would no-op — but the
	// admin check must still fire first and reject the non-admin caller.
	err = v.RemoveAssetInstallation(ctx, "my-skill", InstallTarget{Kind: InstallKindTeam, Team: "platform"})
	if err == nil || !strings.Contains(err.Error(), "not an admin") {
		t.Fatalf("RemoveAssetInstallation by non-admin = %v, want team-admin error", err)
	}
}

func countPathScopes(a *manifest.Asset) int {
	if a == nil {
		return 0
	}
	n := 0
	for _, s := range a.Scopes {
		if s.Kind == manifest.ScopeKindPath {
			n++
		}
	}
	return n
}

// TestPathVault_BotUpdate_PreservesTeams pins the contract that a
// description-only update (Teams = nil) leaves existing team
// memberships intact. The CLI's newBotUpdateCommand sets Teams to nil
// to avoid the read-modify-write race that would clobber concurrent
// `bot team add/remove` calls on Sleuth — the file-vault path has to
// honor the same "nil means don't touch" semantics or it silently
// wipes memberships on every description edit.
func TestPathVault_BotUpdate_PreservesTeams(t *testing.T) {
	mgmt.ResetActorCache()
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "alice@example.com")
	runGit(t, dir, "config", "user.name", "Alice")

	if err := manifest.Save(dir, &manifest.Manifest{SchemaVersion: manifest.CurrentSchemaVersion}); err != nil {
		t.Fatalf("seed manifest: %v", err)
	}
	v, err := NewPathVault("file://" + dir)
	if err != nil {
		t.Fatalf("NewPathVault: %v", err)
	}
	ctx := context.Background()

	if err := v.CreateTeam(ctx, mgmt.Team{
		Name: "platform", Members: []string{"alice@example.com"}, Admins: []string{"alice@example.com"},
	}); err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	if _, err := v.CreateBot(ctx, mgmt.Bot{Name: "ci", Teams: []string{"platform"}}); err != nil {
		t.Fatalf("CreateBot: %v", err)
	}

	// Description-only update: Teams left nil to mean "don't touch".
	if err := v.UpdateBot(ctx, mgmt.Bot{Name: "ci", Description: "updated"}); err != nil {
		t.Fatalf("UpdateBot: %v", err)
	}

	bot, err := v.GetBot(ctx, "ci")
	if err != nil {
		t.Fatalf("GetBot: %v", err)
	}
	if !bot.IsOnTeam("platform") {
		t.Errorf("description-only update wiped team memberships: got Teams=%v", bot.Teams)
	}
	if bot.Description != "updated" {
		t.Errorf("description not updated: got %q", bot.Description)
	}
}

// TestPathVault_TeamDeleteCascadesToBots verifies that deleting a team
// strips its name from every bot's Teams slice (the invariant "every
// entry in Bot.Teams references an existing team") and emits an audit
// event per affected bot.
func TestPathVault_TeamDeleteCascadesToBots(t *testing.T) {
	mgmt.ResetActorCache()
	dir := t.TempDir()

	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "alice@example.com")
	runGit(t, dir, "config", "user.name", "Alice Admin")

	if err := manifest.Save(dir, &manifest.Manifest{
		SchemaVersion: manifest.CurrentSchemaVersion,
	}); err != nil {
		t.Fatalf("seed manifest: %v", err)
	}

	v, err := NewPathVault("file://" + dir)
	if err != nil {
		t.Fatalf("NewPathVault: %v", err)
	}
	ctx := context.Background()

	if err := v.CreateTeam(ctx, mgmt.Team{
		Name:    "platform",
		Members: []string{"alice@example.com"},
		Admins:  []string{"alice@example.com"},
	}); err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	if _, err := v.CreateBot(ctx, mgmt.Bot{Name: "ci-bot", Teams: []string{"platform"}}); err != nil {
		t.Fatalf("CreateBot: %v", err)
	}

	if err := v.DeleteTeam(ctx, "platform"); err != nil {
		t.Fatalf("DeleteTeam: %v", err)
	}

	bot, err := v.GetBot(ctx, "ci-bot")
	if err != nil {
		t.Fatalf("GetBot after team delete: %v", err)
	}
	if bot.IsOnTeam("platform") {
		t.Errorf("bot.Teams still contains deleted team: %v", bot.Teams)
	}

	events, err := v.QueryAuditEvents(ctx, mgmt.AuditFilter{EventPrefix: "bot."})
	if err != nil {
		t.Fatalf("QueryAuditEvents: %v", err)
	}
	var sawCascade bool
	for _, ev := range events {
		if ev.Event != mgmt.EventBotTeamRemoved {
			continue
		}
		if ev.Target != "ci-bot" {
			continue
		}
		if reason, _ := ev.Data["reason"].(string); reason == "team_deleted" {
			sawCascade = true
			break
		}
	}
	if !sawCascade {
		t.Errorf("expected bot.team_removed audit event with reason=team_deleted; got events=%+v", events)
	}
}

// TestPathVault_BotLifecycleE2E exercises the full bot management flow
// on a path vault: create a team, create a bot, add it to the team,
// install assets directly to the bot, and verify SX_BOT-mode resolution
// returns the right asset set (direct + team + org-wide; not user-only).
func TestPathVault_BotLifecycleE2E(t *testing.T) {
	mgmt.ResetActorCache()
	dir := t.TempDir()

	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "alice@example.com")
	runGit(t, dir, "config", "user.name", "Alice Admin")

	if err := manifest.Save(dir, &manifest.Manifest{
		SchemaVersion: manifest.CurrentSchemaVersion,
		Assets: []manifest.Asset{
			{
				Name: "direct", Version: "1.0.0", Type: asset.TypeSkill,
				SourceHTTP: &manifest.SourceHTTP{URL: "https://example.com/d.zip"},
			},
			{
				Name: "team-only", Version: "1.0.0", Type: asset.TypeSkill,
				SourceHTTP: &manifest.SourceHTTP{URL: "https://example.com/t.zip"},
			},
			{
				Name: "global", Version: "1.0.0", Type: asset.TypeSkill,
				SourceHTTP: &manifest.SourceHTTP{URL: "https://example.com/g.zip"},
				// Empty Scopes is org-wide.
			},
			{
				Name: "user-only", Version: "1.0.0", Type: asset.TypeSkill,
				SourceHTTP: &manifest.SourceHTTP{URL: "https://example.com/u.zip"},
				Scopes: []manifest.Scope{
					{Kind: manifest.ScopeKindUser, User: "alice@example.com"},
				},
			},
		},
	}); err != nil {
		t.Fatalf("seed manifest: %v", err)
	}

	v, err := NewPathVault("file://" + dir)
	if err != nil {
		t.Fatalf("NewPathVault failed: %v", err)
	}
	ctx := context.Background()

	// 1. Create a team and a bot.
	if err := v.CreateTeam(ctx, mgmt.Team{
		Name:         "platform",
		Members:      []string{"alice@example.com"},
		Admins:       []string{"alice@example.com"},
		Repositories: []string{"https://github.com/acme/infra.git"},
	}); err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	if rawToken, err := v.CreateBot(ctx, mgmt.Bot{
		Name:        "python-backend",
		Description: "Backend CI bot",
	}); err != nil {
		t.Fatalf("CreateBot: %v", err)
	} else if rawToken != "" {
		t.Errorf("path vault should never auto-issue a bot key; got token of length %d", len(rawToken))
	}

	// 2. Add the bot to the team.
	if err := v.AddBotTeam(ctx, "python-backend", "platform"); err != nil {
		t.Fatalf("AddBotTeam: %v", err)
	}
	bot, err := v.GetBot(ctx, "python-backend")
	if err != nil {
		t.Fatalf("GetBot: %v", err)
	}
	if !bot.IsOnTeam("platform") {
		t.Errorf("bot teams: got %v, want platform present", bot.Teams)
	}

	// 3. Install one asset directly to the bot, one to the team.
	if err := v.SetAssetInstallation(ctx, "direct", InstallTarget{Kind: InstallKindBot, Bot: "python-backend"}); err != nil {
		t.Fatalf("SetAssetInstallation (bot): %v", err)
	}
	if err := v.SetAssetInstallation(ctx, "team-only", InstallTarget{Kind: InstallKindTeam, Team: "platform"}); err != nil {
		t.Fatalf("SetAssetInstallation (team): %v", err)
	}

	bots, err := v.ListBots(ctx)
	if err != nil {
		t.Fatalf("ListBots: %v", err)
	}
	if len(bots) != 1 {
		t.Fatalf("ListBots returned %d bots, want 1", len(bots))
	}
	// The helper returns InstalledSkills sorted, so pin the exact set so a
	// future regression that adds duplicates or includes other-bot/user
	// skills is caught — not just the contains/not-contains pattern.
	wantSkills := []mgmt.BotSkill{
		{Name: "direct", IsDirectInstall: true},
		{Name: "global", IsDirectInstall: false},
		{Name: "team-only", IsDirectInstall: false},
	}
	if !slices.Equal(bots[0].InstalledSkills, wantSkills) {
		t.Errorf("bot[0].InstalledSkills = %v, want %v", bots[0].InstalledSkills, wantSkills)
	}

	// 4. Resolve the lock file as the bot identity (SX_BOT).
	mgmt.ResetActorCache()
	t.Setenv("SX_BOT", "python-backend")
	raw, _, _, err := v.GetLockFile(ctx, "")
	if err != nil {
		t.Fatalf("GetLockFile: %v", err)
	}
	lf, err := lockfile.Parse(raw)
	if err != nil {
		t.Fatalf("lockfile.Parse: %v", err)
	}

	got := make(map[string]bool, len(lf.Assets))
	for _, a := range lf.Assets {
		got[a.Name] = true
	}

	for _, name := range []string{"direct", "team-only", "global"} {
		if !got[name] {
			t.Errorf("bot caller missing expected asset %s", name)
		}
	}
	if got["user-only"] {
		t.Errorf("bot caller should not see user-only asset")
	}

	// 5. Bot identities are read-only: trying to mutate must fail.
	mgmt.ResetActorCache()
	t.Setenv("SX_BOT", "python-backend")
	if _, err := v.CreateBot(ctx, mgmt.Bot{Name: "another"}); err == nil {
		t.Error("bot identity should not be allowed to mutate vault state")
	}

	// 6. Deleting the bot must not promote bot-only assets to global. The
	// manifest represents global install as an asset with no scopes, so a
	// bot-delete cascade has to drop entries that were only scoped to the
	// deleted bot.
	mgmt.ResetActorCache()
	t.Setenv("SX_BOT", "")
	if err := v.DeleteBot(ctx, "python-backend"); err != nil {
		t.Fatalf("DeleteBot: %v", err)
	}
	updated, _, err := manifest.Load(dir)
	if err != nil {
		t.Fatalf("load manifest after DeleteBot: %v", err)
	}
	if direct := updated.FindAsset("direct"); direct != nil {
		t.Fatalf("bot-only asset survived DeleteBot with scopes %+v; empty scopes would install it globally", direct.Scopes)
	}
	if teamOnly := updated.FindAsset("team-only"); teamOnly == nil {
		t.Fatal("team-scoped asset should survive DeleteBot")
	}
	if global := updated.FindAsset("global"); global == nil || len(global.Scopes) != 0 {
		t.Fatalf("global asset after DeleteBot = %+v, want retained global asset", global)
	}
}

func TestPathVault_SetAssetInstallationRepairsOrphanedStoredAsset(t *testing.T) {
	mgmt.ResetActorCache()
	dir := t.TempDir()

	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "alice@example.com")
	runGit(t, dir, "config", "user.name", "Alice Admin")

	if err := manifest.Save(dir, &manifest.Manifest{
		SchemaVersion: manifest.CurrentSchemaVersion,
		Bots: []manifest.Bot{
			{Name: "testy", Description: "Test bot"},
		},
	}); err != nil {
		t.Fatalf("seed manifest: %v", err)
	}

	assetDir := filepath.Join(dir, "assets", "e2e-git-vault-skill-5524", "1")
	if err := os.MkdirAll(assetDir, 0755); err != nil {
		t.Fatalf("mkdir asset: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "e2e-git-vault-skill-5524", "list.txt"), []byte("1\n"), 0644); err != nil {
		t.Fatalf("write list: %v", err)
	}
	if err := os.WriteFile(filepath.Join(assetDir, "SKILL.md"), []byte("---\nname: e2e-git-vault-skill-5524\ndescription: Test skill.\n---\n\n# Test\n"), 0644); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(assetDir, "metadata.toml"), []byte(`metadata-version = "1.0"

[asset]
  name = "e2e-git-vault-skill-5524"
  version = "1"
  type = "skill"

[skill]
  prompt-file = "SKILL.md"
`), 0644); err != nil {
		t.Fatalf("write metadata: %v", err)
	}

	v, err := NewPathVault("file://" + dir)
	if err != nil {
		t.Fatalf("NewPathVault: %v", err)
	}
	if err := v.SetAssetInstallation(context.Background(), "e2e-git-vault-skill-5524", InstallTarget{
		Kind: InstallKindBot,
		Bot:  "testy",
	}); err != nil {
		t.Fatalf("SetAssetInstallation: %v", err)
	}

	m, _, err := manifest.LoadOrMigrate(dir)
	if err != nil {
		t.Fatalf("reload manifest: %v", err)
	}
	got := m.FindAsset("e2e-git-vault-skill-5524")
	if got == nil {
		t.Fatal("asset was not repaired into manifest")
	}
	if got.Version != "1" || got.Type != asset.TypeSkill {
		t.Fatalf("manifest asset = %+v, want skill version 1", got)
	}
	if got.SourcePath == nil || got.SourcePath.Path != "assets/e2e-git-vault-skill-5524/1" {
		t.Fatalf("source path = %+v, want stored asset path", got.SourcePath)
	}
	if len(got.Scopes) != 1 || got.Scopes[0].Kind != manifest.ScopeKindBot || got.Scopes[0].Bot != "testy" {
		t.Fatalf("scopes = %+v, want bot testy", got.Scopes)
	}

	events, err := mgmt.QueryAuditEvents(dir, mgmt.AuditFilter{Target: "e2e-git-vault-skill-5524"})
	if err != nil {
		t.Fatalf("query audit: %v", err)
	}
	var recovered, installed bool
	for _, event := range events {
		switch event.Event {
		case mgmt.EventAssetRecovered:
			recovered = event.TargetType == mgmt.TargetTypeAsset &&
				event.Data["recovered_from_storage"] == true &&
				event.Data["version"] == "1"
		case mgmt.EventInstallSet:
			installed = event.TargetType == mgmt.TargetTypeInstallation &&
				event.Data["kind"] == string(manifest.ScopeKindBot) &&
				event.Data["bot"] == "testy"
		}
	}
	if !recovered || !installed {
		t.Fatalf("audit events recovered=%v installed=%v events=%+v", recovered, installed, events)
	}
}

// TestPathVault_SetAssetInstallation_RejectsOtherUser verifies the user-
// scoped install guard: only the authenticated caller may be the target.
// Any write-access holder could otherwise force an asset to be "global"
// in another user's resolved lock via the user-match rule.
func TestPathVault_SetAssetInstallation_RejectsOtherUser(t *testing.T) {
	mgmt.ResetActorCache()
	dir := t.TempDir()

	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "alice@example.com")
	runGit(t, dir, "config", "user.name", "Alice Admin")

	if err := manifest.Save(dir, &manifest.Manifest{
		SchemaVersion: manifest.CurrentSchemaVersion,
		Assets: []manifest.Asset{
			{
				Name:       "my-skill",
				Version:    "1.0.0",
				Type:       asset.TypeSkill,
				SourceHTTP: &manifest.SourceHTTP{URL: "https://example.com/my-skill.zip"},
			},
		},
	}); err != nil {
		t.Fatalf("seed manifest: %v", err)
	}

	v, err := NewPathVault("file://" + dir)
	if err != nil {
		t.Fatalf("NewPathVault failed: %v", err)
	}
	ctx := context.Background()

	err = v.SetAssetInstallation(ctx, "my-skill", InstallTarget{Kind: InstallKindUser, User: "bob@example.com"})
	if err == nil {
		t.Fatal("expected user-scoped install for a non-caller to be rejected, got nil error")
	}
	if !strings.Contains(err.Error(), "user-scoped installs may only target the authenticated caller") {
		t.Errorf("unexpected error: %v", err)
	}

	if err := v.SetAssetInstallation(ctx, "my-skill", InstallTarget{Kind: InstallKindUser, User: "alice@example.com"}); err != nil {
		t.Fatalf("self-install should succeed: %v", err)
	}
}

// TestPathVault_TeamMutationsRequireAdmin verifies that every destructive
// team mutation enforces admin membership inside the transaction, not
// just at the CLI pre-check.
func TestPathVault_TeamMutationsRequireAdmin(t *testing.T) {
	mgmt.ResetActorCache()
	dir := t.TempDir()

	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "alice@example.com")
	runGit(t, dir, "config", "user.name", "Alice Admin")

	v, err := NewPathVault("file://" + dir)
	if err != nil {
		t.Fatalf("NewPathVault failed: %v", err)
	}
	ctx := context.Background()

	team := mgmt.Team{
		Name:    "platform",
		Members: []string{"alice@example.com", "bob@example.com"},
		Admins:  []string{"alice@example.com"},
	}
	if err := v.CreateTeam(ctx, team); err != nil {
		t.Fatalf("CreateTeam failed: %v", err)
	}

	mgmt.ResetActorCache()
	runGit(t, dir, "config", "user.email", "bob@example.com")
	runGit(t, dir, "config", "user.name", "Bob Notadmin")

	assertNotAdmin := func(label string, err error) {
		t.Helper()
		if err == nil {
			t.Errorf("%s: expected admin check to reject, got nil", label)
			return
		}
		if !strings.Contains(err.Error(), "are not an admin of team") {
			t.Errorf("%s: unexpected error: %v", label, err)
		}
	}

	assertNotAdmin("AddTeamMember", v.AddTeamMember(ctx, "platform", "carol@example.com", false))
	assertNotAdmin("RemoveTeamMember", v.RemoveTeamMember(ctx, "platform", "alice@example.com"))
	assertNotAdmin("SetTeamAdmin", v.SetTeamAdmin(ctx, "platform", "bob@example.com", true))
	assertNotAdmin("AddTeamRepository", v.AddTeamRepository(ctx, "platform", "https://github.com/acme/new.git"))
	assertNotAdmin("RemoveTeamRepository", v.RemoveTeamRepository(ctx, "platform", "https://github.com/acme/new.git"))
	assertNotAdmin("DeleteTeam", v.DeleteTeam(ctx, "platform"))
}

// TestPathVault_LockFileMigration seeds a pre-manifest sx.lock and
// asserts that the first vault read synthesizes an equivalent sx.toml
// and archives the legacy lock file with a .migrated suffix.
func TestPathVault_LockFileMigration(t *testing.T) {
	mgmt.ResetActorCache()
	dir := t.TempDir()

	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "alice@example.com")
	runGit(t, dir, "config", "user.name", "Alice Admin")

	seedLock := []byte(`lock-version = "1.0"
version = "legacy"
created-by = "test"

[[assets]]
name = "legacy-skill"
version = "1.0.0"
type = "skill"

  [assets.source-http]
  url = "https://example.com/legacy.zip"

  [[assets.scopes]]
  repo = "github.com/acme/legacy"
`)
	if err := writeFile(filepath.Join(dir, "sx.lock"), seedLock); err != nil {
		t.Fatalf("write seed lock: %v", err)
	}

	v, err := NewPathVault("file://" + dir)
	if err != nil {
		t.Fatalf("NewPathVault failed: %v", err)
	}

	// Any read-side call triggers LoadOrMigrate.
	if _, err := v.ListTeams(context.Background(), ListTeamsOptions{}); err != nil {
		t.Fatalf("ListTeams (triggers migration) failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, manifest.FileName)); err != nil {
		t.Errorf("sx.toml not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "sx.lock")); !os.IsNotExist(err) {
		t.Errorf("sx.lock should have been renamed, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "sx.lock.migrated")); err != nil {
		t.Errorf("sx.lock.migrated not created: %v", err)
	}

	m, _, err := manifest.LoadOrMigrate(dir)
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	skill := m.FindAsset("legacy-skill")
	if skill == nil {
		t.Fatal("legacy-skill missing from migrated manifest")
		return
	}
	if len(skill.Scopes) != 1 || skill.Scopes[0].Kind != manifest.ScopeKindRepo {
		t.Errorf("expected one repo scope after migration; got %+v", skill.Scopes)
	}
}

func findLockAsset(lf *lockfile.LockFile, name string) *lockfile.Asset {
	for i := range lf.Assets {
		if lf.Assets[i].Name == name {
			return &lf.Assets[i]
		}
	}
	return nil
}

func anyAuditEvent(events []mgmt.AuditEvent, name string) bool {
	for _, ev := range events {
		if ev.Event == name {
			return true
		}
	}
	return false
}

// writeFile is a tiny helper that creates missing parent dirs.
func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func newPathVaultWithTeams(t *testing.T, count int) (Vault, string) {
	t.Helper()
	mgmt.ResetActorCache()
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "alice@example.com")
	runGit(t, dir, "config", "user.name", "Alice")

	if err := manifest.Save(dir, &manifest.Manifest{SchemaVersion: manifest.CurrentSchemaVersion}); err != nil {
		t.Fatalf("seed manifest: %v", err)
	}
	v, err := NewPathVault("file://" + dir)
	if err != nil {
		t.Fatalf("NewPathVault: %v", err)
	}
	ctx := context.Background()
	for i := range count {
		name := fmt.Sprintf("team-%d", i)
		if err := v.CreateTeam(ctx, mgmt.Team{
			Name:    name,
			Members: []string{"alice@example.com"},
			Admins:  []string{"alice@example.com"},
		}); err != nil {
			t.Fatalf("CreateTeam %s: %v", name, err)
		}
	}
	return v, dir
}

func TestPathVault_ListTeams_DefaultLimit(t *testing.T) {
	v, _ := newPathVaultWithTeams(t, 25)
	ctx := context.Background()

	result, err := v.ListTeams(ctx, ListTeamsOptions{})
	if err != nil {
		t.Fatalf("ListTeams: %v", err)
	}
	if len(result.Teams) != 20 {
		t.Errorf("expected 20 teams, got %d", len(result.Teams))
	}
	if result.TotalCount != 25 {
		t.Errorf("expected TotalCount=25, got %d", result.TotalCount)
	}
	if !result.HasMore {
		t.Error("expected HasMore=true")
	}
}

func TestPathVault_ListTeams_FilterClientSide(t *testing.T) {
	mgmt.ResetActorCache()
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "alice@example.com")
	runGit(t, dir, "config", "user.name", "Alice")

	if err := manifest.Save(dir, &manifest.Manifest{SchemaVersion: manifest.CurrentSchemaVersion}); err != nil {
		t.Fatalf("seed manifest: %v", err)
	}
	v, err := NewPathVault("file://" + dir)
	if err != nil {
		t.Fatalf("NewPathVault: %v", err)
	}
	ctx := context.Background()

	for _, name := range []string{"platform-api", "platform-web", "infra", "data-eng"} {
		if err := v.CreateTeam(ctx, mgmt.Team{
			Name:    name,
			Members: []string{"alice@example.com"},
			Admins:  []string{"alice@example.com"},
		}); err != nil {
			t.Fatalf("CreateTeam %s: %v", name, err)
		}
	}

	result, err := v.ListTeams(ctx, ListTeamsOptions{Filter: "platform"})
	if err != nil {
		t.Fatalf("ListTeams: %v", err)
	}
	if len(result.Teams) != 2 {
		t.Errorf("expected 2 teams matching 'platform', got %d: %+v", len(result.Teams), result.Teams)
	}
	if result.TotalCount != 2 {
		t.Errorf("expected TotalCount=2, got %d", result.TotalCount)
	}
	if result.HasMore {
		t.Error("expected HasMore=false")
	}
	for _, team := range result.Teams {
		if !strings.Contains(team.Name, "platform") {
			t.Errorf("team %q does not match filter 'platform'", team.Name)
		}
	}
}

func TestPathVault_CreateTeam_NoAdminsDefaultsToCaller(t *testing.T) {
	mgmt.ResetActorCache()
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "alice@example.com")
	runGit(t, dir, "config", "user.name", "Alice")

	if err := manifest.Save(dir, &manifest.Manifest{SchemaVersion: manifest.CurrentSchemaVersion}); err != nil {
		t.Fatalf("seed manifest: %v", err)
	}
	v, err := NewPathVault("file://" + dir)
	if err != nil {
		t.Fatalf("NewPathVault: %v", err)
	}
	ctx := context.Background()

	if err := v.CreateTeam(ctx, mgmt.Team{
		Name: "no-admins",
	}); err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}

	team, err := v.GetTeam(ctx, "no-admins")
	if err != nil {
		t.Fatalf("GetTeam: %v", err)
	}
	if !slices.Contains(team.Admins, "alice@example.com") {
		t.Errorf("caller should be auto-added as admin, got admins=%v", team.Admins)
	}
	if !slices.Contains(team.Members, "alice@example.com") {
		t.Errorf("caller should be auto-added as member, got members=%v", team.Members)
	}
}

func TestPathVault_CreateTeam_ExplicitAdminNotCaller(t *testing.T) {
	mgmt.ResetActorCache()
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "alice@example.com")
	runGit(t, dir, "config", "user.name", "Alice")

	if err := manifest.Save(dir, &manifest.Manifest{SchemaVersion: manifest.CurrentSchemaVersion}); err != nil {
		t.Fatalf("seed manifest: %v", err)
	}
	v, err := NewPathVault("file://" + dir)
	if err != nil {
		t.Fatalf("NewPathVault: %v", err)
	}
	ctx := context.Background()

	if err := v.CreateTeam(ctx, mgmt.Team{
		Name:   "explicit-admin",
		Admins: []string{"bob@example.com"},
	}); err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}

	team, err := v.GetTeam(ctx, "explicit-admin")
	if err != nil {
		t.Fatalf("GetTeam: %v", err)
	}
	if !slices.Contains(team.Admins, "bob@example.com") {
		t.Errorf("explicit admin should be set, got admins=%v", team.Admins)
	}
	if slices.Contains(team.Admins, "alice@example.com") {
		t.Errorf("caller should not be auto-added as admin when explicit admins given, got admins=%v", team.Admins)
	}
	if !slices.Contains(team.Members, "bob@example.com") {
		t.Errorf("admin should be auto-added as member, got members=%v", team.Members)
	}
}
