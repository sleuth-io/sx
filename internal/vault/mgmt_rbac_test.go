package vault

import (
	"context"
	"strings"
	"testing"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/manifest"
	"github.com/sleuth-io/sx/internal/mgmt"
)

// seedScopedRBACVault is seedRBACVault plus a starting scope set on my-skill, so
// removal-gating (replace/global) can be exercised. Returns the vault and its
// on-disk root for reading the resulting manifest back.
func seedScopedRBACVault(t *testing.T, actorEmail string, scopes []manifest.Scope, teams []manifest.Team, orgAdmins []string) (*PathVault, string) {
	t.Helper()
	mgmt.ResetActorCache()
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", actorEmail)
	runGit(t, dir, "config", "user.name", "Test User")

	m := &manifest.Manifest{
		SchemaVersion: manifest.CurrentSchemaVersion,
		Assets: []manifest.Asset{{
			Name:       "my-skill",
			Version:    "1.0.0",
			Type:       asset.TypeSkill,
			SourceHTTP: &manifest.SourceHTTP{URL: "https://example.com/my-skill.zip"},
			Scopes:     scopes,
		}},
		Teams: teams,
	}
	if len(orgAdmins) > 0 {
		m.Org = &manifest.Org{Admins: orgAdmins}
	}
	if err := manifest.Save(dir, m); err != nil {
		t.Fatalf("seed manifest: %v", err)
	}
	v, err := NewPathVault("file://" + dir)
	if err != nil {
		t.Fatalf("NewPathVault: %v", err)
	}
	return v, dir
}

// assetScopes reads my-skill's current scopes from the vault's manifest.
func assetScopes(t *testing.T, dir string) []manifest.Scope {
	t.Helper()
	m, ok, err := manifest.Load(dir)
	if err != nil || !ok {
		t.Fatalf("load manifest: ok=%v err=%v", ok, err)
	}
	a := m.FindAsset("my-skill")
	if a == nil {
		t.Fatal("my-skill missing from manifest")
	}
	return a.Scopes
}

// TestScopeRBAC_ReplaceKeepsProtectedTeam reproduces the reported bug: a
// non-admin re-scoping a team-scoped skill to "just for me" (a wholesale
// replace) must NOT silently drop the team scope. The team is preserved and
// reported as skipped, while the user's own scope still applies.
func TestScopeRBAC_ReplaceKeepsProtectedTeam(t *testing.T) {
	ctx := context.Background()
	startScopes := []manifest.Scope{{Kind: manifest.ScopeKindTeam, Team: "platform"}}

	// mallory is not a platform admin (governed vault, she's no org-admin either).
	v, dir := seedScopedRBACVault(t, "mallory@example.com", startScopes,
		[]manifest.Team{platformTeam()}, []string{"boss@example.com"})

	skipped, err := v.SetAssetInstallations(ctx, "my-skill",
		[]InstallTarget{{Kind: InstallKindUser, User: "mallory@example.com"}}, false)
	if err != nil {
		t.Fatalf("SetAssetInstallations: %v", err)
	}
	if len(skipped) != 1 || skipped[0].Target.Kind != InstallKindTeam ||
		!strings.Contains(skipped[0].Reason, "admin of team") {
		t.Fatalf("expected the team scope reported as kept, got %+v", skipped)
	}

	scopes := assetScopes(t, dir)
	if !scopeExistsOnAsset(scopes, manifest.Scope{Kind: manifest.ScopeKindTeam, Team: "platform"}) {
		t.Fatalf("team scope must be preserved through a non-admin replace, got %+v", scopes)
	}
	if !scopeExistsOnAsset(scopes, manifest.Scope{Kind: manifest.ScopeKindUser, User: "mallory@example.com"}) {
		t.Fatalf("the actor's own user scope should have applied, got %+v", scopes)
	}
}

