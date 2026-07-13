package vault

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/lockfile"
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
	return slices.Contains(kinds, want)
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

// Org is exclusive and always a REPLACE on the server, whatever
// appendMode the caller passed: forwarding append=true would send
// "append nothing" and leave existing narrower scopes in place, so an
// org add on a governed library would silently fail to go global.
func TestSleuthOrgInstallAlwaysReplaces(t *testing.T) {
	var gotAppend *bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Variables struct {
				Input struct {
					Append *bool `json:"append"`
				} `json:"input"`
			} `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		gotAppend = req.Variables.Input.Append
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"setAssetInstallations":{"errors":[]}}}`))
	}))
	t.Cleanup(server.Close)

	v := NewSleuthVault(server.URL, "test-token")
	skipped, err := v.SetAssetInstallations(context.Background(), "my-skill",
		[]InstallTarget{{Kind: InstallKindOrg}}, true /* appendMode */)
	if err != nil || len(skipped) != 0 {
		t.Fatalf("SetAssetInstallations: skipped=%v err=%v", skipped, err)
	}
	if gotAppend == nil || *gotAppend {
		t.Fatalf("org install sent append=%v, want explicit false (replace)", gotAppend)
	}
}

// TestPathVault_SetPublishedAssetInstallations_AdvancesRow pins issue #191's
// single-transaction fix: applying scopes for a just-published version must
// advance the manifest row to that version in the same write, carrying
// existing scopes through and then applying the new targets.
func TestPathVault_SetPublishedAssetInstallations_AdvancesRow(t *testing.T) {
	v, dir := seedBulkInstallVault(t)
	ctx := context.Background()

	published := &lockfile.Asset{
		Name: "my-skill", Version: "2.0.0", Type: asset.TypeSkill,
		SourcePath: &lockfile.SourcePath{Path: ".sx/versions/my-skill/2.0.0"},
	}
	skipped, err := v.SetPublishedAssetInstallations(ctx, published, []InstallTarget{
		{Kind: InstallKindRepo, Repo: "github.com/acme/extra"},
	}, true)
	if err != nil || len(skipped) != 0 {
		t.Fatalf("SetPublishedAssetInstallations: skipped=%v err=%v", skipped, err)
	}

	m, _, err := manifest.LoadOrMigrate(dir)
	if err != nil {
		t.Fatal(err)
	}
	a := m.FindAsset("my-skill")
	if a == nil || a.Version != "2.0.0" {
		t.Fatalf("row = %+v, want version 2.0.0", a)
	}
	if a.SourcePath == nil || a.SourcePath.Path != ".sx/versions/my-skill/2.0.0" {
		t.Errorf("source path = %+v, want the published archive path", a.SourcePath)
	}
	if len(a.Scopes) != 2 {
		t.Errorf("scopes = %+v, want baseline repo scope + appended repo scope", a.Scopes)
	}
}

// TestPathVault_SetPublishedAssetInstallations_AllSkippedLeavesRow pins the
// atomicity half of the fix: when every requested target is skipped, the
// version must NOT advance either — the manifest stays exactly as it was,
// rather than committing a version bump whose scope change never landed.
func TestPathVault_SetPublishedAssetInstallations_AllSkippedLeavesRow(t *testing.T) {
	v, dir := seedBulkInstallVault(t)
	ctx := context.Background()

	published := &lockfile.Asset{
		Name: "my-skill", Version: "2.0.0", Type: asset.TypeSkill,
		SourcePath: &lockfile.SourcePath{Path: ".sx/versions/my-skill/2.0.0"},
	}
	skipped, err := v.SetPublishedAssetInstallations(ctx, published, []InstallTarget{
		{Kind: InstallKindTeam, Team: "ghost-team"},
	}, false)
	if err != nil {
		t.Fatalf("SetPublishedAssetInstallations: %v", err)
	}
	if len(skipped) != 1 {
		t.Fatalf("skipped = %+v, want the unknown team reported", skipped)
	}

	m, _, err := manifest.LoadOrMigrate(dir)
	if err != nil {
		t.Fatal(err)
	}
	a := m.FindAsset("my-skill")
	if a == nil || a.Version != "1.0.0" {
		t.Fatalf("row = %+v, want version still 1.0.0 (nothing applied, nothing advanced)", a)
	}
	if len(a.Scopes) != 1 || a.Scopes[0].Kind != manifest.ScopeKindRepo {
		t.Errorf("scopes = %+v, want the baseline repo scope untouched", a.Scopes)
	}
}

