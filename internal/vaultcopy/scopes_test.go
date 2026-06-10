package vaultcopy

import (
	"context"
	"errors"
	"testing"

	"github.com/sleuth-io/sx/internal/manifest"
	"github.com/sleuth-io/sx/internal/vault"
)

// bulkFake implements the bulk (replace-on-set) installer, like the Sleuth
// vault. It records calls and can report unresolved targets or a hard error.
type bulkFake struct {
	bulkCalls  [][]vault.InstallTarget
	clearCalls int
	skipped    []vault.SkippedTarget
	bulkErr    error
}

func (f *bulkFake) SetAssetInstallation(_ context.Context, _ string, _ vault.InstallTarget) error {
	panic("bulkFake.SetAssetInstallation must not be called — bulk dests use SetAssetInstallations")
}

func (f *bulkFake) ClearAssetInstallations(_ context.Context, _ string) error {
	f.clearCalls++
	return nil
}

func (f *bulkFake) SetAssetInstallations(_ context.Context, _ string, targets []vault.InstallTarget, _ bool) ([]vault.SkippedTarget, error) {
	f.bulkCalls = append(f.bulkCalls, targets)
	return f.skipped, f.bulkErr
}

// appendFake implements only the per-target installer, like a file-backed vault.
type appendFake struct {
	singleCalls []vault.InstallTarget
	clearCalls  int
}

func (f *appendFake) SetAssetInstallation(_ context.Context, _ string, t vault.InstallTarget) error {
	f.singleCalls = append(f.singleCalls, t)
	return nil
}

func (f *appendFake) ClearAssetInstallations(_ context.Context, _ string) error {
	f.clearCalls++
	return nil
}

func TestCopyAssetScopes_BulkDestSetsAllAtOnce(t *testing.T) {
	f := &bulkFake{}
	r := &Report{}
	scopes := []manifest.Scope{
		{Kind: manifest.ScopeKindRepo, Repo: "github.com/acme/a"},
		{Kind: manifest.ScopeKindTeam, Team: "platform"},
	}
	copyAssetScopes(context.Background(), f, "skill", scopes, true, false, r)

	if len(f.bulkCalls) != 1 || len(f.bulkCalls[0]) != 2 {
		t.Fatalf("want one bulk call with 2 targets, got %v", f.bulkCalls)
	}
	if r.Scopes != 2 {
		t.Fatalf("Scopes = %d, want 2", r.Scopes)
	}
}

func TestCopyAssetScopes_BulkUnresolvedCountedAndWarned(t *testing.T) {
	// One target unresolved: the bulk call still applies the rest in one shot
	// (no per-target clobbering), and only the resolved count is recorded.
	f := &bulkFake{skipped: []vault.SkippedTarget{{Target: vault.InstallTarget{Kind: vault.InstallKindUser, User: "ghost@x.com"}, Reason: "user not found"}}}
	r := &Report{}
	scopes := []manifest.Scope{
		{Kind: manifest.ScopeKindRepo, Repo: "github.com/acme/a"},
		{Kind: manifest.ScopeKindUser, User: "ghost@x.com"},
	}
	copyAssetScopes(context.Background(), f, "skill", scopes, true, false, r)

	if len(f.bulkCalls) != 1 {
		t.Fatalf("want one bulk call, got %v", f.bulkCalls)
	}
	if r.Scopes != 1 {
		t.Fatalf("Scopes = %d, want 1 (resolved only)", r.Scopes)
	}
	if len(r.Warnings) != 1 {
		t.Fatalf("want one skipped-scope warning, got %v", r.Warnings)
	}
	if f.clearCalls != 0 {
		t.Fatalf("partial success must not clear, got %d clears", f.clearCalls)
	}
}

func TestCopyAssetScopes_BulkAllUnresolvedClears(t *testing.T) {
	// Every target unresolvable → nothing applied → the asset must not keep the
	// auto-applied org-wide install; the engine clears it.
	f := &bulkFake{skipped: []vault.SkippedTarget{{Target: vault.InstallTarget{Kind: vault.InstallKindUser, User: "ghost@x.com"}, Reason: "user not found"}}}
	r := &Report{}
	scopes := []manifest.Scope{{Kind: manifest.ScopeKindUser, User: "ghost@x.com"}}
	copyAssetScopes(context.Background(), f, "skill", scopes, true, false, r)

	if r.Scopes != 0 {
		t.Fatalf("Scopes = %d, want 0", r.Scopes)
	}
	if f.clearCalls != 1 {
		t.Fatalf("all-unresolved must clear the auto-applied install once, got %d", f.clearCalls)
	}
}

func TestCopyAssetScopes_BulkErrorClearsNoFallback(t *testing.T) {
	f := &bulkFake{bulkErr: errors.New("repo rejected by server")}
	r := &Report{}
	scopes := []manifest.Scope{
		{Kind: manifest.ScopeKindRepo, Repo: "github.com/acme/a"},
		{Kind: manifest.ScopeKindTeam, Team: "platform"},
	}
	copyAssetScopes(context.Background(), f, "skill", scopes, true, false, r)

	if r.Scopes != 0 {
		t.Fatalf("Scopes = %d, want 0 on bulk error (no clobbering fallback)", r.Scopes)
	}
	if f.clearCalls != 1 {
		t.Fatalf("bulk error must clear the auto-applied install, got %d clears", f.clearCalls)
	}
}

func TestCopyAssetScopes_AppendDestUsesPerTarget(t *testing.T) {
	f := &appendFake{}
	r := &Report{}
	scopes := []manifest.Scope{
		{Kind: manifest.ScopeKindRepo, Repo: "github.com/acme/a"},
		{Kind: manifest.ScopeKindTeam, Team: "platform"},
	}
	copyAssetScopes(context.Background(), f, "skill", scopes, true, false, r)

	if len(f.singleCalls) != 2 || r.Scopes != 2 {
		t.Fatalf("append dest should set each target, got calls=%v scopes=%d", f.singleCalls, r.Scopes)
	}
}

func TestCopyAssetScopes_OrgWideWhenPresentNoScopes(t *testing.T) {
	f := &appendFake{}
	r := &Report{}
	copyAssetScopes(context.Background(), f, "skill", nil, true, false, r)

	if len(f.singleCalls) != 1 || f.singleCalls[0].Kind != vault.InstallKindOrg {
		t.Fatalf("present-with-no-scopes should set org-wide, got %v", f.singleCalls)
	}
}

func TestCopyAssetScopes_NotPresentClearsInstalls(t *testing.T) {
	// A source asset with no installation should end up uninstalled on the
	// destination — the engine clears any auto-applied install (e.g. the
	// org-wide default skills.new applies on upload) and sets no scopes.
	f := &bulkFake{}
	r := &Report{}
	copyAssetScopes(context.Background(), f, "skill", nil, false, false, r)

	if len(f.bulkCalls) != 0 || r.Scopes != 0 {
		t.Fatalf("not-present asset should set no scopes, got bulk=%v scopes=%d", f.bulkCalls, r.Scopes)
	}
	if f.clearCalls != 1 {
		t.Fatalf("not-present asset should clear installs once, got %d", f.clearCalls)
	}
}

func TestCopyAssetScopes_NotPresentDryRunNoClear(t *testing.T) {
	f := &bulkFake{}
	r := &Report{}
	copyAssetScopes(context.Background(), f, "skill", nil, false, true, r)

	if f.clearCalls != 0 {
		t.Fatalf("dry-run must not clear, got %d", f.clearCalls)
	}
}