// TestScopeRBAC_ReplaceTeamAdminCanRemove is the allowed counterpart: the team's
// admin re-scoping to "just for me" DOES remove the team scope.
func TestScopeRBAC_ReplaceTeamAdminCanRemove(t *testing.T) {
	ctx := context.Background()
	startScopes := []manifest.Scope{{Kind: manifest.ScopeKindTeam, Team: "platform"}}

	v, dir := seedScopedRBACVault(t, "alice@example.com", startScopes,
		[]manifest.Team{platformTeam()}, []string{"boss@example.com"})

	skipped, err := v.SetAssetInstallations(ctx, "my-skill",
		[]InstallTarget{{Kind: InstallKindUser, User: "alice@example.com"}}, false)
	if err != nil {
		t.Fatalf("SetAssetInstallations: %v", err)
	}
	if len(skipped) != 0 {
		t.Fatalf("team admin should replace freely, got skipped=%+v", skipped)
	}
	scopes := assetScopes(t, dir)
	if scopeExistsOnAsset(scopes, manifest.Scope{Kind: manifest.ScopeKindTeam, Team: "platform"}) {
		t.Fatalf("team admin's replace should drop the team scope, got %+v", scopes)
	}
}

// TestScopeRBAC_GlobalKeepsProtectedTeam covers the singular SetInstallations
// ("go global") path: a non-admin can't strip a team scope by globalizing the
// skill — the team survives.
func TestScopeRBAC_GlobalKeepsProtectedTeam(t *testing.T) {
	ctx := context.Background()
	startScopes := []manifest.Scope{{Kind: manifest.ScopeKindTeam, Team: "platform"}}

	v, dir := seedScopedRBACVault(t, "mallory@example.com", startScopes,
		[]manifest.Team{platformTeam()}, []string{"boss@example.com"})

	// "Global" sends an asset with an empty scope set through SetInstallations.
	if err := v.SetInstallations(ctx, &lockfile.Asset{
		Name: "my-skill", Version: "1.0.0", Type: asset.TypeSkill,
		SourceHTTP: &lockfile.SourceHTTP{URL: "https://example.com/my-skill.zip"},
	}, ""); err != nil {
		t.Fatalf("SetInstallations: %v", err)
	}

	scopes := assetScopes(t, dir)
	if !scopeExistsOnAsset(scopes, manifest.Scope{Kind: manifest.ScopeKindTeam, Team: "platform"}) {
		t.Fatalf("non-admin going global must not strip the team scope, got %+v", scopes)
	}
}

// seedRBACVault creates a path vault holding one asset, git-identified as
// actorEmail, with the given teams and org-admins. The org-admins list is the
// single on/off switch for scope governance (see docs/rbac.md), so tests set it
// to exercise the gate.
func seedRBACVault(t *testing.T, actorEmail string, teams []manifest.Team, orgAdmins []string) *PathVault {
	t.Helper()
	mgmt.ResetActorCache()
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", actorEmail)
	runGit(t, dir, "config", "user.name", "Test User")

	m := &manifest.Manifest{
		SchemaVersion: manifest.CurrentSchemaVersion,
		Assets: []manifest.Asset{{
			Name:       "my-skill",
			Version:    "1.0.0",
			Type:       asset.TypeSkill,
			SourceHTTP: &manifest.SourceHTTP{URL: "https://example.com/my-skill.zip"},
		}},
		Teams: teams,
	}
	if len(orgAdmins) > 0 {
		m.Org = &manifest.Org{Admins: orgAdmins}
	}
	if err := manifest.Save(dir, m); err != nil {
		t.Fatalf("seed manifest: %v", err)
	}
	v, err := NewPathVault("file://" + dir)
	if err != nil {
		t.Fatalf("NewPathVault: %v", err)
	}
	return v
}

// platformTeam is admined (and membered) by alice; mallory is in neither.
func platformTeam() manifest.Team {
	return manifest.Team{
		Name:    "platform",
		Members: []string{"alice@example.com"},
		Admins:  []string{"alice@example.com"},
	}
}

// TestScopeRBAC_NoOrgAdminsIsUngoverned: with no org-admins, anyone may set any
// non-team scope. (Team scope is the exception — always team-admin gated; see
// TestScopeRBAC_TeamScopeAlwaysRequiresAdmin.)
func TestScopeRBAC_NoOrgAdminsIsUngoverned(t *testing.T) {
	v := seedRBACVault(t, "mallory@example.com", []manifest.Team{platformTeam()}, nil)
	skipped, err := v.SetAssetInstallations(context.Background(), "my-skill", []InstallTarget{
		{Kind: InstallKindRepo, Repo: "github.com/acme/x"},
	}, false)
	if err != nil {
		t.Fatalf("SetAssetInstallations: %v", err)
	}
	if len(skipped) != 0 {
		t.Fatalf("ungoverned: a non-admin should set a repo scope, got %+v", skipped)
	}
}

