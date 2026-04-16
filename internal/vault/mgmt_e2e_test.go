package vault

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/mgmt"
)

// TestPathVault_TeamUserLifecycleE2E exercises the full management flow on
// a path vault: create a team, add members, install an asset to both a
// team and a user, record usage events, then assert the queried state.
// This mirrors the real-world CLI flow without actually spawning `sx`.
func TestPathVault_TeamUserLifecycleE2E(t *testing.T) {
	mgmt.ResetActorCache()
	dir := t.TempDir()

	// Initialize a real git repo so mgmt.CurrentGitActor can resolve the
	// caller identity via `git config user.email`.
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "alice@example.com")
	runGit(t, dir, "config", "user.name", "Alice Admin")

	// Seed the vault with a bare skill.lock so AddAsset-style flows are
	// not required. The fields match the minimum schema the parser needs.
	seedLock := []byte(`lock-version = "1.0"
version = "test"
created-by = "test"

[[assets]]
name = "my-skill"
version = "1.0.0"
type = "skill"

  [assets.source-http]
  url = "https://example.com/my-skill.zip"

  [[assets.scopes]]
  repo = "github.com/acme/baseline"

[[assets]]
name = "other-skill"
version = "1.0.0"
type = "rule"

  [other.source-http]
  url = "https://example.com/other.zip"
`)
	if err := writeFile(filepath.Join(dir, "sx.lock"), seedLock); err != nil {
		t.Fatalf("write seed lock: %v", err)
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

	// 2. List teams and verify it comes back.
	teams, err := v.ListTeams(ctx)
	if err != nil {
		t.Fatalf("ListTeams failed: %v", err)
	}
	if len(teams) != 1 || teams[0].Name != "platform" {
		t.Fatalf("expected 1 team 'platform', got %+v", teams)
	}
	if len(teams[0].Members) != 2 {
		t.Errorf("expected 2 members, got %d", len(teams[0].Members))
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

	// 5. Fetch the lock file and verify overlay: alice is a member of
	// platform, so she should see the team's repos merged with her base
	// repo scope. Alice is also the user-scoped target, which marks the
	// asset global (empty Scopes wins over team expansion).
	raw, _, _, err := v.GetLockFile(ctx, "")
	if err != nil {
		t.Fatalf("GetLockFile failed: %v", err)
	}
	lf, err := lockfile.Parse(raw)
	if err != nil {
		t.Fatalf("parse lock: %v", err)
	}
	var mySkill *lockfile.Asset
	for i := range lf.Assets {
		if lf.Assets[i].Name == "my-skill" {
			mySkill = &lf.Assets[i]
		}
	}
	if mySkill == nil {
		t.Fatal("my-skill not found in overlaid lock file")
		return
	}
	if len(mySkill.Scopes) != 0 {
		t.Errorf("expected my-skill to be global (user scope wins), got scopes %+v", mySkill.Scopes)
	}

	// 6. Clear and re-install to just the team — alice should now see
	// the team repos in her lock file.
	if err := v.ClearAssetInstallations(ctx, "my-skill"); err != nil {
		t.Fatalf("ClearAssetInstallations failed: %v", err)
	}
	// ClearAssetInstallations also wipes the asset's Scopes in skill.lock,
	// so reseed one baseline scope to verify overlay math against a
	// non-empty starting state.
	lockfile.Write(&lockfile.LockFile{
		LockVersion: lf.LockVersion,
		Version:     lf.Version,
		CreatedBy:   lf.CreatedBy,
		Assets: []lockfile.Asset{
			{
				Name:       "my-skill",
				Version:    "1.0.0",
				Type:       lf.Assets[0].Type,
				SourceHTTP: &lockfile.SourceHTTP{URL: "https://example.com/my-skill.zip"},
				Scopes:     []lockfile.Scope{{Repo: "github.com/acme/baseline"}},
			},
		},
	}, filepath.Join(dir, "sx.lock"))
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
	for i := range lf.Assets {
		if lf.Assets[i].Name == "my-skill" {
			mySkill = &lf.Assets[i]
		}
	}
	if mySkill == nil || len(mySkill.Scopes) < 2 {
		t.Fatalf("expected overlay to add team repos, got %+v", mySkill)
	}

	// 7. Record usage events for both alice and bob against my-skill, and
	// one for a third user against a different asset.
	events := []mgmt.UsageEvent{
		{Actor: "alice@example.com", AssetName: "my-skill", AssetVersion: "1.0.0", AssetType: "skill"},
		{Actor: "bob@example.com", AssetName: "my-skill", AssetVersion: "1.0.0", AssetType: "skill"},
		{Actor: "alice@example.com", AssetName: "other-skill", AssetVersion: "1.0.0", AssetType: "rule"},
	}
	if err := v.RecordUsageEvents(ctx, events); err != nil {
		t.Fatalf("RecordUsageEvents failed: %v", err)
	}

	// 8. Query stats: my-skill should be the top asset.
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

	// 9. Audit events should include team create, repo add, install set,
	// and install cleared — at least these five.
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
		found := false
		for _, ev := range events2 {
			if ev.Event == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected audit event %q not found", want)
		}
	}

	// 10. Delete the team and verify cascade removal of installations.
	if err := v.DeleteTeam(ctx, "platform"); err != nil {
		t.Fatalf("DeleteTeam failed: %v", err)
	}
	teams, err = v.ListTeams(ctx)
	if err != nil {
		t.Fatalf("ListTeams after delete failed: %v", err)
	}
	if len(teams) != 0 {
		t.Errorf("expected no teams, got %d", len(teams))
	}

	// Installations row for the deleted team should be gone.
	ifile, _, err := mgmt.LoadInstallations(dir)
	if err != nil {
		t.Fatalf("LoadInstallations failed: %v", err)
	}
	for _, ins := range ifile.Installations {
		if ins.Kind == mgmt.InstallKindTeam && ins.Team == "platform" {
			t.Errorf("expected team install row to be cascade-deleted, still found: %+v", ins)
		}
	}
}

// TestPathVault_NoInstallationsFileLockFilePassthrough verifies the fast
// path: when .sx/installations.toml does not exist, GetLockFile returns
// the raw skill.lock bytes unchanged so legacy vaults have zero overhead.
func TestPathVault_NoInstallationsFileLockFilePassthrough(t *testing.T) {
	mgmt.ResetActorCache()
	dir := t.TempDir()
	raw := []byte(`lock-version = "1.0"
version = "test"
created-by = "test"

[[assets]]
name = "foo"
version = "1.0.0"
type = "skill"
`)
	if err := writeFile(filepath.Join(dir, "sx.lock"), raw); err != nil {
		t.Fatalf("write: %v", err)
	}

	v, err := NewPathVault("file://" + dir)
	if err != nil {
		t.Fatalf("NewPathVault failed: %v", err)
	}

	got, _, _, err := v.GetLockFile(context.Background(), "")
	if err != nil {
		t.Fatalf("GetLockFile failed: %v", err)
	}
	if string(got) != string(raw) {
		t.Errorf("expected raw passthrough, got modified bytes:\nwant=%q\ngot=%q", raw, got)
	}
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
