package vault

import (
	"context"
	"fmt"
	"maps"
	"path"
	"path/filepath"
	"slices"

	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/manifest"
	"github.com/sleuth-io/sx/internal/mgmt"
	"github.com/sleuth-io/sx/internal/vault/layout"
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
	// Preserve Clients from any existing entry at the *same name+version*
	// when the incoming asset has none. Re-add paths (handleIdenticalAsset,
	// configureRuleScopes, configureExistingAsset) construct lockfile.Asset
	// without metadata in scope; without this guard, upsert would wipe an
	// author-declared client filter on every re-configure of an existing
	// version. We deliberately scope inheritance to the same version so a
	// new version that intentionally drops `clients` does not silently
	// inherit the prior version's restriction.
	if len(asset.Clients) == 0 {
		for i := range m.Assets {
			if m.Assets[i].Name == asset.Name && m.Assets[i].Version == asset.Version && len(m.Assets[i].Clients) > 0 {
				asset.Clients = append([]string(nil), m.Assets[i].Clients...)
				break
			}
		}
	}
	m.UpsertAsset(lockfileAssetToManifest(*asset))
	return manifest.Save(vaultRoot, m)
}

// upsertAssetInheritingScopes inserts or replaces an asset in the vault's
// manifest, carrying any existing entry's scope rows through verbatim —
// every kind included. Converting scopes through the lockfile.Scope shape
// here would silently drop team/user/bot rows (that shape can only express
// repo/path) and thereby GLOBALIZE a team- or user-scoped asset on every
// republish. Used when publishing a new version with no scope change
// requested: the asset's sharing must survive the version bump. The RBAC
// edit gate (docs/rbac.md) has already vetted that the caller may
// republish the asset.
func upsertAssetInheritingScopes(vaultRoot string, asset *lockfile.Asset) error {
	m, err := loadManifest(vaultRoot)
	if err != nil {
		return err
	}
	// Gather scopes from EVERY same-name row, not just FindAsset's first
	// match: the rest of the system treats an asset's effective scopes as
	// the union across legacy duplicate rows (see commonCurrentInstallTargets),
	// and the upsert below prunes those rows — seeding from the first row
	// alone would silently drop any scope that lives only on a later one.
	var preserved []manifest.Scope
	inherit := false
	seen := map[string]bool{}
	for i := range m.Assets {
		if m.Assets[i].Name != asset.Name {
			continue
		}
		inherit = true
		for _, s := range m.Assets[i].Scopes {
			if k := scopeDedupKey(s); !seen[k] {
				seen[k] = true
				preserved = append(preserved, s)
			}
		}
	}
	ma := lockfileAssetToManifest(*asset)
	// Preserve Clients from any existing same name+version entry when the
	// incoming asset declares none (mirrors upsertAssetInManifest).
	if len(ma.Clients) == 0 {
		for i := range m.Assets {
			if m.Assets[i].Name == ma.Name && m.Assets[i].Version == ma.Version && len(m.Assets[i].Clients) > 0 {
				ma.Clients = append([]string(nil), m.Assets[i].Clients...)
				break
			}
		}
	}
	row := m.UpsertAsset(ma)
	if inherit {
		row.Scopes = preserved
	}
	return manifest.Save(vaultRoot, m)
}

// removeAssetFromManifest deletes every entry for the named asset, or
// only rows matching version when non-empty. When the asset's last row is
// removed, its name is pruned from every collection (the opportunistic
// pruning manifest-spec.md describes).
func removeAssetFromManifest(vaultRoot, name, version string) (int, error) {
	m, err := loadManifest(vaultRoot)
	if err != nil {
		return 0, err
	}
	removed := m.RemoveAsset(name, version)
	if removed > 0 && m.FindAsset(name) == nil {
		for i := range m.Collections {
			m.Collections[i].Assets = slices.DeleteFunc(m.Collections[i].Assets, func(a string) bool {
				return a == name
			})
		}
	}
	return removed, manifest.Save(vaultRoot, m)
}

// renameAssetInManifest rewrites every entry with the old name. Source-path
// rows that pointed at the asset's own storage location are rewritten to the
// renamed location — the files moved on disk, so a stale path would break
// resolution for every consumer of this asset.
func renameAssetInManifest(vaultRoot string, l layout.Layout, oldName, newName string) error {
	m, err := loadManifest(vaultRoot)
	if err != nil {
		return err
	}
	for i := range m.Assets {
		if m.Assets[i].Name != oldName {
			continue
		}
		m.Assets[i].Name = newName
		sp := m.Assets[i].SourcePath
		if sp != nil && sourcePathsEqual(sp.Path, l.SourcePathRel(oldName, m.Assets[i].Version)) {
			sp.Path = l.SourcePathRel(newName, m.Assets[i].Version)
		}
	}
	// Collections reference assets by name; follow the rename so the asset
	// doesn't silently vanish from its collections.
	for i := range m.Collections {
		for j, assetName := range m.Collections[i].Assets {
			if assetName == oldName {
				m.Collections[i].Assets[j] = newName
			}
		}
	}
	return manifest.Save(vaultRoot, m)
}