// TestGitVault_SetPublishedAssetInstallations_OneCommit pins the other half
// of the issue #191 review feedback: a scoped republish must land the version
// advance and the scope change as ONE commit on git vaults, not a version
// commit followed by a separate scope commit.
func TestGitVault_SetPublishedAssetInstallations_OneCommit(t *testing.T) {
	mgmt.ResetActorCache()
	t.Setenv("SX_CACHE_DIR", t.TempDir())

	remoteDir := filepath.Join(t.TempDir(), "vault.git")
	gitRun(t, "", "init", "--bare", "-b", "main", remoteDir)

	seedDir := filepath.Join(t.TempDir(), "seed")
	gitRun(t, "", "init", "-b", "main", seedDir)
	gitRun(t, seedDir, "config", "user.email", "seed@example.com")
	gitRun(t, seedDir, "config", "user.name", "Seed")
	for _, rel := range []string{
		".sx/versions/my-skill/1/SKILL.md",
		"assets/my-skill/SKILL.md",
	} {
		path := filepath.Join(seedDir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("# my-skill"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(seedDir, ".sx", "versions", "my-skill", "list.txt"), []byte("1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := manifest.Save(seedDir, &manifest.Manifest{
		SchemaVersion: manifest.CurrentSchemaVersion,
		Assets: []manifest.Asset{{
			Name: "my-skill", Version: "1", Type: asset.TypeSkill,
			SourcePath: &manifest.SourcePath{Path: ".sx/versions/my-skill/1"},
			Scopes: []manifest.Scope{
				{Kind: manifest.ScopeKindRepo, Repo: "github.com/acme/baseline"},
			},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	gitRun(t, seedDir, "add", ".")
	gitRun(t, seedDir, "commit", "-m", "seed v2 vault")
	gitRun(t, seedDir, "remote", "add", "origin", remoteDir)
	gitRun(t, seedDir, "push", "origin", "main")

	v, err := NewGitVault("file://" + remoteDir)
	if err != nil {
		t.Fatalf("NewGitVault: %v", err)
	}
	if _, _, _, err := v.GetLockFile(context.Background(), ""); err != nil {
		t.Fatalf("GetLockFile (forces clone): %v", err)
	}
	gitRun(t, v.repoPath, "config", "user.email", "alice@example.com")
	gitRun(t, v.repoPath, "config", "user.name", "Alice")
	mgmt.ResetActorCache()

	before := strings.TrimSpace(gitOut(t, "", "--git-dir", remoteDir, "rev-list", "--count", "main"))

	published := &lockfile.Asset{
		Name: "my-skill", Version: "2", Type: asset.TypeSkill,
		SourcePath: &lockfile.SourcePath{Path: ".sx/versions/my-skill/2"},
	}
	skipped, err := v.SetPublishedAssetInstallations(context.Background(), published,
		[]InstallTarget{{Kind: InstallKindOrg}}, false)
	if err != nil || len(skipped) != 0 {
		t.Fatalf("SetPublishedAssetInstallations: skipped=%v err=%v", skipped, err)
	}

	after := strings.TrimSpace(gitOut(t, "", "--git-dir", remoteDir, "rev-list", "--count", "main"))
	beforeN, _ := strconv.Atoi(before)
	afterN, _ := strconv.Atoi(after)
	if afterN != beforeN+1 {
		log := gitOut(t, "", "--git-dir", remoteDir, "log", "--format=%s", "main")
		t.Fatalf("commits: before=%d after=%d, want exactly one new commit; log:\n%s", beforeN, afterN, log)
	}

	verify := filepath.Join(t.TempDir(), "verify")
	gitRun(t, "", "clone", remoteDir, verify)
	m, _, err := manifest.Load(verify)
	if err != nil {
		t.Fatal(err)
	}
	a := m.FindAsset("my-skill")
	if a == nil || a.Version != "2" {
		t.Fatalf("row = %+v, want version 2 on the remote", a)
	}
	if len(a.Scopes) != 0 {
		t.Errorf("scopes = %+v, want none (org-wide replace)", a.Scopes)
	}
}
