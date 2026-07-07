// Package publish holds the UI-free core of the asset publish pipeline:
// detecting what a bundle of files is, suggesting the next version, and
// stamping metadata into the uploadable zip. Both `sx add` and the desktop
// app route through these functions so the two front ends cannot drift.
package publish

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/assets/detectors"
	"github.com/sleuth-io/sx/internal/metadata"
	"github.com/sleuth-io/sx/internal/utils"
	"github.com/sleuth-io/sx/internal/version"
)

// VersionReader is the slice of the vault interface SuggestVersion needs.
type VersionReader interface {
	GetVersionList(ctx context.Context, name string) ([]string, error)
	GetAssetByVersion(ctx context.Context, name, version string) ([]byte, error)
}

// DetectNameAndType inspects a zipped asset: if it carries a metadata.toml,
// the declared name and type win; otherwise the type is auto-detected from
// the file listing and fallbackName is used. hasMetadata reports whether a
// parseable metadata.toml was present.
func DetectNameAndType(zipData []byte, fallbackName string) (name string, t asset.Type, hasMetadata bool, err error) {
	if metadataBytes, readErr := utils.ReadZipFile(zipData, "metadata.toml"); readErr == nil {
		meta, parseErr := metadata.Parse(metadataBytes)
		if parseErr != nil {
			return "", asset.Type{}, false, fmt.Errorf("failed to parse metadata: %w", parseErr)
		}
		return meta.Asset.Name, meta.Asset.Type, true, nil
	}

	files, listErr := utils.ListZipFiles(zipData)
	if listErr != nil {
		return "", asset.Type{}, false, fmt.Errorf("failed to list zip files: %w", listErr)
	}
	detected := detectors.DetectAssetType(files, fallbackName, "")
	return fallbackName, detected.Asset.Type, false, nil
}

// SuggestVersion returns the version a new publish of zipData should get:
// "1" for a brand-new asset, the incremented latest otherwise. identical is
// true when zipData matches the latest stored version byte-for-byte
// (publishing would be a no-op).
func SuggestVersion(ctx context.Context, vault VersionReader, name string, zipData []byte) (suggested string, identical bool, err error) {
	versions, err := vault.GetVersionList(ctx, name)
	if err != nil {
		return "", false, fmt.Errorf("failed to get version list: %w", err)
	}
	return SuggestVersionFromList(ctx, vault, name, versions, zipData)
}

// SuggestVersionFromList is SuggestVersion for callers that already hold the
// version list.
func SuggestVersionFromList(ctx context.Context, vault VersionReader, name string, versions []string, zipData []byte) (suggested string, identical bool, err error) {
	if len(versions) == 0 {
		return "1", false, nil
	}
	latest := versions[len(versions)-1]

	existing, err := vault.GetAssetByVersion(ctx, name, latest)
	if err != nil {
		// Can't fetch the previous version for comparison — suggest the
		// increment and let the publish proceed.
		return version.IncrementMajor(latest), false, nil
	}
	same, err := utils.CompareZipContents(zipData, existing)
	if err != nil {
		return "", false, fmt.Errorf("failed to compare contents: %w", err)
	}
	if same {
		return latest, true, nil
	}
	return version.IncrementMajor(latest), false, nil
}

// BuildMetadata produces the metadata to stamp into a publish: existing
// metadata from the zip (with name/version/type overridden) when present,
// or freshly detected metadata otherwise. A missing description backfills
// from the bundle's markdown frontmatter — authors declare it there, and
// every listing surface reads metadata.
func BuildMetadata(name, ver string, assetType asset.Type, zipData []byte) *metadata.Metadata {
	meta := (*metadata.Metadata)(nil)
	if metadataBytes, err := utils.ReadZipFile(zipData, "metadata.toml"); err == nil {
		if existing, parseErr := metadata.Parse(metadataBytes); parseErr == nil {
			meta = existing
		}
	}
	if meta == nil {
		files, _ := utils.ListZipFiles(zipData)
		meta = detectors.DetectAssetType(files, name, ver)
	}
	meta.Asset.Name = name
	meta.Asset.Version = ver
	meta.Asset.Type = assetType
	if meta.Asset.Description == "" {
		meta.Asset.Description = inferZipDescription(zipData)
	}
	return meta
}

// inferZipDescription pulls a description from the bundle's markdown:
// SKILL.md first, then the alphabetically first top-level markdown file.
func inferZipDescription(zipData []byte) string {
	files, err := utils.ListZipFiles(zipData)
	if err != nil {
		return ""
	}
	var candidates []string
	for _, name := range files {
		if strings.ContainsRune(name, '/') {
			continue
		}
		lower := strings.ToLower(name)
		if !strings.HasSuffix(lower, ".md") && !strings.HasSuffix(lower, ".markdown") {
			continue
		}
		if lower == "skill.md" {
			candidates = append([]string{name}, candidates...)
		} else {
			candidates = append(candidates, name)
		}
	}
	if len(candidates) == 0 {
		return ""
	}
	if strings.ToLower(candidates[0]) != "skill.md" {
		sort.Strings(candidates)
	}
	for _, name := range candidates {
		content, err := utils.ReadZipFile(zipData, name)
		if err != nil {
			continue
		}
		if desc := asset.InferDescription(string(content)); desc != "" {
			return desc
		}
	}
	return ""
}

// ApplyMetadata writes meta into the zip as metadata.toml, replacing an
// existing entry or adding one.
func ApplyMetadata(meta *metadata.Metadata, zipData []byte, hasMetadata bool) ([]byte, error) {
	metadataBytes, err := metadata.Marshal(meta)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal metadata: %w", err)
	}
	if hasMetadata {
		out, err := utils.ReplaceFileInZip(zipData, "metadata.toml", metadataBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to replace metadata in zip: %w", err)
		}
		return out, nil
	}
	out, err := utils.AddFileToZip(zipData, "metadata.toml", metadataBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to add metadata to zip: %w", err)
	}
	return out, nil
}
