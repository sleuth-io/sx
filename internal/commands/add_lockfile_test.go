package commands

import (
	"context"
	"testing"

	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/vault"
)

// spyVault records SetInstallations / InheritInstallations calls so unit
// tests can assert on the (asset, scopeEntity) pair that writeLockFileForNoInstall
// threads through. All other Vault methods are unimplemented — the embedded
// nil-interface deliberately panics if anything else is called.
type spyVault struct {
	vault.Vault
	setCalls       []spySetCall
	bulkCalls      []spyBulkCall
	inheritCalls   []*lockfile.Asset
	setShouldError error
}

type spySetCall struct {
	asset       *lockfile.Asset
	scopeEntity string
}

type spyBulkCall struct {
	assetName  string
	targets    []vault.InstallTarget
	appendMode bool
}

// SetAssetInstallations satisfies installSetter so the unified --no-install
// path (--repo/--team/...) can be exercised; it records the targets the helper
// resolved instead of touching a real vault.
func (s *spyVault) SetAssetInstallations(_ context.Context, assetName string, targets []vault.InstallTarget, appendMode bool) ([]vault.SkippedTarget, error) {
	if s.setShouldError != nil {
		return nil, s.setShouldError
	}
	s.bulkCalls = append(s.bulkCalls, spyBulkCall{assetName: assetName, targets: targets, appendMode: appendMode})
	return nil, nil
}

func (s *spyVault) SetInstallations(_ context.Context, asset *lockfile.Asset, scopeEntity string) error {
	if s.setShouldError != nil {
		return s.setShouldError
	}
	s.setCalls = append(s.setCalls, spySetCall{asset: asset, scopeEntity: scopeEntity})
	return nil
}

func (s *spyVault) InheritInstallations(_ context.Context, asset *lockfile.Asset) error {
	s.inheritCalls = append(s.inheritCalls, asset)
	return nil
}

