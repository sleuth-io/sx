package commands

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/clients"
	"github.com/sleuth-io/sx/internal/config"
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/scope"
	"github.com/sleuth-io/sx/internal/ui"
)

// buildProfileLock constructs a profileLockFile suitable for merge
// tests without touching the disk. Each asset is global-scoped so
// scope.NewMatcher matches everything.
func buildProfileLock(profile string, assetNames ...string) profileLockFile {
	lf := &lockfile.LockFile{LockVersion: "1"}
	for _, name := range assetNames {
		lf.Assets = append(lf.Assets, lockfile.Asset{
			Name:    name,
			Version: "1.0.0",
			Type:    asset.TypeSkill,
		})
	}
	return profileLockFile{
		ProfileName: profile,
		Config:      &config.Config{ProfileName: profile},
		LockFile:    lf,
	}
}

func mergeAssetNames(assets []*lockfile.Asset) []string {
	out := make([]string, 0, len(assets))
	for _, a := range assets {
		out = append(out, a.Name)
	}
	return out
}

func TestMergeApplicableAssets_DefaultFirstWins(t *testing.T) {
	clientList := []clients.Client{&stubClient{id: "claude-code"}}
	matcher := scope.NewMatcher(&scope.Scope{Type: lockfile.ScopeGlobal})

	// "work" is default, listed first per GetActiveProfileNames bubbling.
	locks := []profileLockFile{
		buildProfileLock("work", "shared", "work-only"),
		buildProfileLock("personal", "shared", "personal-only"),
	}
	sortedAssets, origin, conflicts, err := mergeApplicableAssets(locks, clientList, matcher)
	if err != nil {
		t.Fatalf("mergeApplicableAssets: %v", err)
	}
	names := mergeAssetNames(sortedAssets)
	slices.Sort(names)
	wantNames := []string{"personal-only", "shared", "work-only"}
	if !slices.Equal(names, wantNames) {
		t.Fatalf("names=%v want %v", names, wantNames)
	}
	if len(conflicts) != 1 || conflicts[0].AssetName != "shared" {
		t.Fatalf("expected one conflict for shared, got %v", conflicts)
	}
	if conflicts[0].Winner != "work" {
		t.Fatalf("winner=%s want work", conflicts[0].Winner)
	}
	if !slices.Equal(conflicts[0].Shadowed, []string{"personal"}) {
		t.Fatalf("shadowed=%v want [personal]", conflicts[0].Shadowed)
	}
	if origin["shared"] != "work" {
		t.Fatalf("origin[shared]=%s want work", origin["shared"])
	}
	if origin["personal-only"] != "personal" {
		t.Fatalf("origin[personal-only]=%s want personal", origin["personal-only"])
	}
}

func TestMergeApplicableAssets_ThreeWayConflict(t *testing.T) {
	clientList := []clients.Client{&stubClient{id: "claude-code"}}
	matcher := scope.NewMatcher(&scope.Scope{Type: lockfile.ScopeGlobal})

	locks := []profileLockFile{
		buildProfileLock("a", "dup"),
		buildProfileLock("b", "dup"),
		buildProfileLock("c", "dup"),
	}
	_, _, conflicts, err := mergeApplicableAssets(locks, clientList, matcher)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if len(conflicts) != 1 {
		t.Fatalf("expected single conflict record, got %d", len(conflicts))
	}
	if conflicts[0].Winner != "a" {
		t.Fatalf("winner=%s want a", conflicts[0].Winner)
	}
	if !slices.Equal(conflicts[0].Shadowed, []string{"b", "c"}) {
		t.Fatalf("shadowed=%v want [b c]", conflicts[0].Shadowed)
	}
}

func TestReportConflicts_DefaultWinsIsMuted(t *testing.T) {
	var buf bytes.Buffer
	out := ui.NewOutput(&buf, &buf)
	reportConflicts([]assetConflict{{AssetName: "shared", Winner: "work", Shadowed: []string{"personal"}}}, "work", out)
	// Warning should NOT appear when default wins.
	if strings.Contains(buf.String(), "Warning") {
		t.Fatalf("expected muted output when default wins, got: %s", buf.String())
	}
	if !strings.Contains(buf.String(), "shared") {
		t.Fatalf("expected message to mention asset name, got: %s", buf.String())
	}
}

func TestReportConflicts_NonDefaultWinsWarns(t *testing.T) {
	var buf bytes.Buffer
	out := ui.NewOutput(&buf, &buf)
	// Default is "work" but winner is "personal" — should warn loudly.
	reportConflicts([]assetConflict{{AssetName: "shared", Winner: "personal", Shadowed: []string{"work"}}}, "work", out)
	got := buf.String()
	if !strings.Contains(got, "shared") {
		t.Fatalf("expected warning to name asset, got: %s", got)
	}
}

func TestBuildProfileMetadataSkipsFailures(t *testing.T) {
	locks := []profileLockFile{
		{ProfileName: "good", Config: &config.Config{Identity: "good@x.com", ProfileName: "good"}, LockFile: &lockfile.LockFile{}},
		{ProfileName: "fetch-failed", Config: &config.Config{Identity: "x@x.com"}, FetchErr: errors.New("boom")},
		{ProfileName: "no-config", LockFile: &lockfile.LockFile{}},
	}
	meta := buildProfileMetadata(locks)
	if _, ok := meta["good"]; !ok {
		t.Fatalf("good should be in metadata")
	}
	if _, ok := meta["fetch-failed"]; ok {
		t.Fatalf("fetch-failed should not be in metadata (no lockfile)")
	}
	if _, ok := meta["no-config"]; ok {
		t.Fatalf("no-config should not be in metadata (nil config)")
	}
	if meta["good"].Identity != "good@x.com" {
		t.Fatalf("identity carried wrong, got %q", meta["good"].Identity)
	}
}