// TestScopeRBAC_TeamScopeAlwaysRequiresAdmin: locking a skill to a team requires
// being an admin of that team even in an ungoverned vault — a random user can't
// scope someone's skill to a team they don't run.
func TestScopeRBAC_TeamScopeAlwaysRequiresAdmin(t *testing.T) {
	ctx := context.Background()
	team := []manifest.Team{platformTeam()} // alice admins platform; mallory is nobody

	// Ungoverned (no org-admins): non-admin denied.
	v := seedRBACVault(t, "mallory@example.com", team, nil)
	skipped, err := v.SetAssetInstallations(ctx, "my-skill",
		[]InstallTarget{{Kind: InstallKindTeam, Team: "platform"}}, true)
	if err != nil {
		t.Fatalf("SetAssetInstallations: %v", err)
	}
	if len(skipped) != 1 || !strings.Contains(skipped[0].Reason, "admin of team") {
		t.Fatalf("ungoverned non-admin should be denied a team scope, got %+v", skipped)
	}

	// Ungoverned: the team's admin is allowed.
	v = seedRBACVault(t, "alice@example.com", team, nil)
	if skipped, err := v.SetAssetInstallations(ctx, "my-skill",
		[]InstallTarget{{Kind: InstallKindTeam, Team: "platform"}}, true); err != nil || len(skipped) != 0 {
		t.Fatalf("ungoverned team admin should set the team scope: skipped=%+v err=%v", skipped, err)
	}
}

// TestScopeRBAC_TeamScopeRequiresThatTeamsAdmin: once governed, a team scope is
// allowed for an admin of that team and denied for everyone else.
func TestScopeRBAC_TeamScopeRequiresThatTeamsAdmin(t *testing.T) {
	ctx := context.Background()
	team := []manifest.Team{platformTeam()}
	admins := []string{"orgboss@example.com"}

	// Admin of platform → allowed (even though not an org-admin).
	v := seedRBACVault(t, "alice@example.com", team, admins)
	if skipped, err := v.SetAssetInstallations(ctx, "my-skill",
		[]InstallTarget{{Kind: InstallKindTeam, Team: "platform"}}, true); err != nil || len(skipped) != 0 {
		t.Fatalf("team admin should set team scope: skipped=%+v err=%v", skipped, err)
	}

	// Non-admin, non-org-admin (mallory) → denied.
	v = seedRBACVault(t, "mallory@example.com", team, admins)
	skipped, err := v.SetAssetInstallations(ctx, "my-skill",
		[]InstallTarget{{Kind: InstallKindTeam, Team: "platform"}}, true)
	if err != nil {
		t.Fatalf("SetAssetInstallations: %v", err)
	}
	if len(skipped) != 1 || !strings.Contains(skipped[0].Reason, "admin of team") {
		t.Fatalf("expected team-admin denial, got %+v", skipped)
	}
}

// TestScopeRBAC_SelfUserAlwaysAllowed: any actor may scope an asset to their own
// account, even under governance with no admin rights.
func TestScopeRBAC_SelfUserAlwaysAllowed(t *testing.T) {
	v := seedRBACVault(t, "mallory@example.com", []manifest.Team{platformTeam()}, []string{"orgboss@example.com"})
	skipped, err := v.SetAssetInstallations(context.Background(), "my-skill",
		[]InstallTarget{{Kind: InstallKindUser, User: "mallory@example.com"}}, true)
	if err != nil {
		t.Fatalf("SetAssetInstallations: %v", err)
	}
	if len(skipped) != 0 {
		t.Fatalf("self user scope should always be allowed, got %+v", skipped)
	}
}

