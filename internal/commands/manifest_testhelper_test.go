package commands

import (
	"maps"
	"testing"

	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/manifest"
)

// findManifestAsset is a test helper that loads the vault's manifest at
// vaultRoot and returns the named asset in lockfile.Asset form. Team and
// user scopes are excluded — the helper is for tests that previously
// asserted on the unresolved sx.lock shape. Fails the test fatally on
// manifest load errors.
func findManifestAsset(t *testing.T, vaultRoot, name string) (*lockfile.Asset, bool) {
	t.Helper()
	m, ok, err := manifest.Load(vaultRoot)
	if err != nil {
		t.Fatalf("load manifest at %s: %v", vaultRoot, err)
	}
	if !ok || m == nil {
		return nil, false
	}
	a := m.FindAsset(name)
	if a == nil {
		return nil, false
	}

	out := &lockfile.Asset{
		Name:         a.Name,
		Version:      a.Version,
		Type:         a.Type,
		Clients:      append([]string(nil), a.Clients...),
		Dependencies: copyDependenciesManifestToLockfile(a.Dependencies),
	}
	if a.SourceHTTP != nil {
		out.SourceHTTP = &lockfile.SourceHTTP{URL: a.SourceHTTP.URL, Size: a.SourceHTTP.Size}
		if a.SourceHTTP.Hashes != nil {
			out.SourceHTTP.Hashes = make(map[string]string, len(a.SourceHTTP.Hashes))
			maps.Copy(out.SourceHTTP.Hashes, a.SourceHTTP.Hashes)
		}
	}
	if a.SourcePath != nil {
		out.SourcePath = &lockfile.SourcePath{Path: a.SourcePath.Path}
	}
	if a.SourceGit != nil {
		out.SourceGit = &lockfile.SourceGit{URL: a.SourceGit.URL, Ref: a.SourceGit.Ref, Subdirectory: a.SourceGit.Subdirectory}
	}
	for _, s := range a.Scopes {
		switch s.Kind {
		case manifest.ScopeKindRepo:
			out.Scopes = append(out.Scopes, lockfile.Scope{Repo: s.Repo})
		case manifest.ScopeKindPath:
			out.Scopes = append(out.Scopes, lockfile.Scope{Repo: s.Repo, Paths: append([]string(nil), s.Paths...)})
		case manifest.ScopeKindOrg, manifest.ScopeKindTeam, manifest.ScopeKindUser, manifest.ScopeKindBot:
			// Identity-dependent scopes are not expressible in the
			// lockfile.Scope shape; tests that need them read the
			// manifest directly.
		}
	}
	return out, true
}

// fakeLockFromManifest returns a lockfile.LockFile shape populated from
// the vault's manifest so test code written against the old lock parser
// can continue to operate on Assets/Scopes without rewriting.
func fakeLockFromManifest(t *testing.T, vaultRoot string) (*lockfile.LockFile, error) {
	t.Helper()
	m, ok, err := manifest.Load(vaultRoot)
	if err != nil {
		return nil, err
	}
	lf := &lockfile.LockFile{}
	if !ok || m == nil {
		return lf, nil
	}
	lf.Assets = append(lf.Assets, allManifestAssets(t, vaultRoot)...)
	return lf, nil
}

// allManifestAssets returns every asset in the vault's manifest in
// lockfile.Asset form. Used by tests that need to enumerate all versions
// of an asset.
func allManifestAssets(t *testing.T, vaultRoot string) []lockfile.Asset {
	t.Helper()
	m, ok, err := manifest.Load(vaultRoot)
	if err != nil {
		t.Fatalf("load manifest at %s: %v", vaultRoot, err)
	}
	if !ok || m == nil {
		return nil
	}
	out := make([]lockfile.Asset, 0, len(m.Assets))
	for _, a := range m.Assets {
		lf := lockfile.Asset{
			Name:         a.Name,
			Version:      a.Version,
			Type:         a.Type,
			Clients:      append([]string(nil), a.Clients...),
			Dependencies: copyDependenciesManifestToLockfile(a.Dependencies),
		}
		for _, s := range a.Scopes {
			switch s.Kind {
			case manifest.ScopeKindRepo:
				lf.Scopes = append(lf.Scopes, lockfile.Scope{Repo: s.Repo})
			case manifest.ScopeKindPath:
				lf.Scopes = append(lf.Scopes, lockfile.Scope{Repo: s.Repo, Paths: append([]string(nil), s.Paths...)})
			case manifest.ScopeKindOrg, manifest.ScopeKindTeam, manifest.ScopeKindUser, manifest.ScopeKindBot:
				// See findManifestAsset: identity-dependent scopes
				// are omitted from the lockfile shape.
			}
		}
		out = append(out, lf)
	}
	return out
}
