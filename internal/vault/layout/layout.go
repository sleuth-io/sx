// Package layout is the single source of truth for where asset files live
// inside a file-based vault (git or path), across storage format versions.
//
// Format v1 stores every version under the asset directory:
//
//	assets/{name}/list.txt
//	assets/{name}/{version}/...
//
// Format v2 materializes the latest version directly at the asset directory
// so the vault is usable in place (point an editor, Obsidian, or
// .claude/skills at assets/{name}), and keeps the immutable version archive
// under .sx alongside the audit and usage logs:
//
//	assets/{name}/...                     (latest version, materialized copy)
//	.sx/versions/{name}/list.txt
//	.sx/versions/{name}/{version}/...    (immutable archive)
//
// The manifest's schema_version selects the layout: 1 → v1, 2 → v2. All
// methods return vault-root-relative paths; callers join them with the root.
package layout

import (
	"fmt"
	"path/filepath"

	"github.com/sleuth-io/sx/internal/manifest"
)

// Version identifies a vault storage format.
type Version int

const (
	// V1 is the legacy layout: all versions under assets/{name}/.
	V1 Version = 1

	// V2 is the latest-at-root layout: materialized latest under
	// assets/{name}/, archive under .sx/versions/{name}/.
	V2 Version = 2
)

// versionListFile is the per-asset version listing filename.
const versionListFile = "list.txt"

// archiveRoot is the vault-relative directory holding the v2 version archive.
var archiveRoot = filepath.Join(".sx", "versions")

// Layout computes storage paths for one vault format version. The zero value
// is not valid; obtain one via ForVersion or Detect.
type Layout struct {
	version Version
}

// ForVersion returns the layout for a storage format version.
func ForVersion(v Version) (Layout, error) {
	switch v {
	case V1, V2:
		return Layout{version: v}, nil
	default:
		return Layout{}, fmt.Errorf("unsupported vault storage format version: %d", v)
	}
}

// Version returns the storage format version this layout computes paths for.
func (l Layout) Version() Version {
	return l.version
}

// AssetsRoot is the vault-relative directory containing asset directories.
func (l Layout) AssetsRoot() string {
	return "assets"
}

// AssetDir is the vault-relative directory for an asset. In v1 it contains
// version directories and list.txt; in v2 it contains the materialized
// latest version's files.
func (l Layout) AssetDir(name string) string {
	return filepath.Join("assets", name)
}

// VersionsDir is the vault-relative directory holding an asset's version
// directories and its list.txt.
func (l Layout) VersionsDir(name string) string {
	if l.version == V2 {
		return filepath.Join(archiveRoot, name)
	}
	return filepath.Join("assets", name)
}

// VersionDir is the vault-relative directory for one stored version of an
// asset. In v2 this is the immutable archive copy.
func (l Layout) VersionDir(name, version string) string {
	return filepath.Join(l.VersionsDir(name), version)
}

// VersionListPath is the vault-relative path of an asset's list.txt.
func (l Layout) VersionListPath(name string) string {
	return filepath.Join(l.VersionsDir(name), versionListFile)
}

// MetadataPath is the vault-relative path of a stored version's metadata.toml.
func (l Layout) MetadataPath(name, version string) string {
	return filepath.Join(l.VersionDir(name, version), "metadata.toml")
}

// SourcePathRel is the slash-separated vault-relative path recorded in
// manifest and lock source-path entries for a stored version. It always
// points at the immutable version directory, never the mutable v2 root view,
// so pinned resolution and caching stay stable across publishes.
func (l Layout) SourcePathRel(name, version string) string {
	return filepath.ToSlash(l.VersionDir(name, version))
}

// Detect determines the storage format of the vault rooted at vaultRoot.
//
// The manifest's schema_version is authoritative when sx.toml exists. Vaults
// without a manifest are classified by directory shape — see
// manifest.DetectShapeVersion.
func Detect(vaultRoot string) (Layout, error) {
	m, ok, err := manifest.Load(vaultRoot)
	if err != nil {
		return Layout{}, err
	}
	if ok {
		return ForVersion(Version(m.SchemaVersion))
	}
	return ForVersion(Version(manifest.DetectShapeVersion(vaultRoot)))
}