// TestScopeRBAC_BroadScopesRequireOrgAdmin: org/repo/path scopes require being an
// org-admin — denied for a mere team admin, allowed for an org-admin.
func TestScopeRBAC_BroadScopesRequireOrgAdmin(t *testing.T) {
	ctx := context.Background()
	team := []manifest.Team{platformTeam()}
	admins := []string{"orgboss@example.com"}
	broad := []InstallTarget{
		{Kind: InstallKindRepo, Repo: "github.com/acme/x"},
		{Kind: InstallKindPath, Repo: "github.com/acme/y", Paths: []string{"src"}},
	}

	// alice admins a team but is not an org-admin → every broad target denied.
	v := seedRBACVault(t, "alice@example.com", team, admins)
	skipped, err := v.SetAssetInstallations(ctx, "my-skill", broad, true)
	if err != nil {
		t.Fatalf("SetAssetInstallations: %v", err)
	}
	if len(skipped) != len(broad) {
		t.Fatalf("team admin should be denied broad scopes, got %+v", skipped)
	}
	for _, s := range skipped {
		if !strings.Contains(s.Reason, "org-admin") {
			t.Errorf("expected org-admin reason, got %q", s.Reason)
		}
	}

	// The org-admin → allowed.
	v = seedRBACVault(t, "orgboss@example.com", team, admins)
	if skipped, err := v.SetAssetInstallations(ctx, "my-skill", broad, true); err != nil || len(skipped) != 0 {
		t.Fatalf("org-admin should set broad scopes: skipped=%+v err=%v", skipped, err)
	}
}

// TestScopeRBAC_OrgWideGated: org-wide is exclusive and bypasses the per-target
// resolve loop, so confirm it is gated too — denied for a non-org-admin.
func TestScopeRBAC_OrgWideGated(t *testing.T) {
	v := seedRBACVault(t, "alice@example.com", []manifest.Team{platformTeam()}, []string{"orgboss@example.com"})
	skipped, err := v.SetAssetInstallations(context.Background(), "my-skill",
		[]InstallTarget{{Kind: InstallKindOrg}}, false)
	if err != nil {
		t.Fatalf("SetAssetInstallations: %v", err)
	}
	if len(skipped) != 1 || skipped[0].Target.Kind != InstallKindOrg ||
		!strings.Contains(skipped[0].Reason, "org-admin") {
		t.Fatalf("expected org-wide denial for non-org-admin, got %+v", skipped)
	}
}

