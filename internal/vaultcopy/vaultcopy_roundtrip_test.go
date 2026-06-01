package vaultcopy_test

import (
	"archive/zip"
	"bytes"
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/manifest"
	"github.com/sleuth-io/sx/internal/metadata"
	"github.com/sleuth-io/sx/internal/mgmt"
	"github.com/sleuth-io/sx/internal/vault"
	"github.com/sleuth-io/sx/internal/vaultcopy"
)

// TestCopy_PathToPathRoundTrip seeds a source path vault with a team, a
// two-version asset (with a repo scope), audit events, and usage events, copies
// it into an empty destination path vault, and asserts everything landed.
func TestCopy_PathToPathRoundTrip(t *testing.T) {
	mgmt.ResetActorCache()
	ctx := context.Background()

	src := newSeededVault(t)
	dst := newEmptyVault(t)

	// Seed source.
	if err := src.CreateTeam(ctx, mgmt.Team{
		Name:    "platform",
		Members: []string{"alice@example.com", "bob@example.com"},
		Admins:  []string{"alice@example.com"},
	}); err != nil {
		t.Fatalf("seed team: %v", err)
	}
	for _, v := range []string{"1.0.0", "1.1.0"} {
		a := &lockfile.Asset{Name: "my-skill", Version: v, Type: asset.TypeSkill}
		if err := src.AddAsset(ctx, a, skillZip(t, "my-skill", v)); err != nil {
			t.Fatalf("seed asset %s: %v", v, err)
		}
	}
	if err := src.SetAssetInstallation(ctx, "my-skill", vault.InstallTarget{
		Kind: vault.InstallKindRepo, Repo: "github.com/acme/repo",
	}); err != nil {
		t.Fatalf("seed scope: %v", err)
	}
	// An org-wide asset: uploaded then registered globally (empty scopes).
	if err := src.AddAsset(ctx, &lockfile.Asset{Name: "global-skill", Version: "1.0.0", Type: asset.TypeSkill},
		skillZip(t, "global-skill", "1.0.0")); err != nil {
		t.Fatalf("seed global asset: %v", err)
	}
	if err := src.SetAssetInstallation(ctx, "global-skill", vault.InstallTarget{Kind: vault.InstallKindOrg}); err != nil {
		t.Fatalf("seed org scope: %v", err)
	}
	ts := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := src.ImportAuditEvents(ctx, []mgmt.AuditEvent{
		{Timestamp: ts, Actor: "alice@example.com", Event: "asset.created", TargetType: "asset", Target: "my-skill"},
	}); err != nil {
		t.Fatalf("seed audit: %v", err)
	}
	if err := src.RecordUsageEvents(ctx, []mgmt.UsageEvent{
		{Timestamp: ts, Actor: "bob@example.com", AssetName: "my-skill", AssetVersion: "1.1.0", AssetType: "skill"},
	}); err != nil {
		t.Fatalf("seed usage: %v", err)
	}

	// The management ops above (CreateTeam, AddAsset, SetAssetInstallation) each
	// emit their own audit events, so the source audit log holds more than the
	// one event we imported. A lossless copy carries all of them — capture the
	// source count to compare against.
	srcAudit, err := src.QueryAuditEvents(ctx, mgmt.AuditFilter{})
	if err != nil {
		t.Fatalf("read src audit: %v", err)
	}

	// Copy.
	report, err := vaultcopy.Copy(ctx, src, dst, vaultcopy.DefaultOptions())
	if err != nil {
		t.Fatalf("Copy: %v (warnings: %v)", err, report.Warnings)
	}
	if report.Teams != 1 || report.Assets != 2 || report.Versions != 3 || report.Scopes != 2 ||
		report.AuditEvents != len(srcAudit) || report.UsageEvents != 1 {
		t.Fatalf("report = %+v, want 1 team / 2 assets / 3 versions / 2 scopes / %d audit / 1 usage", report, len(srcAudit))
	}

	// Assert destination.
	teams, err := dst.ListTeams(ctx, vault.ListTeamsOptions{Limit: 50})
	if err != nil || len(teams.Teams) != 1 || teams.Teams[0].Name != "platform" {
		t.Fatalf("dst teams = %+v err=%v, want platform", teams, err)
	}
	team, err := dst.GetTeam(ctx, "platform")
	if err != nil || len(team.Members) != 2 || len(team.Admins) != 1 {
		t.Fatalf("dst team = %+v err=%v", team, err)
	}

	versions, err := dst.GetVersionList(ctx, "my-skill")
	if err != nil || len(versions) != 2 {
		t.Fatalf("dst versions = %v err=%v, want 2", versions, err)
	}

	scopeReader := dst.(interface {
		AssetInstallScopes(context.Context, string) ([]manifest.Scope, bool, error)
	})
	scopes, present, err := scopeReader.AssetInstallScopes(ctx, "my-skill")
	if err != nil || !present || len(scopes) != 1 || scopes[0].Kind != manifest.ScopeKindRepo {
		t.Fatalf("dst my-skill scopes = %+v present=%v err=%v, want one repo scope", scopes, present, err)
	}
	// The org-wide asset must be registered (present) with no scopes on dst.
	orgScopes, orgPresent, err := scopeReader.AssetInstallScopes(ctx, "global-skill")
	if err != nil || !orgPresent || len(orgScopes) != 0 {
		t.Fatalf("dst global-skill scopes = %+v present=%v err=%v, want registered org-wide", orgScopes, orgPresent, err)
	}

	// dst holds the imported source events PLUS the copy's own mutation audit
	// (CreateTeam/AddAsset/SetAssetInstallation on dst each emit one), so it's a
	// superset. What matters: the imported history is preserved verbatim.
	audit, err := dst.QueryAuditEvents(ctx, mgmt.AuditFilter{})
	if err != nil || len(audit) < len(srcAudit) {
		t.Fatalf("dst audit count = %d err=%v, want >= %d", len(audit), err, len(srcAudit))
	}
	// The imported event kept its original (older) timestamp, so it sorts last.
	if got := audit[len(audit)-1]; !got.Timestamp.Equal(ts) || got.Actor != "alice@example.com" {
		t.Fatalf("oldest dst audit = %+v, want imported event at %v by alice", got, ts)
	}

	usage, err := dst.ReadUsageEvents(ctx, mgmt.UsageFilter{})
	if err != nil || len(usage) != 1 || usage[0].Actor != "bob@example.com" {
		t.Fatalf("dst usage = %+v err=%v", usage, err)
	}

	// Re-copying assets is idempotent: a path vault overwrites identical
	// versions in place (Sleuth signals ErrVersionExists instead), so neither
	// path duplicates versions.
	if _, err := vaultcopy.Copy(ctx, src, dst, vaultcopy.Options{Assets: true}); err != nil {
		t.Fatalf("re-copy: %v", err)
	}
	if versions, err := dst.GetVersionList(ctx, "my-skill"); err != nil || len(versions) != 2 {
		t.Fatalf("after re-copy dst versions = %v err=%v, want 2 (no duplication)", versions, err)
	}
}

