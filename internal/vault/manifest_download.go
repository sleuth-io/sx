package vault

import (
	"context"
	"fmt"
	"strings"

	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/manifest"
	"github.com/sleuth-io/sx/internal/metadata"
	"github.com/sleuth-io/sx/internal/utils"
)

func manifestAssetVersions(vaultRoot, name string) ([]string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, nil
	}
	m, ok, err := manifest.Load(vaultRoot)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	versions := []string{}
	for _, asset := range m.Assets {
		if asset.Name != name || strings.TrimSpace(asset.Version) == "" {
			continue
		}
		versions = append(versions, asset.Version)
	}
	return versions, nil
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
	for _, asset := range m.Assets {
		if asset.Name == name && asset.Version == version {
			out := manifestAssetToLockfile(asset)
			return &out, true, nil
		}
	}
	return nil, false, nil
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