// TestScopeSetPermission_Matrix exhaustively exercises the scope gate
// (scopeSetPermissionReason) across governance states, actor roles, and target
// kinds — the file-backed vault's full permission matrix.
func TestScopeSetPermission_Matrix(t *testing.T) {
	platform := manifest.Team{Name: "platform", Members: []string{"alice@x.com", "carol@x.com"}, Admins: []string{"alice@x.com"}}
	backend := manifest.Team{Name: "backend", Members: []string{"dave@x.com"}, Admins: []string{"dave@x.com"}}
	teams := []manifest.Team{platform, backend}

	ungoverned := &manifest.Manifest{Teams: teams}
	governed := &manifest.Manifest{Teams: teams, Org: &manifest.Org{Admins: []string{"boss@x.com"}}}

	org := InstallTarget{Kind: InstallKindOrg}
	repo := InstallTarget{Kind: InstallKindRepo, Repo: "github.com/acme/r"}
	path := InstallTarget{Kind: InstallKindPath, Repo: "github.com/acme/r", Paths: []string{"svc"}}
	botT := InstallTarget{Kind: InstallKindBot, Bot: "ci"}
	teamPlatform := InstallTarget{Kind: InstallKindTeam, Team: "platform"}
	teamBackend := InstallTarget{Kind: InstallKindTeam, Team: "backend"}
	teamGhost := InstallTarget{Kind: InstallKindTeam, Team: "ghost"}
	user := func(e string) InstallTarget { return InstallTarget{Kind: InstallKindUser, User: e} }
	a := func(e string) mgmt.Actor { return mgmt.Actor{Email: e} }

	cases := []struct {
		name    string
		m       *manifest.Manifest
		target  InstallTarget
		actor   mgmt.Actor
		allowed bool
	}{
		// Ungoverned: anyone may set any scope EXCEPT a team scope, which always
		// requires being an admin of that team (teams own skills).
		{"ungoverned/random/org", ungoverned, org, a("nobody@x.com"), true},
		{"ungoverned/random/repo", ungoverned, repo, a("nobody@x.com"), true},
		{"ungoverned/team-admin/team", ungoverned, teamPlatform, a("alice@x.com"), true},
		{"ungoverned/random/team-denied", ungoverned, teamPlatform, a("nobody@x.com"), false},
		{"ungoverned/member/team-denied", ungoverned, teamPlatform, a("carol@x.com"), false},
		{"ungoverned/empty-identity/team-denied", ungoverned, teamPlatform, a(""), false},

		// Governed: broad scopes require org-admin.
		{"governed/org-admin/org", governed, org, a("boss@x.com"), true},
		{"governed/org-admin/repo", governed, repo, a("boss@x.com"), true},
		{"governed/org-admin/path", governed, path, a("boss@x.com"), true},
		{"governed/org-admin/bot", governed, botT, a("boss@x.com"), true},
		{"governed/team-admin/repo-denied", governed, repo, a("alice@x.com"), false},
		{"governed/team-admin/org-denied", governed, org, a("alice@x.com"), false},
		{"governed/member/path-denied", governed, path, a("carol@x.com"), false},
		{"governed/random/bot-denied", governed, botT, a("nobody@x.com"), false},
		{"governed/empty-identity/repo-denied", governed, repo, a(""), false},

		// Governed: team scope requires admin of THAT team (or org-admin).
		{"governed/platform-admin/team-platform", governed, teamPlatform, a("alice@x.com"), true},
		{"governed/org-admin/team-platform", governed, teamPlatform, a("boss@x.com"), true},
		{"governed/platform-member/team-platform-denied", governed, teamPlatform, a("carol@x.com"), false},
		{"governed/other-team-admin/team-platform-denied", governed, teamPlatform, a("dave@x.com"), false},
		{"governed/random/team-platform-denied", governed, teamPlatform, a("nobody@x.com"), false},
		{"governed/empty-identity/team-platform-denied", governed, teamPlatform, a(""), false},
		{"governed/backend-admin/team-backend", governed, teamBackend, a("dave@x.com"), true},
		{"governed/platform-admin/team-backend-denied", governed, teamBackend, a("alice@x.com"), false},
		{"governed/team-admin/ghost-team-denied", governed, teamGhost, a("alice@x.com"), false},
		{"governed/org-admin/ghost-team-allowed-by-role", governed, teamGhost, a("boss@x.com"), true},

		// Governed: user scope is self-only; org-admins bypass.
		{"governed/self-user", governed, user("carol@x.com"), a("carol@x.com"), true},
		{"governed/self-user/random", governed, user("nobody@x.com"), a("nobody@x.com"), true},
		{"governed/other-user-denied", governed, user("someone@x.com"), a("carol@x.com"), false},
		{"governed/org-admin/other-user-allowed", governed, user("someone@x.com"), a("boss@x.com"), true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			reason := scopeSetPermissionReason(c.m, c.target, c.actor)
			if (reason == "") != c.allowed {
				t.Fatalf("allowed=%v want=%v (reason=%q)", reason == "", c.allowed, reason)
			}
		})
	}
}

