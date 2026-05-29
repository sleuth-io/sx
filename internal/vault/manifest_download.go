package vault

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/manifest"
	"github.com/sleuth-io/sx/internal/metadata"
	"github.com/sleuth-io/sx/internal/utils"
	"github.com/sleuth-io/sx/internal/version"
)

func manifestAssetVersions(vaultRoot, name string) ([]string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return []string{}, nil
	}
	m, ok, err := manifest.Load(vaultRoot)
	if err != nil {
		return nil, err
	}
	if !ok {
		return []string{}, nil
	}
	versions := []string{}
	for _, asset := range m.Assets {
		if asset.Name != name || strings.TrimSpace(asset.Version) == "" {
			continue
		}
		versions = append(versions, asset.Version)
	}
	return version.Sort(versions), nil
}

func findAssetVersionInManifest(vaultRoot, name, version string) (*lockfile.Asset, bool, error) {
	name = strings.TrimSpace(name)
	version = strings.TrimSpace(version)
	if name == "" || version == "" {
		return nil, false, nil
	}
	m, ok, err := manifest.Load(vaultRoot)
	if err != nil {
		return nil, false, err
	}
	if !ok {
		return nil, false, nil
	}
	var found *lockfile.Asset
	for _, asset := range m.Assets {
		if asset.Name == name && asset.Version == version {
			out := manifestAssetToLockfile(asset)
			if found != nil {
				return nil, false, fmt.Errorf("duplicate manifest asset %q version %q", name, version)
			}
			found = &out
		}
	}
	if found != nil {
		return found, true, nil
	}
	return nil, false, nil
}

func assetFromStorage(vaultRoot, name string) (*lockfile.Asset, bool, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, false, nil
	}
	listPath := filepath.Join(vaultRoot, "assets", name, "list.txt")
	data, err := os.ReadFile(listPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("read version list for %q: %w", name, err)
	}
	versions := version.Sort(parseVersionList(data))
	if len(versions) == 0 {
		return nil, false, nil
	}
	// The install repair call site has no version in scope, so storage
	// recovery picks the highest semver stored on disk.
	latest := versions[len(versions)-1]
	metaPath := filepath.Join(vaultRoot, "assets", name, latest, "metadata.toml")
	metaBytes, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, false, fmt.Errorf("read metadata for %q@%s: %w", name, latest, err)
	}
	meta, err := metadata.Parse(metaBytes)
	if err != nil {
		return nil, false, fmt.Errorf("parse metadata for %q@%s: %w", name, latest, err)
	}
	if meta.Asset.Type.Key == "" {
		return nil, false, fmt.Errorf("metadata for %q@%s has no asset type", name, latest)
	}
	deps := make([]lockfile.Dependency, 0, len(meta.Asset.Dependencies))
	for _, dep := range meta.Asset.Dependencies {
		if dep = strings.TrimSpace(dep); dep != "" {
			deps = append(deps, lockfile.Dependency{Name: dep})
		}
	}
	// Storage recovery is intentionally lossy: the original source kind and
	// manifest-only fields are gone, so the recovered row points at the bytes
	// currently present under assets/<name>/<version>.
	return &lockfile.Asset{
		Name:         name,
		Version:      latest,
		Type:         meta.Asset.Type,
		Clients:      append([]string(nil), meta.Asset.Clients...),
		Dependencies: deps,
		SourcePath: &lockfile.SourcePath{
			Path: filepath.ToSlash(filepath.Join("assets", name, latest)),
		},
	}, true, nil
}

func metadataFromAssetZip(ctx context.Context, repo Vault, asset *lockfile.Asset) (*metadata.Metadata, error) {
	data, err := repo.GetAsset(ctx, asset)
	if err != nil {
		return nil, err
	}
	raw, err := utils.ReadZipFile(data, "metadata.toml")
	if err != nil {
		return nil, fmt.Errorf("failed to read metadata.toml from asset zip: %w", err)
	}
	return metadata.Parse(raw)
}
