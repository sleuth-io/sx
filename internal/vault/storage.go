package vault

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/sleuth-io/sx/internal/metadata"
	"github.com/sleuth-io/sx/internal/utils"
	"github.com/sleuth-io/sx/internal/vault/layout"
	"github.com/sleuth-io/sx/internal/version"
)

// This file holds the layout-aware storage operations shared by the
// file-backed vaults (GitVault, PathVault). All functions operate on the
// layout detected from the vault's manifest, so both backends behave
// identically for v1 and v2 storage formats. See internal/vault/layout for
// the two formats.

// detectLayout resolves the storage layout for a vault root, wrapping
// detection errors with context. A future-schema manifest surfaces
// manifest.ErrUnsupportedSchema here, which callers must not swallow.
func detectLayout(vaultRoot string) (layout.Layout, error) {
	l, err := layout.Detect(vaultRoot)
	if err != nil {
		return layout.Layout{}, fmt.Errorf("failed to detect vault storage format: %w", err)
	}
	return l, nil
}

// storeAssetVersion extracts zipData into the layout's version directory,
// records the version in list.txt, and (v2) refreshes the materialized root
// view so assets/{name} always mirrors the latest archived version.
func storeAssetVersion(vaultRoot string, l layout.Layout, name, ver string, zipData []byte) error {
	versionDir := filepath.Join(vaultRoot, l.VersionDir(name, ver))
	if err := os.MkdirAll(versionDir, 0755); err != nil {
		return fmt.Errorf("failed to create asset directory: %w", err)
	}
	if err := extractZipToDir(zipData, versionDir); err != nil {
		return fmt.Errorf("failed to extract zip to directory: %w", err)
	}
	if err := updateVersionListFile(filepath.Join(vaultRoot, l.VersionListPath(name)), ver); err != nil {
		return fmt.Errorf("failed to update version list: %w", err)
	}
	return refreshRootView(vaultRoot, l, name)
}

// refreshRootView re-copies the latest archived version of an asset into its
// root view at assets/{name}. No-op for v1 vaults. The copy is staged into a
// dot-prefixed sibling and renamed into place so readers never observe a
// half-written view; ListAssets skips dot-prefixed entries.
func refreshRootView(vaultRoot string, l layout.Layout, name string) error {
	if l.Version() != layout.V2 {
		return nil
	}
	viewDir := filepath.Join(vaultRoot, l.AssetDir(name))
	versions, err := readVersionListFile(filepath.Join(vaultRoot, l.VersionListPath(name)))
	if err != nil {
		return err
	}
	if len(versions) == 0 {
		return os.RemoveAll(viewDir)
	}
	latest := versions[len(versions)-1]
	srcDir := filepath.Join(vaultRoot, l.VersionDir(name, latest))

	stagingDir := filepath.Join(vaultRoot, l.AssetsRoot(), "."+name+".staging")
	if err := os.RemoveAll(stagingDir); err != nil {
		return err
	}
	if err := copyDir(srcDir, stagingDir); err != nil {
		return fmt.Errorf("failed to stage root view for %s: %w", name, err)
	}
	if err := os.RemoveAll(viewDir); err != nil {
		_ = os.RemoveAll(stagingDir)
		return err
	}
	if err := os.Rename(stagingDir, viewDir); err != nil {
		_ = os.RemoveAll(stagingDir)
		return fmt.Errorf("failed to install root view for %s: %w", name, err)
	}
	return nil
}

