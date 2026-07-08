package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/metadata"
	"github.com/sleuth-io/sx/internal/publish"
	"github.com/sleuth-io/sx/internal/utils"
)

// PluginMetadataPatch is the safe, curated subset of asset metadata an
// extension may write: descriptive fields only, never content, type,
// scoping, or installs. Nil pointers mean "leave unchanged".
type PluginMetadataPatch struct {
	Description *string  `json:"description"`
	Keywords    []string `json:"keywords"`
	Owner       *string  `json:"owner"`
	Status      *string  `json:"status"`
}

// PluginWriteMetadata publishes a new revision of an asset with updated
// metadata and unchanged content — the assets:write-metadata capability.
// A revision (not an in-place edit) keeps versions immutable and the
// change audited like any publish; sharing is inherited.
func (a *App) PluginWriteMetadata(name string, patch PluginMetadataPatch) error {
	if err := validateAssetRef(name, ""); err != nil {
		return err
	}
	v, err := a.currentVault()
	if err != nil {
		return err
	}
	zipData, err := a.latestAssetZip(name)
	if err != nil {
		return err
	}

	var meta *metadata.Metadata
	if metaBytes, err := utils.ReadZipFile(zipData, "metadata.toml"); err == nil {
		meta, _ = metadata.Parse(metaBytes)
	}
	if meta == nil {
		return fmt.Errorf("%s has no readable metadata", name)
	}
	if meta.Asset.Type.Key == asset.TypeAppPlugin.Key {
		// Extensions are hidden from the extension API — including from
		// each other's metadata edits.
		return errors.New("extensions cannot edit other extensions")
	}
	if patch.Description != nil {
		meta.Asset.Description = strings.TrimSpace(*patch.Description)
	}
	if patch.Keywords != nil {
		meta.Asset.Keywords = patch.Keywords
	}
	if meta.Custom == nil {
		meta.Custom = map[string]any{}
	}
	if patch.Owner != nil {
		meta.Custom["owner"] = strings.TrimSpace(*patch.Owner)
	}
	if patch.Status != nil {
		meta.Custom["status"] = strings.TrimSpace(*patch.Status)
	}

	// Stamp with the current version first so the identical-content check
	// (which ignores only the version field) sees the metadata change.
	candidate, err := publish.ApplyMetadata(meta, zipData, true)
	if err != nil {
		return err
	}
	version, identical, err := publish.SuggestVersion(a.ctx, v, name, candidate)
	if err != nil {
		return friendlyVaultError(err)
	}
	if identical {
		return nil // no-op patch; nothing to publish
	}
	meta.Asset.Version = version
	finalZip, err := publish.ApplyMetadata(meta, zipData, true)
	if err != nil {
		return err
	}
	lockAsset := &lockfile.Asset{
		Name:    meta.Asset.Name,
		Version: version,
		Type:    meta.Asset.Type,
		Clients: append([]string(nil), meta.Asset.Clients...),
	}
	if err := v.AddAsset(a.ctx, lockAsset, finalZip); err != nil {
		return friendlyVaultError(err)
	}
	if err := v.InheritInstallations(a.ctx, lockAsset); err != nil {
		return friendlyVaultError(err)
	}
	a.purgeSearchCache(name)
	return nil
}