// TestWriteLockFileForNoInstall covers the regression-prone code paths in
// writeLockFileForNoInstall that cannot be reached through the integration
// test framework (the version prompt blocks plain --no-install on stdin),
// and that the path vault flattens out (path vaults ignore scopeEntity, so
// integration assertions can't distinguish "forwarded" from "silently
// dropped").
func TestWriteLockFileForNoInstall(t *testing.T) {
	t.Run("forwards --scope=<entity> to SetInstallations", func(t *testing.T) {
		// Pre-fix this branch was a hardcoded "" passed to updateLockFile;
		// --no-install --scope=personal silently dropped the entity.
		spy := &spyVault{}
		out := &outputHelper{}
		asset := &lockfile.Asset{Name: "my-skill", Version: "1.0.0"}
		opts := addOptions{NoInstall: true, Scope: "personal"}

		if err := writeLockFileForNoInstall(context.Background(), out, spy, asset, opts); err != nil {
			t.Fatalf("writeLockFileForNoInstall: %v", err)
		}

		if len(spy.setCalls) != 1 {
			t.Fatalf("SetInstallations calls = %d, want 1", len(spy.setCalls))
		}
		if spy.setCalls[0].scopeEntity != "personal" {
			t.Errorf("scopeEntity = %q, want %q (entity dropped)", spy.setCalls[0].scopeEntity, "personal")
		}
		// The empty Scopes payload is correct alongside ScopeEntity —
		// vault entity routes the install, not the Scopes slice.
		if len(spy.setCalls[0].asset.Scopes) != 0 {
			t.Errorf("Scopes = %v, want empty slice (entity routes via scopeEntity)", spy.setCalls[0].asset.Scopes)
		}
	})

	t.Run("plain --no-install (Remove branch) falls back to global", func(t *testing.T) {
		// --no-install with no --yes and no scope flag puts getScopes()
		// in the Remove branch, which the helper deliberately treats as
		// global. Locks in the explicit fallback so a future change to
		// Remove semantics doesn't silently regress this path.
		spy := &spyVault{}
		out := &outputHelper{}
		asset := &lockfile.Asset{Name: "my-skill", Version: "1.0.0"}
		opts := addOptions{NoInstall: true}

		if err := writeLockFileForNoInstall(context.Background(), out, spy, asset, opts); err != nil {
			t.Fatalf("writeLockFileForNoInstall: %v", err)
		}

		if len(spy.setCalls) != 1 {
			t.Fatalf("SetInstallations calls = %d, want 1", len(spy.setCalls))
		}
		if spy.setCalls[0].scopeEntity != "" {
			t.Errorf("scopeEntity = %q, want \"\" (no entity flag)", spy.setCalls[0].scopeEntity)
		}
		// Global is represented by an empty (but non-nil) Scopes slice.
		if spy.setCalls[0].asset.Scopes == nil {
			t.Errorf("Scopes = nil, want empty slice (Remove fallback should produce global)")
		}
		if len(spy.setCalls[0].asset.Scopes) != 0 {
			t.Errorf("Scopes = %v, want empty slice (global)", spy.setCalls[0].asset.Scopes)
		}
	})

	t.Run("--yes --no-install (Inherit branch) falls back to global", func(t *testing.T) {
		// Same fallback as Remove, different getScopes branch.
		spy := &spyVault{}
		out := &outputHelper{}
		asset := &lockfile.Asset{Name: "my-skill", Version: "1.0.0"}
		opts := addOptions{NoInstall: true, Yes: true}

		if err := writeLockFileForNoInstall(context.Background(), out, spy, asset, opts); err != nil {
			t.Fatalf("writeLockFileForNoInstall: %v", err)
		}

		if len(spy.setCalls) != 1 {
			t.Fatalf("SetInstallations calls = %d, want 1", len(spy.setCalls))
		}
		if spy.setCalls[0].asset.Scopes == nil || len(spy.setCalls[0].asset.Scopes) != 0 {
			t.Errorf("Scopes = %v, want empty slice (global)", spy.setCalls[0].asset.Scopes)
		}
	})

	t.Run("--no-install --team routes through the bulk installer, not global", func(t *testing.T) {
		// Regression: getScopes never inspected the unified flags, so
		// --no-install --team foo fell through to the global fallback and
		// silently dropped the team. It must now persist the team target.
		spy := &spyVault{}
		out := &outputHelper{}
		asset := &lockfile.Asset{Name: "my-skill", Version: "1.0.0"}
		opts := addOptions{NoInstall: true, Teams: []string{"foo"}}

		if err := writeLockFileForNoInstall(context.Background(), out, spy, asset, opts); err != nil {
			t.Fatalf("writeLockFileForNoInstall: %v", err)
		}

		if len(spy.setCalls) != 0 {
			t.Fatalf("SetInstallations calls = %d, want 0 (unified flags use the bulk path)", len(spy.setCalls))
		}
		if len(spy.bulkCalls) != 1 {
			t.Fatalf("SetAssetInstallations calls = %d, want 1", len(spy.bulkCalls))
		}
		got := spy.bulkCalls[0].targets
		if len(got) != 1 || got[0].Kind != vault.InstallKindTeam || got[0].Team != "foo" {
			t.Errorf("targets = %v, want one team:foo target", got)
		}
		if spy.bulkCalls[0].appendMode {
			t.Errorf("appendMode = true, want false (replace without --add-to-scope)")
		}
	})

	t.Run("--no-install --repo routes through the bulk installer", func(t *testing.T) {
		spy := &spyVault{}
		out := &outputHelper{}
		asset := &lockfile.Asset{Name: "my-skill", Version: "1.0.0"}
		opts := addOptions{NoInstall: true, Repos: []string{"git@github.com:org/repo.git"}}

		if err := writeLockFileForNoInstall(context.Background(), out, spy, asset, opts); err != nil {
			t.Fatalf("writeLockFileForNoInstall: %v", err)
		}

		if len(spy.bulkCalls) != 1 {
			t.Fatalf("SetAssetInstallations calls = %d, want 1", len(spy.bulkCalls))
		}
		got := spy.bulkCalls[0].targets
		if len(got) != 1 || got[0].Kind != vault.InstallKindRepo || got[0].Repo != "git@github.com:org/repo.git" {
			t.Errorf("targets = %v, want one repo target", got)
		}
	})

	t.Run("--no-install --add-to-scope --team uses append mode", func(t *testing.T) {
		spy := &spyVault{}
		out := &outputHelper{}
		asset := &lockfile.Asset{Name: "my-skill", Version: "1.0.0"}
		opts := addOptions{NoInstall: true, Teams: []string{"foo"}, AddToScope: true}

		if err := writeLockFileForNoInstall(context.Background(), out, spy, asset, opts); err != nil {
			t.Fatalf("writeLockFileForNoInstall: %v", err)
		}

		if len(spy.bulkCalls) != 1 {
			t.Fatalf("SetAssetInstallations calls = %d, want 1", len(spy.bulkCalls))
		}
		if !spy.bulkCalls[0].appendMode {
			t.Errorf("appendMode = false, want true (--add-to-scope)")
		}
	})

	t.Run("--no-install --scope-repo writes the repo scope", func(t *testing.T) {
		spy := &spyVault{}
		out := &outputHelper{}
		asset := &lockfile.Asset{Name: "my-skill", Version: "1.0.0"}
		opts := addOptions{NoInstall: true, ScopeRepos: []string{"git@github.com:org/repo.git"}}

		if err := writeLockFileForNoInstall(context.Background(), out, spy, asset, opts); err != nil {
			t.Fatalf("writeLockFileForNoInstall: %v", err)
		}

		if len(spy.setCalls) != 1 {
			t.Fatalf("SetInstallations calls = %d, want 1", len(spy.setCalls))
		}
		got := spy.setCalls[0].asset.Scopes
		if len(got) != 1 {
			t.Fatalf("Scopes = %v, want 1 scope", got)
		}
		if got[0].Repo != "git@github.com:org/repo.git" {
			t.Errorf("Scopes[0].Repo = %q, want %q", got[0].Repo, "git@github.com:org/repo.git")
		}
	})
}
