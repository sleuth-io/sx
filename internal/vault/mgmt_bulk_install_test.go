package vault

import (
	"context"
	"testing"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/manifest"
	"github.com/sleuth-io/sx/internal/mgmt"
)

// seedBulkInstallVault creates a path vault with one asset carrying a baseline
// repo scope and a "platform" team admined by the actor (alice), so identity
// scopes (team/user) can be exercised. Returns the vault and its dir.
func seedBulkInstallVault(t *testing.T) (*PathVault, string) {
	t.Helper()
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
		},
	}); err != nil {
		t.Fatalf("seed manifest: %v", err)
	}

	v, err := NewPathVault("file://" + dir)
	if err != nil {
		t.Fatalf("NewPathVault: %v", err)
	}
	if err := v.CreateTeam(context.Background(), mgmt.Team{
		Name:    "platform",
		Members: []string{"alice@example.com"},
		Admins:  []string{"alice@example.com"},
	}); err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	return v, dir
}

func scopeKinds(t *testing.T, dir string) []manifest.ScopeKind {
	t.Helper()
	m, _, err := manifest.LoadOrMigrate(dir)
	if err != nil {
		t.Fatalf("reload manifest: %v", err)
	}
	a := m.FindAsset("my-skill")
	if a == nil {
		return nil
	}
	kinds := make([]manifest.ScopeKind, 0, len(a.Scopes))
	for _, s := range a.Scopes {
		kinds = append(kinds, s.Kind)
	}
	return kinds
}

func hasKind(kinds []manifest.ScopeKind, want manifest.ScopeKind) bool {
	for _, k := range kinds {
		if k == want {
			return true
		}
	}
	return false
}

// TestPathVault_SetAssetInstallations_Replace pins SD-10170: a file-backed vault
// (the "github"/git family) must apply a batch of identity + repo scopes, not
// just the Sleuth vault. Replace mode drops the seeded baseline repo scope and
// installs exactly the named set (team + self user).
func TestPathVault_SetAssetInstallations_Replace(t *testing.T) {
	v, dir := seedBulkInstallVault(t)
	ctx := context.Background()

	unresolved, err := v.SetAssetInstallations(ctx, "my-skill", []InstallTarget{
		{Kind: InstallKindTeam, Team: "platform"},
		{Kind: InstallKindUser, User: "alice@example.com"},
	}, false)
	if err != nil {
		t.Fatalf("SetAssetInstallations: %v", err)
	}
	if len(unresolved) != 0 {
		t.Errorf("file vault should never report unresolved targets, got %+v", unresolved)
	}

	kinds := scopeKinds(t, dir)
	if hasKind(kinds, manifest.ScopeKindRepo) {
		t.Errorf("replace should have dropped the baseline repo scope, got %+v", kinds)
	}
	if !hasKind(kinds, manifest.ScopeKindTeam) || !hasKind(kinds, manifest.ScopeKindUser) {
		t.Errorf("expected team+user scopes after replace, got %+v", kinds)
	}
}

// TestPathVault_SetAssetInstallations_Append keeps the existing scope and adds
// the named ones.
func TestPathVault_SetAssetInstallations_Append(t *testing.T) {
	v, dir := seedBulkInstallVault(t)
	ctx := context.Background()

	if _, err := v.SetAssetInstallations(ctx, "my-skill", []InstallTarget{
		{Kind: InstallKindTeam, Team: "platform"},
	}, true); err != nil {
		t.Fatalf("SetAssetInstallations (append): %v", err)
	}

	kinds := scopeKinds(t, dir)
	if !hasKind(kinds, manifest.ScopeKindRepo) || !hasKind(kinds, manifest.ScopeKindTeam) {
		t.Errorf("append should keep baseline repo and add team, got %+v", kinds)
	}
}

// TestPathVault_SetAssetInstallations_OrgIsExclusive verifies an org target
// clears every scope (the asset goes global).
func TestPathVault_SetAssetInstallations_OrgIsExclusive(t *testing.T) {
	v, dir := seedBulkInstallVault(t)
	ctx := context.Background()

	if _, err := v.SetAssetInstallations(ctx, "my-skill", []InstallTarget{
		{Kind: InstallKindOrg},
	}, false); err != nil {
		t.Fatalf("SetAssetInstallations (org): %v", err)
	}
	if kinds := scopeKinds(t, dir); len(kinds) != 0 {
		t.Errorf("org install should clear all scopes (global), got %+v", kinds)
	}
}

// TestPathVault_SetAssetInstallations_SkipsUnresolvableTargets mirrors the
// Sleuth vault: targets that can't be set (a team that doesn't exist) are
// skipped and returned as unresolved, while the resolvable ones still apply —
// the batch is not aborted.
func TestPathVault_SetAssetInstallations_SkipsUnresolvableTargets(t *testing.T) {
	v, dir := seedBulkInstallVault(t)
	ctx := context.Background()

	unresolved, err := v.SetAssetInstallations(ctx, "my-skill", []InstallTarget{
		{Kind: InstallKindTeam, Team: "platform"},     // exists, alice is admin → set
		{Kind: InstallKindTeam, Team: "Nonexistent"},  // missing → skipped
		{Kind: InstallKindTeam, Team: "also-missing"}, // missing → skipped
	}, true)
	if err != nil {
		t.Fatalf("SetAssetInstallations should not error on unresolvable targets: %v", err)
	}
	if len(unresolved) != 2 {
		t.Fatalf("expected 2 unresolved targets, got %+v", unresolved)
	}
	if !hasKind(scopeKinds(t, dir), manifest.ScopeKindTeam) {
		t.Errorf("the resolvable team scope should have been set despite the skipped ones")
	}
}