// deleteAssetStorage removes stored files for an asset. With an empty
// version, the whole asset (root view and archive) is removed; otherwise
// only that version is removed and, on v2, the root view is refreshed to the
// remaining latest. Removing the last version removes the asset entirely.
func deleteAssetStorage(vaultRoot string, l layout.Layout, name, ver string) error {
	if ver == "" {
		if err := os.RemoveAll(filepath.Join(vaultRoot, l.AssetDir(name))); err != nil {
			return err
		}
		if l.Version() == layout.V2 {
			return os.RemoveAll(filepath.Join(vaultRoot, l.VersionsDir(name)))
		}
		return nil
	}

	if err := os.RemoveAll(filepath.Join(vaultRoot, l.VersionDir(name, ver))); err != nil {
		return err
	}
	listPath := filepath.Join(vaultRoot, l.VersionListPath(name))
	if err := removeFromVersionListFile(listPath, ver); err != nil {
		return err
	}
	remaining, err := readVersionListFile(listPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(remaining) == 0 {
		return deleteAssetStorage(vaultRoot, l, name, "")
	}
	return refreshRootView(vaultRoot, l, name)
}

// renameAssetStorage renames an asset's stored files (root view and, on v2,
// the version archive). Fails if the target name already exists.
func renameAssetStorage(vaultRoot string, l layout.Layout, oldName, newName string) error {
	oldDir := filepath.Join(vaultRoot, l.AssetDir(oldName))
	newDir := filepath.Join(vaultRoot, l.AssetDir(newName))
	if _, err := os.Stat(newDir); err == nil {
		return fmt.Errorf("target asset directory already exists: %s", newName)
	}
	if err := os.Rename(oldDir, newDir); err != nil {
		return fmt.Errorf("failed to rename asset directory: %w", err)
	}
	if l.Version() == layout.V2 {
		oldVersions := filepath.Join(vaultRoot, l.VersionsDir(oldName))
		newVersions := filepath.Join(vaultRoot, l.VersionsDir(newName))
		if _, err := os.Stat(newVersions); err == nil {
			return fmt.Errorf("target asset archive already exists: %s", newName)
		}
		if err := os.Rename(oldVersions, newVersions); err != nil {
			return fmt.Errorf("failed to rename asset archive: %w", err)
		}
	}
	return nil
}

// versionListForAsset reads an asset's version list through the layout,
// falling back to the manifest for assets that exist only as manifest rows
// (e.g. http- or git-sourced assets with no stored files).
func versionListForAsset(vaultRoot string, l layout.Layout, name string) ([]string, error) {
	listPath := filepath.Join(vaultRoot, l.VersionListPath(name))
	if _, err := os.Stat(listPath); os.IsNotExist(err) {
		return manifestAssetVersions(vaultRoot, name)
	}
	data, err := os.ReadFile(listPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read version list: %w", err)
	}
	return parseVersionList(data), nil
}

// readVersionListFile reads and semver-sorts a list.txt. Returns the
// underlying error (including os.IsNotExist) unwrapped so callers can branch
// on a missing list.
func readVersionListFile(listPath string) ([]string, error) {
	data, err := os.ReadFile(listPath)
	if err != nil {
		return nil, err
	}
	return version.Sort(parseVersionList(data)), nil
}

// updateVersionListFile appends a version to a list.txt if not already
// present, creating the file (and parent directory) as needed. The write is
// atomic so a concurrent reader never observes a truncated list.
func updateVersionListFile(listPath, newVersion string) error {
	var versions []string
	if data, err := os.ReadFile(listPath); err == nil {
		versions = parseVersionList(data)
	}
	if slices.Contains(versions, newVersion) {
		return nil
	}
	versions = append(versions, newVersion)
	if err := os.MkdirAll(filepath.Dir(listPath), 0755); err != nil {
		return err
	}
	content := strings.Join(versions, "\n") + "\n"
	return utils.WriteFileAtomic(listPath, []byte(content), 0644)
}

// removeFromVersionListFile removes a version from a list.txt. Missing files
// are a no-op; an emptied list is written as an empty file (callers decide
// whether to remove the asset entirely).
func removeFromVersionListFile(listPath, ver string) error {
	data, err := os.ReadFile(listPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var filtered []string
	for _, v := range parseVersionList(data) {
		if v != ver {
			filtered = append(filtered, v)
		}
	}
	if len(filtered) == 0 {
		return utils.WriteFileAtomic(listPath, []byte(""), 0644)
	}
	content := strings.Join(filtered, "\n") + "\n"
	return utils.WriteFileAtomic(listPath, []byte(content), 0644)
}

// StorageSourcePath returns the vault-relative source path recorded in the
// manifest for a stored version, per the vault's current storage layout.
func (g *GitVault) StorageSourcePath(name, version string) (string, error) {
	l, err := detectLayout(g.repoPath)
	if err != nil {
		return "", err
	}
	return l.SourcePathRel(name, version), nil
}

// StorageSourcePath returns the vault-relative source path recorded in the
// manifest for a stored version, per the vault's current storage layout.
func (p *PathVault) StorageSourcePath(name, version string) (string, error) {
	l, err := detectLayout(p.repoPath)
	if err != nil {
		return "", err
	}
	return l.SourcePathRel(name, version), nil
}

// updateRenamedAssetMetadata rewrites the name field in every stored
// metadata.toml for a just-renamed asset — each archived version plus, on
// v2, the materialized root view. Best-effort: failures warn but do not
// abort the rename, matching the previous behavior.
func updateRenamedAssetMetadata(vaultRoot string, l layout.Layout, newName string) {
	versions, err := versionListForAsset(vaultRoot, l, newName)
	if err == nil {
		for _, v := range versions {
			metadataPath := filepath.Join(vaultRoot, l.MetadataPath(newName, v))
			if err := metadata.UpdateName(metadataPath, newName); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not update metadata for %s@%s: %v\n", newName, v, err)
			}
		}
	}
	if l.Version() == layout.V2 {
		viewMetadata := filepath.Join(vaultRoot, l.AssetDir(newName), "metadata.toml")
		if err := metadata.UpdateName(viewMetadata, newName); err != nil && !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "Warning: could not update root view metadata for %s: %v\n", newName, err)
		}
	}
}

// copyDir recursively copies a directory tree. Asset trees come from
// extracted zips, so only regular files and directories are expected;
// anything else is an error rather than silently skipped.
func copyDir(srcDir, dstDir string) error {
	return filepath.WalkDir(srcDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dstDir, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("unsupported file type in asset tree: %s", path)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		mode := os.FileMode(0644)
		if info.Mode()&0111 != 0 {
			mode = 0755
		}
		return copyFile(path, target, mode)
	})
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
