package vaultcopy_test

import (
	"archive/zip"
	"bytes"
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/sleuth-io/sx/v2/internal/asset"
	"github.com/sleuth-io/sx/v2/internal/lockfile"
	"github.com/sleuth-io/sx/v2/internal/manifest"
	"github.com/sleuth-io/sx/v2/internal/metadata"
	"github.com/sleuth-io/sx/v2/internal/mgmt"
	"github.com/sleuth-io/sx/v2/internal/vault"
	"github.com/sleuth-io/sx/v2/internal/vaultcopy"
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
	// A collection with membership and its own team install row — copied as
	// a unit (one row on the collection, never fanned out onto members).
	if err := src.(vault.CollectionStore).SaveCollection(ctx, manifest.Collection{
		Name: "essentials", Description: "Starter set", Assets: []string{"my-skill"},
	}); err != nil {
		t.Fatalf("seed collection: %v", err)
	}
	if err := src.(vault.CollectionInstaller).SetCollectionInstallation(ctx, "essentials", vault.InstallTarget{
		Kind: vault.InstallKindTeam, Team: "platform",
	}); err != nil {
		t.Fatalf("seed collection install: %v", err)
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
	if report.Teams != 1 || report.Assets != 2 || report.Versions != 3 || report.Scopes != 3 ||
		report.Collections != 1 || report.AuditEvents != len(srcAudit) || report.UsageEvents != 1 {
		t.Fatalf("report = %+v, want 1 team / 2 assets / 3 versions / 1 collection / 3 scopes / %d audit / 1 usage", report, len(srcAudit))
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
	// The collection landed with membership and its own install row; the
	// member asset's scopes were NOT widened by the collection install.
	dstCols, err := dst.(vault.CollectionStore).ListCollections(ctx)
	if err != nil || len(dstCols) != 1 || dstCols[0].Name != "essentials" ||
		len(dstCols[0].Assets) != 1 || dstCols[0].Assets[0] != "my-skill" {
		t.Fatalf("dst collections = %+v err=%v, want essentials with my-skill", dstCols, err)
	}
	colTargets, colPresent, err := dst.(vault.CollectionInstaller).CurrentCollectionInstallTargets(ctx, "essentials")
	if err != nil || !colPresent || len(colTargets) != 1 ||
		colTargets[0].Kind != vault.InstallKindTeam || colTargets[0].Team != "platform" {
		t.Fatalf("dst collection installs = %+v present=%v err=%v, want one platform team row", colTargets, colPresent, err)
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

// emptyVersionListVault simulates a backend that exposes no version history
// for an asset even though the asset (and its latest version) is downloadable —
// the copy engine must fall back to the summary's latest version instead of
// silently copying nothing.
type emptyVersionListVault struct {
	vault.Vault
}

func (v *emptyVersionListVault) GetVersionList(context.Context, string) ([]string, error) {
	return nil, nil
}

func TestCopy_EmptyVersionListFallsBackToLatest(t *testing.T) {
	mgmt.ResetActorCache()
	ctx := context.Background()

	src := newSeededVault(t)
	dst := newEmptyVault(t)

	for _, v := range []string{"1.0.0", "1.1.0"} {
		a := &lockfile.Asset{Name: "my-skill", Version: v, Type: asset.TypeSkill}
		if err := src.AddAsset(ctx, a, skillZip(t, "my-skill", v)); err != nil {
			t.Fatalf("seed asset %s: %v", v, err)
		}
	}

	report, err := vaultcopy.Copy(ctx, &emptyVersionListVault{src}, dst, vaultcopy.Options{Assets: true})
	if err != nil {
		t.Fatalf("Copy: %v (warnings: %v)", err, report.Warnings)
	}
	if report.Assets != 1 || report.Versions != 1 {
		t.Fatalf("report = %+v, want 1 asset / 1 version (latest only)", report)
	}
	found := false
	for _, w := range report.Warnings {
		if strings.Contains(w, "no version history") && strings.Contains(w, "my-skill") {
			found = true
		}
	}
	if !found {
		t.Fatalf("want a no-version-history warning, got %v", report.Warnings)
	}
	versions, err := dst.GetVersionList(ctx, "my-skill")
	if err != nil || len(versions) != 1 || versions[0] != "1.1.0" {
		t.Fatalf("dst versions = %v err=%v, want just latest 1.1.0", versions, err)
	}
}

// A user scope belonging to someone other than the operator must survive the
// copy: the engine's trusted bulk write lifts the self-only restriction, which
// exists to stop interactive privilege escalation, not faithful migration.
func TestCopy_ForeignUserScopeCopied(t *testing.T) {
	mgmt.ResetActorCache()
	ctx := context.Background()

	src := newSeededVault(t)
	dst := newEmptyVault(t)

	a := &lockfile.Asset{Name: "my-skill", Version: "1.0.0", Type: asset.TypeSkill}
	if err := src.AddAsset(ctx, a, skillZip(t, "my-skill", "1.0.0")); err != nil {
		t.Fatalf("seed asset: %v", err)
	}
	// Seed a scope for a user who is not the operator (admin@example.com);
	// only a trusted write can record it, same as the copy engine uses.
	bulk := src.(interface {
		SetAssetInstallations(context.Context, string, []vault.InstallTarget, bool) ([]vault.SkippedTarget, error)
	})
	skipped, err := bulk.SetAssetInstallations(vault.ContextWithTrustedScopeWrite(ctx), "my-skill",
		[]vault.InstallTarget{{Kind: vault.InstallKindUser, User: "bob@example.com"}}, false)
	if err != nil || len(skipped) != 0 {
		t.Fatalf("seed foreign user scope: err=%v skipped=%+v", err, skipped)
	}

	report, err := vaultcopy.Copy(ctx, src, dst, vaultcopy.Options{Assets: true})
	if err != nil {
		t.Fatalf("Copy: %v (warnings: %v)", err, report.Warnings)
	}
	if report.Scopes != 1 {
		t.Fatalf("report = %+v (warnings: %v), want 1 scope", report, report.Warnings)
	}
	scopeReader := dst.(interface {
		AssetInstallScopes(context.Context, string) ([]manifest.Scope, bool, error)
	})
	scopes, present, err := scopeReader.AssetInstallScopes(ctx, "my-skill")
	if err != nil || !present || len(scopes) != 1 ||
		scopes[0].Kind != manifest.ScopeKindUser || scopes[0].User != "bob@example.com" {
		t.Fatalf("dst scopes = %+v present=%v err=%v, want bob@example.com user scope", scopes, present, err)
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

// A collection's "just for me" install belonging to another user must survive
// the copy without tripping the destination's self-only rule — the engine's
// trusted write covers collection installs the same as asset scopes.
func TestCopy_ForeignUserScopeOnCollectionCopied(t *testing.T) {
	mgmt.ResetActorCache()
	ctx := context.Background()

	src := newSeededVault(t)
	dst := newEmptyVault(t)

	if err := src.(vault.CollectionStore).SaveCollection(ctx, manifest.Collection{Name: "essentials"}); err != nil {
		t.Fatalf("seed collection: %v", err)
	}
	if err := src.(vault.CollectionInstaller).SetCollectionInstallation(
		vault.ContextWithTrustedScopeWrite(ctx), "essentials",
		vault.InstallTarget{Kind: vault.InstallKindUser, User: "bob@example.com"},
	); err != nil {
		t.Fatalf("seed foreign user install: %v", err)
	}

	report, err := vaultcopy.Copy(ctx, src, dst, vaultcopy.Options{Collections: true})
	if err != nil {
		t.Fatalf("Copy: %v (warnings: %v)", err, report.Warnings)
	}
	for _, w := range report.Warnings {
		if strings.Contains(w, "may only target the authenticated caller") {
			t.Fatalf("copy tripped the self-only rule: %v", report.Warnings)
		}
	}
	targets, present, err := dst.(vault.CollectionInstaller).CurrentCollectionInstallTargets(ctx, "essentials")
	if err != nil || !present || len(targets) != 1 ||
		targets[0].Kind != vault.InstallKindUser || targets[0].User != "bob@example.com" {
		t.Fatalf("dst targets = %+v present=%v err=%v, want bob@example.com user target", targets, present, err)
	}
}

// The bot-key note in the copy report must match the destination: a file
// vault issues no API keys, so telling the operator to run `sx bot key create`
// (which errors there) would contradict the migration docs.
func TestCopy_BotKeyNoteMatchesDestination(t *testing.T) {
	mgmt.ResetActorCache()
	ctx := context.Background()

	src := newSeededVault(t)
	dst := newEmptyVault(t)
	if _, err := src.CreateBot(ctx, mgmt.Bot{Name: "ci-reviewer", Description: "PR review job"}); err != nil {
		t.Fatalf("seed bot: %v", err)
	}

	report, err := vaultcopy.Copy(ctx, src, dst, vaultcopy.Options{Bots: true})
	if err != nil {
		t.Fatalf("Copy: %v (warnings: %v)", err, report.Warnings)
	}
	if report.Bots != 1 {
		t.Fatalf("report = %+v, want 1 bot", report)
	}
	var sawNote bool
	for _, w := range report.Warnings {
		if strings.Contains(w, "sx bot key create") {
			t.Fatalf("file-vault destination must not be told to create API keys: %q", w)
		}
		if strings.Contains(w, "SX_BOT") {
			sawNote = true
		}
	}
	if !sawNote {
		t.Fatalf("want the SX_BOT bot-identity note, got %v", report.Warnings)
	}
}

// keyIssuingVault wraps a file vault so it satisfies vault.BotApiKeyManager —
// the probe `sx bot key create` uses — standing in for a skills.new-style
// destination that does issue bot API keys.
type keyIssuingVault struct {
	vault.Vault
}

func (v *keyIssuingVault) CreateBotApiKey(context.Context, string, string) (string, mgmt.BotApiKey, error) {
	return "raw", mgmt.BotApiKey{ID: "k1"}, nil
}
func (v *keyIssuingVault) ListBotApiKeys(context.Context, string) ([]mgmt.BotApiKey, error) {
	return nil, nil
}
func (v *keyIssuingVault) DeleteBotApiKey(context.Context, string, string) error { return nil }

// A destination that does issue keys must get the `sx bot key create` note —
// the counterpart of TestCopy_BotKeyNoteMatchesDestination, so neither branch
// can be inverted or dropped unnoticed.
func TestCopy_BotKeyNoteForKeyIssuingDestination(t *testing.T) {
	mgmt.ResetActorCache()
	ctx := context.Background()

	src := newSeededVault(t)
	dst := &keyIssuingVault{newEmptyVault(t)}
	if _, err := src.CreateBot(ctx, mgmt.Bot{Name: "ci-reviewer"}); err != nil {
		t.Fatalf("seed bot: %v", err)
	}

	report, err := vaultcopy.Copy(ctx, src, dst, vaultcopy.Options{Bots: true})
	if err != nil {
		t.Fatalf("Copy: %v (warnings: %v)", err, report.Warnings)
	}
	var sawCreate bool
	for _, w := range report.Warnings {
		if strings.Contains(w, "SX_BOT") {
			t.Fatalf("key-issuing destination must not get the identity-only note: %q", w)
		}
		if strings.Contains(w, "sx bot key create") {
			sawCreate = true
		}
	}
	if !sawCreate {
		t.Fatalf("want the 'sx bot key create' note, got %v", report.Warnings)
	}
}