func newEmptyVault(t *testing.T) vault.Vault {
	t.Helper()
	dir := t.TempDir()
	gitInit(t, dir)
	if err := manifest.Save(dir, &manifest.Manifest{SchemaVersion: manifest.CurrentSchemaVersion}); err != nil {
		t.Fatalf("manifest.Save: %v", err)
	}
	v, err := vault.NewPathVault("file://" + dir)
	if err != nil {
		t.Fatalf("NewPathVault: %v", err)
	}
	return v
}

func newSeededVault(t *testing.T) vault.Vault { return newEmptyVault(t) }

func gitInit(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "admin@example.com"},
		{"config", "user.name", "Admin"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func skillZip(t *testing.T, name, ver string) []byte {
	t.Helper()
	meta, err := metadata.Marshal(&metadata.Metadata{
		MetadataVersion: metadata.CurrentMetadataVersion,
		Asset:           metadata.Asset{Name: name, Version: ver, Type: asset.TypeSkill},
		Skill:           &metadata.SkillConfig{},
	})
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	write := func(fname string, data []byte) {
		w, err := zw.Create(fname)
		if err != nil {
			t.Fatalf("zip create %s: %v", fname, err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatalf("zip write %s: %v", fname, err)
		}
	}
	write("metadata.toml", meta)
	write("SKILL.md", []byte("# "+name+"\nversion "+ver+"\n"))
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}
