package vaultcopy

import (
	"context"
	"errors"
	"testing"

	"github.com/sleuth-io/sx/internal/manifest"
	"github.com/sleuth-io/sx/internal/vault"
)

// fakeInstaller records scope-set calls and implements both the per-target and
// bulk installer interfaces, optionally failing the bulk call.
type fakeInstaller struct {
	bulkCalls   [][]vault.InstallTarget
	singleCalls []vault.InstallTarget
	bulkErr     error
}

func (f *fakeInstaller) SetAssetInstallation(_ context.Context, _ string, t vault.InstallTarget) error {
	f.singleCalls = append(f.singleCalls, t)
	return nil
}

func (f *fakeInstaller) SetAssetInstallations(_ context.Context, _ string, targets []vault.InstallTarget) error {
	f.bulkCalls = append(f.bulkCalls, targets)
	return f.bulkErr
}

func TestCopyAssetScopes_MultiScopeUsesBulk(t *testing.T) {
	f := &fakeInstaller{}
	r := &Report{}
	scopes := []manifest.Scope{
		{Kind: manifest.ScopeKindRepo, Repo: "github.com/acme/a"},
		{Kind: manifest.ScopeKindTeam, Team: "platform"},
	}
	copyAssetScopes(context.Background(), f, "skill", scopes, true, false, r)

	if len(f.bulkCalls) != 1 || len(f.bulkCalls[0]) != 2 {
		t.Fatalf("want one bulk call with 2 targets, got bulk=%v single=%v", f.bulkCalls, f.singleCalls)
	}
	if len(f.singleCalls) != 0 {
		t.Fatalf("want no per-target calls when bulk succeeds, got %v", f.singleCalls)
	}
	if r.Scopes != 2 {
		t.Fatalf("Scopes = %d, want 2", r.Scopes)
	}
}

func TestCopyAssetScopes_BulkFailureFallsBack(t *testing.T) {
	f := &fakeInstaller{bulkErr: errors.New("repo not found")}
	r := &Report{}
	scopes := []manifest.Scope{
		{Kind: manifest.ScopeKindRepo, Repo: "github.com/acme/a"},
		{Kind: manifest.ScopeKindBot, Bot: "ci"},
	}
	copyAssetScopes(context.Background(), f, "skill", scopes, true, false, r)

	if len(f.bulkCalls) != 1 {
		t.Fatalf("want one bulk attempt, got %v", f.bulkCalls)
	}
	if len(f.singleCalls) != 2 {
		t.Fatalf("want per-target fallback (2 calls), got %v", f.singleCalls)
	}
	if r.Scopes != 2 {
		t.Fatalf("Scopes = %d, want 2", r.Scopes)
	}
}

func TestCopyAssetScopes_SingleScopeSkipsBulk(t *testing.T) {
	f := &fakeInstaller{}
	r := &Report{}
	copyAssetScopes(context.Background(), f, "skill",
		[]manifest.Scope{{Kind: manifest.ScopeKindRepo, Repo: "github.com/acme/a"}}, true, false, r)

	if len(f.bulkCalls) != 0 || len(f.singleCalls) != 1 {
		t.Fatalf("single scope should use per-target, got bulk=%v single=%v", f.bulkCalls, f.singleCalls)
	}
}

func TestCopyAssetScopes_OrgWideWhenPresentNoScopes(t *testing.T) {
	f := &fakeInstaller{}
	r := &Report{}
	copyAssetScopes(context.Background(), f, "skill", nil, true, false, r)

	if len(f.singleCalls) != 1 || f.singleCalls[0].Kind != vault.InstallKindOrg {
		t.Fatalf("present-with-no-scopes should set org-wide, got %v", f.singleCalls)
	}
}

func TestCopyAssetScopes_NotPresentDoesNothing(t *testing.T) {
	f := &fakeInstaller{}
	r := &Report{}
	copyAssetScopes(context.Background(), f, "skill", nil, false, false, r)

	if len(f.singleCalls) != 0 || len(f.bulkCalls) != 0 || r.Scopes != 0 {
		t.Fatalf("not-present asset should set nothing, got bulk=%v single=%v scopes=%d", f.bulkCalls, f.singleCalls, r.Scopes)
	}
}
