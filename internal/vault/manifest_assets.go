package vault

import (
	"context"
	"maps"

	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/manifest"
	"github.com/sleuth-io/sx/internal/mgmt"
)

// resolveLockBytesForActor loads the vault's manifest and returns the
// resolved lock file bytes for the caller identified by vaultRoot's git
// config. Every file-backed vault's GetLockFile delegates here.
//
// Returns ErrLockFileNotFound only when the vault has never been
// initialized (no manifest on disk). An initialized vault with zero
// assets returns a valid empty lock file so the install pipeline can
// still detect tracker deltas and clean up previously-installed assets.
func resolveLockBytesForActor(ctx context.Context, vaultRoot string) ([]byte, error) {
	m, err := loadManifest(vaultRoot)
	if err != nil {
		return nil, err
	}
	if m == nil {
		return nil, ErrLockFileNotFound
	}
	actor, err := mgmt.CurrentGitActor(ctx, vaultRoot)
	if err != nil {
		return nil, err
	}
	lf := manifest.Resolve(m, actor)
	return lockfile.Marshal(lf)
}

// upsertAssetInManifest inserts or replaces an asset in the vault's
// manifest. Scopes on the incoming asset are preserved verbatim —
// callers that want to inherit existing scopes should call
// inheritAssetScopesFromManifest before invoking this.
func upsertAssetInManifest(vaultRoot string, asset *lockfile.Asset) error {
	m, err := loadManifest(vaultRoot)
	if err != nil {
		return err
	}
	m.UpsertAsset(lockfileAssetToManifest(*asset))
	return manifest.Save(vaultRoot, m)
}

// inheritAssetScopesFromManifest copies the scope list from any existing
// entry for asset.Name into the incoming asset. No-op if the asset is
// not present. Produces lockfile scopes (the caller typically turns
// around and passes the asset back to upsert, which converts both the
// asset and its scopes into manifest form).
func inheritAssetScopesFromManifest(vaultRoot string, asset *lockfile.Asset) error {
	m, err := loadManifest(vaultRoot)
	if err != nil {
		return err
	}
	existing := m.FindAsset(asset.Name)
	if existing == nil {
		return nil
	}
	// Inherited scopes carry across kinds (repo, path, team, user). Convert
	// them through a trivial Resolve against the no-op actor so they land
	// in lockfile.Scope form. We intentionally skip team/user rows because
	// those are identity-dependent and the new owner may not be in the
	// same teams; keep only the structural (repo/path) scopes.
	asset.Scopes = nil
	for _, s := range existing.Scopes {
		switch s.Kind {
		case manifest.ScopeKindRepo:
			asset.Scopes = append(asset.Scopes, lockfile.Scope{Repo: s.Repo})
		case manifest.ScopeKindPath:
			asset.Scopes = append(asset.Scopes, lockfile.Scope{Repo: s.Repo, Paths: append([]string(nil), s.Paths...)})
		case manifest.ScopeKindOrg, manifest.ScopeKindTeam, manifest.ScopeKindUser, manifest.ScopeKindBot:
			// Identity-dependent scopes do not inherit — the new
			// owner may not be in the same teams or bot identities,
			// and a blanket carry-over would leak access.
		}
	}
	return nil
}

// removeAssetFromManifest deletes every entry for the named asset, or
// only rows matching version when non-empty.
func removeAssetFromManifest(vaultRoot, name, version string) error {
	m, err := loadManifest(vaultRoot)
	if err != nil {
		return err
	}
	m.RemoveAsset(name, version)
	return manifest.Save(vaultRoot, m)
}

// renameAssetInManifest rewrites every entry with the old name.
func renameAssetInManifest(vaultRoot, oldName, newName string) error {
	m, err := loadManifest(vaultRoot)
	if err != nil {
		return err
	}
	for i := range m.Assets {
		if m.Assets[i].Name == oldName {
			m.Assets[i].Name = newName
		}
	}
	return manifest.Save(vaultRoot, m)
}

// findAssetInManifest returns (asset, true) when the named asset exists
// in the vault. Used by callers that need to check before modifying.
func findAssetInManifest(vaultRoot, name string) (*lockfile.Asset, bool) {
	m, err := loadManifest(vaultRoot)
	if err != nil || m == nil {
		return nil, false
	}
	a := m.FindAsset(name)
	if a == nil {
		return nil, false
	}
	out := manifestAssetToLockfile(*a)
	return &out, true
}

// ExistingAssetScopes returns the manifest's current repo/path scopes for
// the named asset. Team and user scopes are excluded — the method exists
// to support UX prompts in `sx add` that show the publisher what repo-
// shaped scopes an asset already has. Returns nil if the asset is not in
// the manifest or the vault doesn't support manifest-backed lookup.
func (p *PathVault) ExistingAssetScopes(name string) []lockfile.Scope {
	if a, ok := findAssetInManifest(p.repoPath, name); ok {
		return a.Scopes
	}
	return nil
}

// ExistingAssetScopes is the GitVault version; see PathVault's doc.
func (g *GitVault) ExistingAssetScopes(name string) []lockfile.Scope {
	if a, ok := findAssetInManifest(g.repoPath, name); ok {
		return a.Scopes
	}
	return nil
}

