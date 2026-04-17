package manifest

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"

	"github.com/sleuth-io/sx/internal/buildinfo"
	"github.com/sleuth-io/sx/internal/lockfile"
)

// legacyLockFileName is the pre-manifest filename that older sx builds
// wrote at the vault root. LoadOrMigrate synthesizes an sx.toml from it
// on the first read and renames it with a .migrated suffix so the
// original is preserved on disk for rollback.
const legacyLockFileName = "sx.lock"

// LoadOrMigrate returns the manifest at vaultRoot/sx.toml, creating it
// from a legacy sx.lock if sx.toml does not yet exist. The returned
// bool is true when a migration ran on this call.
//
// After a successful migration, the legacy sx.lock is renamed to
// sx.lock.migrated so it's preserved on disk but no longer read. If no
// legacy file exists either, an empty manifest is written and returned.
func LoadOrMigrate(vaultRoot string) (*Manifest, bool, error) {
	if m, ok, err := Load(vaultRoot); err != nil {
		return nil, false, err
	} else if ok {
		return m, false, nil
	}

	m, migrated, err := buildFromLegacy(vaultRoot)
	if err != nil {
		return nil, false, err
	}

	m.CreatedBy = buildinfo.GetCreatedBy()
	if err := Save(vaultRoot, m); err != nil {
		return nil, false, err
	}

	if migrated {
		if err := archiveLegacyLockFile(vaultRoot); err != nil {
			return nil, false, err
		}
	}
	return m, migrated, nil
}

// buildFromLegacy synthesizes a Manifest by reading an on-disk sx.lock if
// present. Returns the new manifest and a flag set when the lock file
// contributed data.
func buildFromLegacy(vaultRoot string) (*Manifest, bool, error) {
	m := &Manifest{SchemaVersion: CurrentSchemaVersion}

	lockPath := filepath.Join(vaultRoot, legacyLockFileName)
	lf, lfExists, err := readLockFileIfExists(lockPath)
	if err != nil {
		return nil, false, fmt.Errorf("read legacy lock file: %w", err)
	}
	if !lfExists {
		return m, false, nil
	}
	m.Assets = convertLockAssets(lf.Assets)
	return m, true, nil
}

func readLockFileIfExists(path string) (*lockfile.LockFile, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}
	lf, err := lockfile.Parse(data)
	if err != nil {
		return nil, false, err
	}
	return lf, true, nil
}

// convertLockAssets maps lockfile.Asset entries (with their repo/path
// scopes) into the manifest's unified Asset representation. An empty
// scope list on the source asset yields an empty scope list on the
// target, which means "org-wide" in both formats.
func convertLockAssets(in []lockfile.Asset) []Asset {
	if len(in) == 0 {
		return nil
	}
	out := make([]Asset, 0, len(in))
	for _, src := range in {
		dst := Asset{
			Name:         src.Name,
			Version:      src.Version,
			Type:         src.Type,
			Clients:      append([]string(nil), src.Clients...),
			Dependencies: convertDependencies(src.Dependencies),
			SourceHTTP:   convertSourceHTTP(src.SourceHTTP),
			SourcePath:   convertSourcePath(src.SourcePath),
			SourceGit:    convertSourceGit(src.SourceGit),
			Scopes:       convertLockScopes(src.Scopes),
		}
		out = append(out, dst)
	}
	return out
}

func convertDependencies(in []lockfile.Dependency) []Dependency {
	if len(in) == 0 {
		return nil
	}
	out := make([]Dependency, len(in))
	for i, d := range in {
		out[i] = Dependency{Name: d.Name, Version: d.Version}
	}
	return out
}

func convertSourceHTTP(in *lockfile.SourceHTTP) *SourceHTTP {
	if in == nil {
		return nil
	}
	return &SourceHTTP{
		URL:    in.URL,
		Hashes: copyStringMap(in.Hashes),
		Size:   in.Size,
	}
}

func convertSourcePath(in *lockfile.SourcePath) *SourcePath {
	if in == nil {
		return nil
	}
	return &SourcePath{Path: in.Path}
}

func convertSourceGit(in *lockfile.SourceGit) *SourceGit {
	if in == nil {
		return nil
	}
	return &SourceGit{
		URL:          in.URL,
		Ref:          in.Ref,
		Subdirectory: in.Subdirectory,
	}
}

// convertLockScopes maps lockfile.Scope entries onto manifest.Scope. A
// scope with no paths is repo-wide; one with paths is path-restricted.
func convertLockScopes(in []lockfile.Scope) []Scope {
	if len(in) == 0 {
		return nil
	}
	out := make([]Scope, 0, len(in))
	for _, s := range in {
		if len(s.Paths) == 0 {
			out = append(out, Scope{Kind: ScopeKindRepo, Repo: s.Repo})
			continue
		}
		out = append(out, Scope{
			Kind:  ScopeKindPath,
			Repo:  s.Repo,
			Paths: append([]string(nil), s.Paths...),
		})
	}
	return out
}

// archiveLegacyLockFile renames sx.lock to sx.lock.migrated. A missing
// file is a no-op; errors from a present file bubble up so callers can
// surface the failure and roll back.
func archiveLegacyLockFile(vaultRoot string) error {
	src := filepath.Join(vaultRoot, legacyLockFileName)
	if _, err := os.Stat(src); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("stat sx.lock: %w", err)
	}
	dst := src + ".migrated"
	if err := os.Rename(src, dst); err != nil {
		return fmt.Errorf("rename sx.lock: %w", err)
	}
	return nil
}

func copyStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	maps.Copy(out, in)
	return out
}
