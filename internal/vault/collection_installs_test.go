package vault

import (
	"context"
	"testing"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/manifest"
	"github.com/sleuth-io/sx/internal/mgmt"
)

func newCollectionInstallTestVault(t *testing.T) (*PathVault, string) {
	t.Helper()
	mgmt.ResetActorCache()
	dir := t.TempDir()

	// Vault mutations resolve the actor from the vault dir's git config;
	// CI runners have no global identity, so pin one locally.
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "alice@example.com")
	runGit(t, dir, "config", "user.name", "Alice Admin")

	if err := manifest.Save(dir, &manifest.Manifest{
		SchemaVersion: manifest.CurrentSchemaVersion,
		Assets: []manifest.Asset{
			{
				Name: "member", Version: "1", Type: asset.TypeSkill,
				SourceHTTP: &manifest.SourceHTTP{URL: "https://example.com/member.zip"},
				Scopes:     []manifest.Scope{{Kind: manifest.ScopeKindRepo, Repo: "github.com/acme/direct"}},
			},
			{
				Name: "other", Version: "1", Type: asset.TypeSkill,
				SourceHTTP: &manifest.SourceHTTP{URL: "https://example.com/other.zip"},
			},
		},
		Collections: []manifest.Collection{
			{Name: "essentials", Assets: []string{"member"}},
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

func collectionScopes(t *testing.T, dir, name string) []manifest.Scope {
	t.Helper()
	m, _, err := manifest.LoadOrMigrate(dir)
	if err != nil {
		t.Fatalf("reload manifest: %v", err)
	}
	c, err := m.FindCollection(name)
	if err != nil {
		t.Fatalf("FindCollection: %v", err)
	}
	return c.Scopes
}

// Installing a collection stores ONE row on the collection — member
// assets' own scopes are never rewritten, and removal is symmetric.
func TestPathVault_CollectionInstallRoundTrip(t *testing.T) {
	v, dir := newCollectionInstallTestVault(t)
	ctx := context.Background()
	team := InstallTarget{Kind: InstallKindTeam, Team: "platform"}

	if err := v.SetCollectionInstallation(ctx, "essentials", team); err != nil {
		t.Fatalf("SetCollectionInstallation: %v", err)
	}
	// Idempotent: the same target dedupes instead of stacking rows.
	if err := v.SetCollectionInstallation(ctx, "essentials", team); err != nil {
		t.Fatalf("SetCollectionInstallation (repeat): %v", err)
	}
	scopes := collectionScopes(t, dir, "essentials")
	if len(scopes) != 1 || scopes[0].Kind != manifest.ScopeKindTeam || scopes[0].Team != "platform" {
		t.Fatalf("collection scopes = %+v, want one team row", scopes)
	}

	// The member's own scopes are untouched by the collection install.
	m, _, err := manifest.LoadOrMigrate(dir)
	if err != nil {
		t.Fatalf("reload manifest: %v", err)
	}
	a := m.FindAsset("member")
	if len(a.Scopes) != 1 || a.Scopes[0].Repo != "github.com/acme/direct" {
		t.Fatalf("member scopes = %+v, want the direct repo row only", a.Scopes)
	}

	targets, present, err := v.CurrentCollectionInstallTargets(ctx, "essentials")
	if err != nil || !present {
		t.Fatalf("CurrentCollectionInstallTargets: present=%v err=%v", present, err)
	}
	if len(targets) != 1 || targets[0].Kind != InstallKindTeam || targets[0].Team != "platform" {
		t.Fatalf("targets = %+v", targets)
	}

	// Uninstalling the collection removes ITS row; the member's direct
	// repo scope survives (the provenance-loss case fan-out couldn't fix).
	if err := v.RemoveCollectionInstallation(ctx, "essentials", team); err != nil {
		t.Fatalf("RemoveCollectionInstallation: %v", err)
	}
	if scopes := collectionScopes(t, dir, "essentials"); len(scopes) != 0 {
		t.Fatalf("collection scopes after remove = %+v, want none", scopes)
	}
	m, _, _ = manifest.LoadOrMigrate(dir)
	if a := m.FindAsset("member"); len(a.Scopes) != 1 {
		t.Fatalf("member scopes after collection uninstall = %+v, want direct repo intact", a.Scopes)
	}
}

// Org-wide is an explicit, removable row on collections (a collection with
// no rows grants nothing), unlike assets where org-wide is the empty set.
func TestPathVault_CollectionOrgRow(t *testing.T) {
	v, dir := newCollectionInstallTestVault(t)
	ctx := context.Background()
	org := InstallTarget{Kind: InstallKindOrg}

	if err := v.SetCollectionInstallation(ctx, "essentials", org); err != nil {
		t.Fatalf("SetCollectionInstallation(org): %v", err)
	}
	// Idempotent like every other kind: a repeat must not stack a second
	// org row (installScopeMatches can't match org; the collection path
	// uses the org-aware matcher).
	if err := v.SetCollectionInstallation(ctx, "essentials", org); err != nil {
		t.Fatalf("SetCollectionInstallation(org, repeat): %v", err)
	}
	scopes := collectionScopes(t, dir, "essentials")
	if len(scopes) != 1 || scopes[0].Kind != manifest.ScopeKindOrg {
		t.Fatalf("collection scopes = %+v, want one org row", scopes)
	}
	if err := v.RemoveCollectionInstallation(ctx, "essentials", org); err != nil {
		t.Fatalf("RemoveCollectionInstallation(org): %v", err)
	}
	if scopes := collectionScopes(t, dir, "essentials"); len(scopes) != 0 {
		t.Fatalf("org row not removed: %+v", scopes)
	}
}

func TestPathVault_CollectionInstallValidation(t *testing.T) {
	v, _ := newCollectionInstallTestVault(t)
	ctx := context.Background()

	if err := v.SetCollectionInstallation(ctx, "missing", InstallTarget{Kind: InstallKindOrg}); err == nil {
		t.Fatalf("want error for unknown collection")
	}
	if err := v.SetCollectionInstallation(ctx, "essentials", InstallTarget{Kind: InstallKindTeam, Team: "ghosts"}); err == nil {
		t.Fatalf("want error for unknown team")
	}
	// Removing a row that isn't present is a no-op, matching asset installs.
	if err := v.RemoveCollectionInstallation(ctx, "essentials", InstallTarget{Kind: InstallKindTeam, Team: "platform"}); err != nil {
		t.Fatalf("remove absent row: %v", err)
	}
}

// Team and repo listers report collection-derived members alongside
// directly-scoped assets — the sidebar counts read the resolved reach.
func TestPathVault_CollectionScopesInListers(t *testing.T) {
	v, _ := newCollectionInstallTestVault(t)
	ctx := context.Background()

	if err := v.SetCollectionInstallation(ctx, "essentials", InstallTarget{Kind: InstallKindTeam, Team: "platform"}); err != nil {
		t.Fatalf("install to team: %v", err)
	}
	if err := v.SetCollectionInstallation(ctx, "essentials", InstallTarget{Kind: InstallKindRepo, Repo: "github.com/acme/infra"}); err != nil {
		t.Fatalf("install to repo: %v", err)
	}

	teams, err := v.ListTeamAssets(ctx)
	if err != nil {
		t.Fatalf("ListTeamAssets: %v", err)
	}
	if got := teams["platform"]; len(got) != 1 || got[0] != "member" {
		t.Fatalf("teams[platform] = %v, want [member]", got)
	}

	repos, err := v.ListRepoAssets(ctx)
	if err != nil {
		t.Fatalf("ListRepoAssets: %v", err)
	}
	if got := repos["github.com/acme/infra"]; len(got) != 1 || got[0] != "member" {
		t.Fatalf("repos[infra] = %v, want [member]", got)
	}
	if got := repos["github.com/acme/direct"]; len(got) != 1 || got[0] != "member" {
		t.Fatalf("repos[direct] = %v, want [member] (direct scope intact)", got)
	}
}