// lockfileAssetToManifest converts a lockfile.Asset (how the install
// pipeline and CLI add/upsert calls shape an asset) into a manifest.Asset.
// The scope conversion mirrors convertLockScopes in migrate.go: a scope
// with no paths is repo-wide, with paths is path-restricted.
func lockfileAssetToManifest(a lockfile.Asset) manifest.Asset {
	dst := manifest.Asset{
		Name:         a.Name,
		Version:      a.Version,
		Type:         a.Type,
		Clients:      append([]string(nil), a.Clients...),
		Dependencies: convertDepsLockfileToManifest(a.Dependencies),
		SourceHTTP:   convertLockfileSourceHTTPToManifest(a.SourceHTTP),
		SourcePath:   convertLockfileSourcePathToManifest(a.SourcePath),
		SourceGit:    convertLockfileSourceGitToManifest(a.SourceGit),
	}
	if len(a.Scopes) > 0 {
		dst.Scopes = make([]manifest.Scope, 0, len(a.Scopes))
		for _, s := range a.Scopes {
			if len(s.Paths) == 0 {
				dst.Scopes = append(dst.Scopes, manifest.Scope{Kind: manifest.ScopeKindRepo, Repo: s.Repo})
				continue
			}
			dst.Scopes = append(dst.Scopes, manifest.Scope{
				Kind:  manifest.ScopeKindPath,
				Repo:  s.Repo,
				Paths: append([]string(nil), s.Paths...),
			})
		}
	}
	return dst
}

// manifestAssetToLockfile inverts lockfileAssetToManifest. Team/user
// scopes cannot be represented in the lockfile.Asset shape (scopes are
// flat repo/path tuples), so those scopes are dropped; callers that need
// resolved, identity-aware scopes should go through manifest.Resolve
// instead of this helper.
func manifestAssetToLockfile(a manifest.Asset) lockfile.Asset {
	dst := lockfile.Asset{
		Name:         a.Name,
		Version:      a.Version,
		Type:         a.Type,
		Clients:      append([]string(nil), a.Clients...),
		Dependencies: convertDepsManifestToLockfile(a.Dependencies),
		SourceHTTP:   convertManifestSourceHTTPToLockfile(a.SourceHTTP),
		SourcePath:   convertManifestSourcePathToLockfile(a.SourcePath),
		SourceGit:    convertManifestSourceGitToLockfile(a.SourceGit),
	}
	for _, s := range a.Scopes {
		switch s.Kind {
		case manifest.ScopeKindRepo:
			dst.Scopes = append(dst.Scopes, lockfile.Scope{Repo: s.Repo})
		case manifest.ScopeKindPath:
			dst.Scopes = append(dst.Scopes, lockfile.Scope{Repo: s.Repo, Paths: append([]string(nil), s.Paths...)})
		case manifest.ScopeKindOrg, manifest.ScopeKindTeam, manifest.ScopeKindUser, manifest.ScopeKindBot:
			// Identity-dependent scopes cannot be represented in the
			// lockfile.Scope shape. Callers that need resolved, per-
			// user (or per-bot) scopes go through manifest.Resolve.
		}
	}
	return dst
}

func convertDepsLockfileToManifest(in []lockfile.Dependency) []manifest.Dependency {
	if len(in) == 0 {
		return nil
	}
	out := make([]manifest.Dependency, len(in))
	for i, d := range in {
		out[i] = manifest.Dependency{Name: d.Name, Version: d.Version}
	}
	return out
}

func convertDepsManifestToLockfile(in []manifest.Dependency) []lockfile.Dependency {
	if len(in) == 0 {
		return nil
	}
	out := make([]lockfile.Dependency, len(in))
	for i, d := range in {
		out[i] = lockfile.Dependency{Name: d.Name, Version: d.Version}
	}
	return out
}

func convertLockfileSourceHTTPToManifest(in *lockfile.SourceHTTP) *manifest.SourceHTTP {
	if in == nil {
		return nil
	}
	out := &manifest.SourceHTTP{URL: in.URL, Size: in.Size}
	if in.Hashes != nil {
		out.Hashes = make(map[string]string, len(in.Hashes))
		maps.Copy(out.Hashes, in.Hashes)
	}
	return out
}

func convertManifestSourceHTTPToLockfile(in *manifest.SourceHTTP) *lockfile.SourceHTTP {
	if in == nil {
		return nil
	}
	out := &lockfile.SourceHTTP{URL: in.URL, Size: in.Size}
	if in.Hashes != nil {
		out.Hashes = make(map[string]string, len(in.Hashes))
		maps.Copy(out.Hashes, in.Hashes)
	}
	return out
}

func convertLockfileSourcePathToManifest(in *lockfile.SourcePath) *manifest.SourcePath {
	if in == nil {
		return nil
	}
	return &manifest.SourcePath{Path: in.Path}
}

func convertManifestSourcePathToLockfile(in *manifest.SourcePath) *lockfile.SourcePath {
	if in == nil {
		return nil
	}
	return &lockfile.SourcePath{Path: in.Path}
}

func convertLockfileSourceGitToManifest(in *lockfile.SourceGit) *manifest.SourceGit {
	if in == nil {
		return nil
	}
	return &manifest.SourceGit{URL: in.URL, Ref: in.Ref, Subdirectory: in.Subdirectory}
}

func convertManifestSourceGitToLockfile(in *manifest.SourceGit) *lockfile.SourceGit {
	if in == nil {
		return nil
	}
	return &lockfile.SourceGit{URL: in.URL, Ref: in.Ref, Subdirectory: in.Subdirectory}
}