// TestUninstallScopeRBAC: removing a scope needs the same authority as setting
// it — a non-admin can't remove a team scope, the team's admin can.
func TestUninstallScopeRBAC(t *testing.T) {
	ctx := context.Background()
	seed := func(actorEmail string) *PathVault {
		mgmt.ResetActorCache()
		dir := t.TempDir()
		runGit(t, dir, "init")
		runGit(t, dir, "config", "user.email", actorEmail)
		runGit(t, dir, "config", "user.name", "U")
		m := &manifest.Manifest{
			SchemaVersion: manifest.CurrentSchemaVersion,
			Assets: []manifest.Asset{{
				Name: "my-skill", Version: "1", Type: asset.TypeSkill,
				SourceHTTP: &manifest.SourceHTTP{URL: "https://example.com/s.zip"},
				Scopes:     []manifest.Scope{{Kind: manifest.ScopeKindTeam, Team: "platform"}},
			}},
			Teams: []manifest.Team{platformTeam()},
			Org:   &manifest.Org{Admins: []string{"boss@example.com"}},
		}
		if err := manifest.Save(dir, m); err != nil {
			t.Fatalf("seed: %v", err)
		}
		v, err := NewPathVault("file://" + dir)
		if err != nil {
			t.Fatalf("NewPathVault: %v", err)
		}
		return v
	}

	// A denied removal is reported per-target (no hard error), mirroring set.
	v := seed("mallory@example.com")
	removed, failures, err := v.UninstallAssetTargets(ctx, "my-skill",
		[]InstallTarget{{Kind: InstallKindTeam, Team: "platform"}})
	if err != nil {
		t.Fatalf("a denied removal should not hard-error: %v", err)
	}
	if removed != 0 || len(failures) != 1 || !strings.Contains(failures[0], "admin of team") {
		t.Fatalf("non-admin team-scope removal should be a reported failure: removed=%d failures=%+v", removed, failures)
	}

	v = seed("alice@example.com")
	removed, _, err = v.UninstallAssetTargets(ctx, "my-skill",
		[]InstallTarget{{Kind: InstallKindTeam, Team: "platform"}})
	if err != nil || removed != 1 {
		t.Fatalf("team admin should remove the team scope: removed=%d err=%v", removed, err)
	}
}

// TestUninstallScopeRBAC_PartialBatch: a removal batch removes what the actor is
// allowed to and reports the rest as failures, instead of aborting everything on
// one denied target.
func TestUninstallScopeRBAC_PartialBatch(t *testing.T) {
	ctx := context.Background()
	mgmt.ResetActorCache()
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "mallory@example.com")
	runGit(t, dir, "config", "user.name", "Mallory")
	m := &manifest.Manifest{
		SchemaVersion: manifest.CurrentSchemaVersion,
		Assets: []manifest.Asset{{
			Name: "my-skill", Version: "1", Type: asset.TypeSkill,
			SourceHTTP: &manifest.SourceHTTP{URL: "https://example.com/s.zip"},
			Scopes: []manifest.Scope{
				{Kind: manifest.ScopeKindTeam, Team: "platform"},
				{Kind: manifest.ScopeKindUser, User: "mallory@example.com"},
			},
		}},
		Teams: []manifest.Team{platformTeam()}, // mallory is not a platform admin
		Org:   &manifest.Org{Admins: []string{"boss@example.com"}},
	}
	if err := manifest.Save(dir, m); err != nil {
		t.Fatalf("seed: %v", err)
	}
	v, err := NewPathVault("file://" + dir)
	if err != nil {
		t.Fatalf("NewPathVault: %v", err)
	}

	// Mallory may remove her own user scope but not the team scope.
	removed, failures, err := v.UninstallAssetTargets(ctx, "my-skill", []InstallTarget{
		{Kind: InstallKindTeam, Team: "platform"},
		{Kind: InstallKindUser, User: "mallory@example.com"},
	})
	if err != nil {
		t.Fatalf("partial batch should not hard-error: %v", err)
	}
	if removed != 1 || len(failures) != 1 || !strings.Contains(failures[0], "admin of team") {
		t.Fatalf("expected the user scope removed and the team scope reported: removed=%d failures=%+v", removed, failures)
	}
}

// TestScopeRBAC_TrustedWriteBypassesGate: a trusted bulk write (vault copy)
// skips the gate even for a non-admin in a governed vault.
func TestScopeRBAC_TrustedWriteBypassesGate(t *testing.T) {
	v := seedRBACVault(t, "mallory@example.com", []manifest.Team{platformTeam()}, []string{"boss@example.com"})
	ctx := ContextWithTrustedScopeWrite(context.Background())
	skipped, err := v.SetAssetInstallations(ctx, "my-skill",
		[]InstallTarget{{Kind: InstallKindRepo, Repo: "github.com/acme/x"}}, true)
	if err != nil || len(skipped) != 0 {
		t.Fatalf("trusted write should bypass the gate: skipped=%+v err=%v", skipped, err)
	}
}
