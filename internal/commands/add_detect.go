package commands

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/assets/detectors"
	"github.com/sleuth-io/sx/internal/github"
	"github.com/sleuth-io/sx/internal/metadata"
	"github.com/sleuth-io/sx/internal/ui/components"
	"github.com/sleuth-io/sx/internal/utils"
	vaultpkg "github.com/sleuth-io/sx/internal/vault"
	versionpkg "github.com/sleuth-io/sx/internal/version"
)

// detectAssetInfo extracts or detects asset name and type, then confirms with user
func detectAssetInfo(out *outputHelper, status *components.Status, zipFile string, zipData []byte, opts addOptions) (name string, assetType asset.Type, metadataExists bool, err error) {
	// Extract or detect name and type
	name, assetType, metadataExists, err = extractOrDetectNameAndType(status, zipFile, zipData)
	if err != nil {
		return
	}

	// Apply flag overrides
	if opts.Name != "" {
		name = opts.Name
	}
	if opts.Type != "" {
		assetType = asset.FromString(opts.Type)
		if !assetType.IsValid() {
			return "", asset.Type{}, false, fmt.Errorf("invalid asset type: %s", opts.Type)
		}
	}

	// Confirm name and type with user (skipped if --yes)
	name, assetType, err = confirmNameAndType(out, name, assetType, opts)
	if err != nil {
		return
	}

	return name, assetType, metadataExists, nil
}

// extractOrDetectNameAndType extracts name and type from metadata or auto-detects them
func extractOrDetectNameAndType(status *components.Status, zipFile string, zipData []byte) (name string, assetType asset.Type, metadataExists bool, err error) {
	status.Start("Detecting asset name and type")

	metadataBytes, err := utils.ReadZipFile(zipData, "metadata.toml")
	if err == nil {
		// Metadata exists, parse it
		meta, err := metadata.Parse(metadataBytes)
		if err != nil {
			status.Fail("Failed to parse metadata")
			return "", asset.Type{}, false, fmt.Errorf("failed to parse metadata: %w", err)
		}
		status.Done("")
		return meta.Asset.Name, meta.Asset.Type, true, nil
	}

	// No metadata, auto-detect name and type
	status.Update("Auto-detecting asset type")

	// List files in zip
	files, err := utils.ListZipFiles(zipData)
	if err != nil {
		status.Fail("Failed to list zip files")
		return "", asset.Type{}, false, fmt.Errorf("failed to list zip files: %w", err)
	}

	// Auto-detect values
	name = guessAssetName(zipFile)

	// Use handlers to detect type
	detectedMeta := detectors.DetectAssetType(files, name, "")
	assetType = detectedMeta.Asset.Type

	status.Done("")
	return name, assetType, false, nil
}

// confirmNameAndType displays name and type and asks for confirmation
func confirmNameAndType(out *outputHelper, name string, inType asset.Type, opts addOptions) (outName string, outType asset.Type, err error) {
	outName = name
	outType = inType

	out.println()
	out.println("Detected asset:")
	out.printf("  Name: %s\n", outName)
	out.printf("  Type: %s\n", outType)
	out.println()

	// Skip confirmation if --yes
	if opts.Yes {
		return
	}

	confirmed, err := components.ConfirmWithIO("Is this correct?", true, out.cmd.InOrStdin(), out.cmd.OutOrStdout())
	if err != nil {
		err = fmt.Errorf("failed to read confirmation: %w", err)
		return
	}

	if !confirmed {
		// Prompt for custom name and type
		nameInput, err2 := components.InputWithIO("Asset name", "", outName, out.cmd.InOrStdin(), out.cmd.OutOrStdout())
		if err2 != nil {
			err = fmt.Errorf("failed to read name: %w", err2)
			return
		}
		if nameInput != "" {
			outName = nameInput
		}

		typeInput, err2 := components.InputWithIO("Asset type", "", outType.Label, out.cmd.InOrStdin(), out.cmd.OutOrStdout())
		if err2 != nil {
			err = fmt.Errorf("failed to read type: %w", err2)
			return
		}
		if typeInput != "" {
			outType = asset.FromString(typeInput)
		}
	}

	return
}