// sourcePathsEqual compares two vault-relative source paths, tolerating the
// "./" prefix and separator differences found in historically written rows.
func sourcePathsEqual(a, b string) bool {
	clean := func(p string) string {
		return path.Clean(filepath.ToSlash(p))
	}
	return clean(a) == clean(b)
}

// manifestAssetScopes returns the complete authoring scopes (org/repo/path/
// team/user/bot) for an asset from the manifest — unlike ExistingAssetScopes,
// which flattens to repo/path only. The bool reports whether the asset has a
// manifest entry at all: a registered org-wide asset returns (nil/empty, true)
// while an asset present only as uploaded files returns (nil, false). The copy
// engine needs that distinction to register org-wide installs without
// registering files-only assets.
func manifestAssetScopes(vaultRoot, name string) ([]manifest.Scope, bool) {
	m, err := loadManifest(vaultRoot)
	if err != nil || m == nil {
		return nil, false
	}
	a := m.FindAsset(name)
	if a == nil {
		return nil, false
	}
	return a.Scopes, true
}

// AssetInstallScopes exposes the named asset's complete authoring scopes and
// whether it has a manifest entry. The ctx/error signature matches the
// server-backed implementation so the copy engine can read scopes uniformly.
// See manifestAssetScopes.
func (p *PathVault) AssetInstallScopes(_ context.Context, name string) ([]manifest.Scope, bool, error) {
	scopes, present := manifestAssetScopes(p.repoPath, name)
	return scopes, present, nil
}

// AssetInstallScopes exposes the named asset's complete authoring scopes and
// whether it has a manifest entry. See manifestAssetScopes.
func (g *GitVault) AssetInstallScopes(_ context.Context, name string) ([]manifest.Scope, bool, error) {
	scopes, present := manifestAssetScopes(g.repoPath, name)
	return scopes, present, nil
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

// commonCurrentInstallTargets reads the named asset's current installation from
// the manifest as kind-aware targets — repo/path AND identity (team/user/bot).
// It is the file-backed counterpart to SleuthVault.CurrentInstallTargets and
// lets git/path vaults satisfy the same currentInstallReader interface, so the
// `sx add` scope editor renders every scope kind through ONE code path instead
// of falling back to the repo/path-only view (which silently showed a
// user-scoped asset as "global"). The bool reports whether the asset is in the
// manifest at all; present with no targets means a global (org-wide) install.
func commonCurrentInstallTargets(vaultRoot, name string) ([]InstallTarget, bool, error) {
	m, err := loadManifest(vaultRoot)
	if err != nil {
		return nil, false, err
	}
	present := false
	var targets []InstallTarget
	seen := map[string]bool{}
	for _, a := range m.Assets {
		if a.Name != name {
			continue
		}
		present = true
		// Scopes inherit across same-name version rows, so aggregate and dedup
		// rather than trusting the first row alone.
		for _, s := range a.Scopes {
			t, ok := manifestScopeToTarget(s)
			if !ok {
				continue
			}
			key := fmt.Sprintf("%s|%s|%v|%s|%s|%s", t.Kind, t.Repo, t.Paths, t.Team, t.User, t.Bot)
			if seen[key] {
				continue
			}
			seen[key] = true
			targets = append(targets, t)
		}
	}
	if !present {
		return nil, false, nil
	}
	return targets, true, nil
}

// manifestScopeToTarget converts a stored manifest scope row into a kind-aware
// install target. Org scopes never appear as rows (org-wide is the empty scope
// set), so an unexpected kind is reported as not-convertible.
func manifestScopeToTarget(s manifest.Scope) (InstallTarget, bool) {
	switch s.Kind {
	case manifest.ScopeKindRepo:
		return InstallTarget{Kind: InstallKindRepo, Repo: s.Repo}, true
	case manifest.ScopeKindPath:
		return InstallTarget{Kind: InstallKindPath, Repo: s.Repo, Paths: append([]string(nil), s.Paths...)}, true
	case manifest.ScopeKindTeam:
		return InstallTarget{Kind: InstallKindTeam, Team: s.Team}, true
	case manifest.ScopeKindUser:
		return InstallTarget{Kind: InstallKindUser, User: s.User}, true
	case manifest.ScopeKindBot:
		return InstallTarget{Kind: InstallKindBot, Bot: s.Bot}, true
	case manifest.ScopeKindOrg:
		// org-wide is the empty scope set, never a stored row
		return InstallTarget{}, false
	default:
		return InstallTarget{}, false
	}
}

// CurrentInstallTargets reports the named asset's current installation from the
// path vault's manifest; see commonCurrentInstallTargets.
func (p *PathVault) CurrentInstallTargets(ctx context.Context, name string) ([]InstallTarget, bool, error) {
	return commonCurrentInstallTargets(p.repoPath, name)
}

// CurrentInstallTargets reports the named asset's current installation from the
// git vault's manifest; see commonCurrentInstallTargets.
func (g *GitVault) CurrentInstallTargets(ctx context.Context, name string) ([]InstallTarget, bool, error) {
	return commonCurrentInstallTargets(g.repoPath, name)
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