// TestPathVault_SetAssetInstallations_AllUnresolvableLeavesScopesUntouched
// guards the dangerous case: a REPLACE whose every target is unresolvable must
// NOT clear the asset's existing scopes (which would silently globalize it).
func TestPathVault_SetAssetInstallations_AllUnresolvableLeavesScopesUntouched(t *testing.T) {
	v, dir := seedBulkInstallVault(t)
	ctx := context.Background()

	before := scopeKinds(t, dir) // baseline repo scope from the seed
	if !hasKind(before, manifest.ScopeKindRepo) {
		t.Fatalf("precondition: expected a baseline repo scope, got %+v", before)
	}

	unresolved, err := v.SetAssetInstallations(ctx, "my-skill", []InstallTarget{
		{Kind: InstallKindTeam, Team: "ghost-a"},
		{Kind: InstallKindTeam, Team: "ghost-b"},
	}, false) // REPLACE
	if err != nil {
		t.Fatalf("SetAssetInstallations: %v", err)
	}
	if len(unresolved) != 2 {
		t.Errorf("expected both targets unresolved, got %+v", unresolved)
	}
	// The original baseline repo scope must survive — no globalization.
	if after := scopeKinds(t, dir); !hasKind(after, manifest.ScopeKindRepo) {
		t.Errorf("all-unresolvable REPLACE must leave existing scopes untouched, got %+v", after)
	}
}

// TestPathVault_CurrentInstallTargets_ShowsUserScope is the SD-10170 display
// fix: a file vault must report a user/team/bot scope through the same
// CurrentInstallTargets path the Sleuth vault uses, so `sx add` renders "just
// for me" instead of silently collapsing a user-scoped asset to "global".
func TestPathVault_CurrentInstallTargets_ShowsUserScope(t *testing.T) {
	v, _ := seedBulkInstallVault(t)
	ctx := context.Background()

	if _, err := v.SetAssetInstallations(ctx, "my-skill", []InstallTarget{
		{Kind: InstallKindUser, User: "alice@example.com"},
	}, false); err != nil {
		t.Fatalf("SetAssetInstallations: %v", err)
	}

	targets, present, err := v.CurrentInstallTargets(ctx, "my-skill")
	if err != nil {
		t.Fatalf("CurrentInstallTargets: %v", err)
	}
	if !present {
		t.Fatal("expected asset to be reported as installed")
	}
	if len(targets) != 1 || targets[0].Kind != InstallKindUser || targets[0].User != "alice@example.com" {
		t.Fatalf("expected a single user target for alice, got %+v", targets)
	}
}

// TestPathVault_CurrentInstallTargets_GlobalAndAbsent covers the two boundary
// cases: an org-wide install is present with no targets (renders "global"), and
// an asset not in the manifest is reported absent.
func TestPathVault_CurrentInstallTargets_GlobalAndAbsent(t *testing.T) {
	v, _ := seedBulkInstallVault(t)
	ctx := context.Background()

	if _, err := v.SetAssetInstallations(ctx, "my-skill", []InstallTarget{
		{Kind: InstallKindOrg},
	}, false); err != nil {
		t.Fatalf("SetAssetInstallations (org): %v", err)
	}
	targets, present, err := v.CurrentInstallTargets(ctx, "my-skill")
	if err != nil {
		t.Fatalf("CurrentInstallTargets: %v", err)
	}
	if !present || len(targets) != 0 {
		t.Fatalf("org-wide should be present with no targets, got present=%v targets=%+v", present, targets)
	}

	if _, present, err := v.CurrentInstallTargets(ctx, "no-such-asset"); err != nil || present {
		t.Fatalf("absent asset should report present=false, got present=%v err=%v", present, err)
	}
}

// TestPathVault_UninstallAssetTargets removes a subset of scopes and reports the
// count; unremovable targets (org has no row) come back as failures, not errors.
func TestPathVault_UninstallAssetTargets(t *testing.T) {
	v, dir := seedBulkInstallVault(t)
	ctx := context.Background()

	if _, err := v.SetAssetInstallations(ctx, "my-skill", []InstallTarget{
		{Kind: InstallKindTeam, Team: "platform"},
	}, true); err != nil {
		t.Fatalf("seed scopes: %v", err)
	}

	removed, failures, err := v.UninstallAssetTargets(ctx, "my-skill", []InstallTarget{
		{Kind: InstallKindTeam, Team: "platform"},
		{Kind: InstallKindOrg}, // no scope row exists for org — should surface as a failure
	})
	if err != nil {
		t.Fatalf("UninstallAssetTargets: %v", err)
	}
	if removed != 1 {
		t.Errorf("expected 1 scope removed, got %d", removed)
	}
	if len(failures) != 1 {
		t.Errorf("expected 1 failure (org has no removable row), got %+v", failures)
	}

	kinds := scopeKinds(t, dir)
	if hasKind(kinds, manifest.ScopeKindTeam) {
		t.Errorf("team scope should be gone, got %+v", kinds)
	}
	if !hasKind(kinds, manifest.ScopeKindRepo) {
		t.Errorf("baseline repo scope should remain, got %+v", kinds)
	}
}