// determineSuggestedVersionAndCheckIdentical determines the version to suggest and whether contents are identical
func determineSuggestedVersionAndCheckIdentical(ctx context.Context, status *components.Status, vault vaultpkg.Vault, name string, versions []string, newZipData []byte) (version string, identical bool, err error) {
	if len(versions) == 0 {
		// No existing versions, suggest 1
		return "1", false, nil
	}

	// Get the latest version
	latestVersion := versions[len(versions)-1]

	// Try to get the asset for comparison
	status.Start("Comparing with v" + latestVersion)

	existingZipData, err := vault.GetAssetByVersion(ctx, name, latestVersion)
	if err != nil {
		// If we can't get the existing version, suggest incrementing
		status.Clear()
		return versionpkg.IncrementMajor(latestVersion), false, nil
	}

	// Compare the contents
	contentsIdentical, err := utils.CompareZipContents(newZipData, existingZipData)
	status.Clear()
	if err != nil {
		return "", false, fmt.Errorf("failed to compare contents: %w", err)
	}

	if contentsIdentical {
		return latestVersion, true, nil
	}

	// Contents differ, suggest next version
	return versionpkg.IncrementMajor(latestVersion), false, nil
}

// promptForVersion prompts the user to confirm or edit the version
func promptForVersion(out *outputHelper, suggestedVersion string) (string, error) {
	out.println()
	version, err := components.InputWithIO("Version", "", suggestedVersion, out.cmd.InOrStdin(), out.cmd.OutOrStdout())
	if err != nil {
		return "", fmt.Errorf("failed to read version: %w", err)
	}

	return version, nil
}

// createMetadata creates a metadata object with the given name, version, and type
func createMetadata(name, version string, assetType asset.Type, zipFile string, zipData []byte) *metadata.Metadata {
	// Try to read existing metadata from zip first
	if metadataBytes, err := utils.ReadZipFile(zipData, "metadata.toml"); err == nil {
		if existingMeta, err := metadata.Parse(metadataBytes); err == nil {
			// Use existing metadata, just update name/version/type
			existingMeta.Asset.Name = name
			existingMeta.Asset.Version = version
			existingMeta.Asset.Type = assetType
			return existingMeta
		}
		// If parse fails, fall through to create new metadata
	}

	// No existing metadata or failed to parse - create new metadata using detection
	files, _ := utils.ListZipFiles(zipData)
	meta := detectors.DetectAssetType(files, name, version)

	// Override with our confirmed values
	meta.Asset.Name = name
	meta.Asset.Version = version
	meta.Asset.Type = assetType

	return meta
}

// updateMetadataInZip updates or adds metadata.toml in the zip with the correct version
func updateMetadataInZip(meta *metadata.Metadata, zipData []byte, metadataExists bool) ([]byte, error) {
	metadataBytes, err := metadata.Marshal(meta)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal metadata: %w", err)
	}

	if metadataExists {
		// Replace existing metadata.toml in zip
		newZipData, err := utils.ReplaceFileInZip(zipData, "metadata.toml", metadataBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to replace metadata in zip: %w", err)
		}
		return newZipData, nil
	}

	// Add new metadata.toml to zip
	newZipData, err := utils.AddFileToZip(zipData, "metadata.toml", metadataBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to add metadata to zip: %w", err)
	}
	return newZipData, nil
}

// guessAssetName extracts a reasonable asset name from the zip file path or URL
func guessAssetName(zipPath string) string {
	// Handle GitHub tree URLs specially
	if treeURL := github.ParseTreeURL(zipPath); treeURL != nil {
		return treeURL.SkillName()
	}

	// Handle URLs - extract path component
	if isURL(zipPath) {
		if parsed, err := url.Parse(zipPath); err == nil {
			zipPath = parsed.Path
		}
	}

	// Get base filename
	base := strings.TrimPrefix(zipPath, "./")
	base = strings.TrimPrefix(base, "../")

	// If it's just a path, get the last component
	if idx := strings.LastIndex(base, "/"); idx != -1 {
		base = base[idx+1:]
	}
	if idx := strings.LastIndex(base, "\\"); idx != -1 {
		base = base[idx+1:]
	}

	// Strip any file extension
	if idx := strings.LastIndex(base, "."); idx != -1 {
		base = base[:idx]
	}

	// Strip version suffix if present (e.g., "my-skill-1.0.0" -> "my-skill")
	parts := strings.Split(base, "-")
	if len(parts) > 1 {
		lastPart := parts[len(parts)-1]
		// Check if last part looks like a version
		if strings.Contains(lastPart, ".") {
			allDigitsOrDots := true
			for _, c := range lastPart {
				if c != '.' && (c < '0' || c > '9') {
					allDigitsOrDots = false
					break
				}
			}
			if allDigitsOrDots {
				base = strings.Join(parts[:len(parts)-1], "-")
			}
		}
	}

	if base == "" {
		base = "my-asset"
	}

	return base
}
